package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coachbuddy/gateway-go/internal/auth"
	"github.com/coachbuddy/gateway-go/internal/models"
	"github.com/coachbuddy/gateway-go/internal/store"
	"github.com/google/uuid"
)

type APIHandler struct {
	Store              *store.DataStore
	Tokens             *auth.TokenManager
	AgentsServiceURL   string
	McpRustURL         string
	PublicShareBaseURL string
	InternalToken      string // shared secret sent to agents-python as X-Internal-Gateway-Token
	HttpClient         *http.Client
}

func NewAPIHandler(ds *store.DataStore, tm *auth.TokenManager, agentsURL, mcpRustURL, shareBaseURL, internalToken string) *APIHandler {
	return &APIHandler{
		Store:              ds,
		Tokens:             tm,
		InternalToken:      internalToken,
		AgentsServiceURL:   strings.TrimRight(agentsURL, "/"),
		McpRustURL:         strings.TrimRight(mcpRustURL, "/"),
		PublicShareBaseURL: strings.TrimRight(shareBaseURL, "/"),
		HttpClient: &http.Client{
			Timeout: 150 * time.Second,
		},
	}
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// --- Health Check ---

func (h *APIHandler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"service": "gateway-go",
		"version": "1.0.0",
		"features": []string{
			"jwt-auth",
			"aes-256-gcm-byok-vault",
			"google-adk-orchestrator-proxy",
			"redis-sse-streaming",
			"rust-mcp-qr-sharing",
		},
	})
}

// --- Auth Endpoints ---

func (h *APIHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email                   string `json:"email"`
		Password                string `json:"password"`
		DisplayName             string `json:"displayName"`
		BoxName                 string `json:"boxName"`
		DefaultClassDurationMin int    `json:"defaultClassDurationMin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = "Coach"
	}
	coach, err := h.Store.CreateCoach(r.Context(), req.Email, req.Password, req.DisplayName, req.BoxName, req.DefaultClassDurationMin)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	token, err := h.Tokens.GenerateToken(coach.ID, coach.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign JWT")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"token": token,
		"coach": coach,
	})
}

func (h *APIHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	coach, err := h.Store.AuthenticateCoach(r.Context(), req.Email, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	token, err := h.Tokens.GenerateToken(coach.ID, coach.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign JWT")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token": token,
		"coach": coach,
	})
}

func (h *APIHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	coachID := auth.GetCoachID(r.Context())
	coach, err := h.Store.GetCoach(r.Context(), coachID)
	if err != nil {
		writeError(w, http.StatusNotFound, "coach not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"coach": coach})
}

func (h *APIHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	coachID := auth.GetCoachID(r.Context())
	var req struct {
		DisplayName             string `json:"displayName"`
		BoxName                 string `json:"boxName"`
		DefaultClassDurationMin int    `json:"defaultClassDurationMin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	coach, err := h.Store.UpdateCoachProfile(r.Context(), coachID, req.DisplayName, req.BoxName, req.DefaultClassDurationMin)
	if errors.Is(err, store.ErrCoachNotFound) {
		writeError(w, http.StatusNotFound, "coach not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update profile")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"coach": coach})
}

// --- BYOK Encrypted API Key Vault & Agent Routing Settings ---

func (h *APIHandler) ListKeys(w http.ResponseWriter, r *http.Request) {
	coachID := auth.GetCoachID(r.Context())
	keys, err := h.Store.ListAPIKeys(r.Context(), coachID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if keys == nil {
		keys = []*models.LLMAPIKey{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"keys": keys})
}

func (h *APIHandler) SaveKey(w http.ResponseWriter, r *http.Request) {
	coachID := auth.GetCoachID(r.Context())
	var req struct {
		Provider  string `json:"provider"`
		APIKey    string `json:"apiKey"`
		IsPrimary bool   `json:"isPrimary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	if strings.TrimSpace(req.APIKey) == "" {
		writeError(w, http.StatusBadRequest, "apiKey is required")
		return
	}
	if req.Provider == "" {
		req.Provider = "gemini"
	}
	keyRec, err := h.Store.SaveAPIKey(r.Context(), coachID, req.Provider, req.APIKey, req.IsPrimary)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"key": keyRec})
}

func (h *APIHandler) DeleteKey(w http.ResponseWriter, r *http.Request) {
	coachID := auth.GetCoachID(r.Context())
	keyID := r.PathValue("id")
	if keyID == "" {
		writeError(w, http.StatusBadRequest, "missing key id")
		return
	}
	_ = h.Store.DeleteAPIKey(r.Context(), coachID, keyID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *APIHandler) GetAgentPrefs(w http.ResponseWriter, r *http.Request) {
	coachID := auth.GetCoachID(r.Context())
	prefs, err := h.Store.GetAgentPrefs(r.Context(), coachID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"preferences": prefs})
}

func (h *APIHandler) SaveAgentPrefs(w http.ResponseWriter, r *http.Request) {
	coachID := auth.GetCoachID(r.Context())
	var req struct {
		Preferences []models.AgentModelPref `json:"preferences"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	prefs, err := h.Store.SaveAgentPrefs(r.Context(), coachID, req.Preferences)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"preferences": prefs})
}

// --- WOD Session Generation & Orchestration ---

func (h *APIHandler) GenerateSession(w http.ResponseWriter, r *http.Request) {
	coachID := auth.GetCoachID(r.Context())
	var input models.SessionInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	if input.TargetDurationMinutes <= 0 {
		input.TargetDurationMinutes = 60
	}
	if strings.TrimSpace(input.RawText) == "" && strings.TrimSpace(input.ImageBase64) == "" {
		writeError(w, http.StatusBadRequest, "please provide either WOD text or a WOD screenshot")
		return
	}

	sessionID := "wod-" + uuid.NewString()[:8]
	routingConfig, err := h.Store.BuildEphemeralRoutingConfig(r.Context(), coachID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Cannot orchestrate session: "+err.Error())
		return
	}

	orchReq := models.OrchestrateRequest{
		SessionID:     sessionID,
		CoachID:       coachID,
		Input:         input,
		RoutingConfig: routingConfig,
	}

	bodyBytes, _ := json.Marshal(orchReq)
	httpReq, err := http.NewRequestWithContext(r.Context(), "POST", h.AgentsServiceURL+"/api/v1/orchestrate", bytes.NewReader(bodyBytes))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create orchestrate request")
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Internal-Gateway-Token", h.InternalToken)

	resp, err := h.HttpClient.Do(httpReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Python Multi-Agent orchestrator unreachable: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Python orchestrator error (%d): %s", resp.StatusCode, string(respBody)))
		return
	}

	var sessionDoc map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&sessionDoc); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to decode orchestrator response: "+err.Error())
		return
	}

	_ = h.Store.SaveSession(r.Context(), sessionID, sessionDoc)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessionId": sessionID,
		"status":    "completed",
		"session":   sessionDoc,
	})
}

func (h *APIHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	coachID := auth.GetCoachID(r.Context())
	sessions, err := h.Store.ListSessions(r.Context(), coachID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sessions == nil {
		sessions = []map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": sessions})
}

// getOwnedSession loads a session and checks that it belongs to the authenticated coach.
// A session owned by someone else is reported as not found, so its existence is not revealed.
func (h *APIHandler) getOwnedSession(r *http.Request, sessionID string) (map[string]interface{}, bool) {
	coachID := auth.GetCoachID(r.Context())
	if coachID == "" {
		return nil, false
	}
	doc, err := h.Store.GetSession(r.Context(), sessionID)
	if err != nil || doc == nil {
		return nil, false
	}
	if owner, _ := doc["coachId"].(string); owner != coachID {
		return nil, false
	}
	return doc, true
}

func (h *APIHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	doc, ok := h.getOwnedSession(r, r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"session": doc})
}

func (h *APIHandler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if _, ok := h.getOwnedSession(r, sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if err := h.Store.DeleteSession(r.Context(), sessionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- SSE Progress Stream (Redis Pub/Sub) ---

func (h *APIHandler) StreamSessionEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	// Same rule as read, delete and share: another coach's session, or an unknown
	// one, is reported as not found before the stream opens.
	if _, ok := h.getOwnedSession(r, sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	rc := h.Store.Redis()
	if rc == nil {
		// Emit immediate completion event if Redis isn't running
		payload, _ := json.Marshal(map[string]interface{}{
			"sessionId": sessionID,
			"step":      "completed",
			"status":    "completed",
			"message":   "Multi-agent orchestration complete",
		})
		fmt.Fprintf(w, "data: %s\n\n", string(payload))
		flusher.Flush()
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	// Replay any buffered events from Redis list before listening to live stream
	buffered, err := rc.LRange(ctx, "session:buffered_events:"+sessionID, 0, -1).Result()
	if err == nil {
		for _, evt := range buffered {
			fmt.Fprintf(w, "data: %s\n\n", evt)
			flusher.Flush()
			if strings.Contains(evt, `"step":"completed"`) || strings.Contains(evt, `"step": "completed"`) {
				return
			}
		}
	}

	pubsub := rc.Subscribe(ctx, "session:events:"+sessionID)
	defer pubsub.Close()

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg.Payload)
			flusher.Flush()
			if strings.Contains(msg.Payload, `"step":"completed"`) || strings.Contains(msg.Payload, `"step": "completed"`) {
				return
			}
		}
	}
}

// --- Share Session & Rust MCP SVG QR Code ---

func (h *APIHandler) CreateShareLink(w http.ResponseWriter, r *http.Request) {
	coachID := auth.GetCoachID(r.Context())
	sessionID := r.PathValue("id")

	if _, ok := h.getOwnedSession(r, sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var req struct {
		IncludeCoachNotes *bool `json:"includeCoachNotes"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	includeNotes := true
	if req.IncludeCoachNotes != nil {
		includeNotes = *req.IncludeCoachNotes
	}

	// 12 hex chars (48 bits) so public share codes cannot be guessed by enumeration.
	shortCode := "cb-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	shareURL := fmt.Sprintf("%s/%s", h.PublicShareBaseURL, shortCode)

	qrSVG := h.fetchQRFromRustMCP(shareURL)

	link := &models.SharedLink{
		ShortCode:         shortCode,
		ShareURL:          shareURL,
		MongoSessionID:    sessionID,
		CoachID:           coachID,
		IncludeCoachNotes: includeNotes,
		QRSVG:             qrSVG,
		ViewCount:         0,
		CreatedAt:         time.Now(),
	}
	_ = h.Store.SaveSharedLink(r.Context(), link)

	writeJSON(w, http.StatusCreated, link)
}

func (h *APIHandler) ResolveShareLink(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	link, err := h.Store.GetSharedLink(r.Context(), code)
	if err != nil {
		writeError(w, http.StatusNotFound, "shared link expired or not found")
		return
	}
	link.ShareURL = fmt.Sprintf("%s/%s", h.PublicShareBaseURL, link.ShortCode)

	sessionDoc, err := h.Store.GetSession(r.Context(), link.MongoSessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "underlying WOD session not found")
		return
	}

	// Copy session document so we can filter private coaching warnings/notes if Athlete mode was selected
	raw, _ := json.Marshal(sessionDoc)
	var filtered map[string]interface{}
	_ = json.Unmarshal(raw, &filtered)

	// =========================================================================
	// TODO(engineer) 2.3: Server-side Sanitization for Athlete Share Links
	//
	// When `!link.IncludeCoachNotes`, athletes scan the QR code and should see
	// the workout movements and timeline structure, BUT NOT private coaching cues.
	//
	// Actions to take when `!link.IncludeCoachNotes`:
	// 1. Remove `coachWarnings` from timeline:
	//    `delete(tl, "coachWarnings")`
	// 2. Remove `notes` from each block in timeline:
	//    `if blocks, ok := tl["blocks"].([]interface{}); ok { for _, b := range blocks { if bm, ok := b.(map[string]interface{}); ok { delete(bm, "notes") } } }`
	// 3. Remove `cues` from exercises (both `parsedWod.movements` or `scaling.exercises`):
	//    Check `scaling` -> `exercises`: remove `cues`
	// 4. Remove `agentTraces` from root `filtered`:
	//    `delete(filtered, "agentTraces")`
	// =========================================================================

	if !link.IncludeCoachNotes {
		// delete any agent cues, details, data
		delete(filtered, "agentTraces")
		// delete coach notes from the timeline data
		if tl, ok := filtered["timeline"].(map[string]interface{}); ok {
			delete(tl, "coachWarnings")
			if blocks, ok := tl["blocks"].([]interface{}); ok {
				for _, b := range blocks {
					if bm, ok := b.(map[string]interface{}); ok {
						delete(bm, "notes")
					}
				}
			}
		}

		// delete scaling details
		if scaling, ok := filtered["scaling"].(map[string]interface{}); ok {
			if exercises, ok := scaling["exercises"].([]interface{}); ok {
				for _, ex := range exercises {
					if em, ok := ex.(map[string]interface{}); ok {
						delete(em, "cues")
					}
				}
			}
		}
		if exercises, ok := filtered["exercises"].([]interface{}); ok {
			for _, ex := range exercises {
				if em, ok := ex.(map[string]interface{}); ok {
					delete(em, "cues")
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"share":   link,
		"session": filtered,
	})
}

func (h *APIHandler) fetchQRFromRustMCP(shareURL string) string {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"data": shareURL,
		"size": 256,
	})
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(h.McpRustURL+"/tools/qr", "application/json", bytes.NewReader(reqBody))
	if err == nil && resp != nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			var parsed struct {
				SVG string `json:"svg"`
			}
			if json.Unmarshal(body, &parsed) == nil && strings.Contains(parsed.SVG, "<svg") {
				return parsed.SVG
			}
		}
	}
	return generateDeterministicFallbackSVGQR(shareURL)
}

func generateDeterministicFallbackSVGQR(data string) string {
	sum := sha256.Sum256([]byte(data))
	var rects strings.Builder
	grid := 21
	cell := 10
	margin := 20
	size := grid*cell + margin*2

	isFinder := func(r, c int) bool {
		inTL := r < 7 && c < 7
		inTR := r < 7 && c >= grid-7
		inBL := r >= grid-7 && c < 7
		return inTL || inTR || inBL
	}

	drawFinder := func(ox, oy int) {
		rects.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="70" height="70" rx="6" fill="none" stroke="#CCFF00" stroke-width="10"/>`, ox, oy))
		rects.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="30" height="30" rx="3" fill="#CCFF00"/>`, ox+20, oy+20))
	}

	drawFinder(margin, margin)
	drawFinder(margin+(grid-7)*cell, margin)
	drawFinder(margin, margin+(grid-7)*cell)

	bitIdx := 0
	for r := 0; r < grid; r++ {
		for c := 0; c < grid; c++ {
			if isFinder(r, c) {
				continue
			}
			b := sum[(bitIdx/8)%len(sum)]
			on := ((b >> (bitIdx % 8)) & 1) == 1
			bitIdx++
			if on {
				x := margin + c*cell
				y := margin + r*cell
				rects.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="8" height="8" rx="2" fill="#F8FAFC"/>`, x+1, y+1))
			}
		}
	}

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="256" height="256"><rect width="100%%" height="100%%" rx="16" fill="#0B0F17"/>%s</svg>`, size, size, rects.String())
}

func (h *APIHandler) buildResilientSessionPlan(sessionID, coachID string, input models.SessionInput, routes map[string]models.EphemeralAgentRoute) map[string]interface{} {
	title := input.Title
	if title == "" {
		lines := strings.Split(strings.TrimSpace(input.RawText), "\n")
		if len(lines) > 0 && len(lines[0]) > 3 {
			title = lines[0]
			if len(title) > 68 {
				title = title[:68] + "..."
			}
		} else {
			title = "Custom Box WOD Session Plan"
		}
	}

	dur := input.TargetDurationMinutes
	if dur <= 0 {
		dur = 60
	}

	warmupStyle := "Dynamic Game & Joint Prep Flow"
	if strings.Contains(strings.ToLower(input.CoachConstraints), "broom") || strings.Contains(strings.ToLower(input.CoachConstraints), "pvc") {
		warmupStyle = "Broomstick (PVC) Reaction Game + Mobility Flow"
	}

	return map[string]interface{}{
		"_id":       sessionID,
		"sessionId": sessionID,
		"coachId":   coachID,
		"status":    "completed",
		"title":     title,
		"createdAt": time.Now().Format(time.RFC3339),
		"input":     input,
		"parsedWod": map[string]interface{}{
			"blocks": []map[string]interface{}{
				{"type": "strength", "name": "Skill & Strength Progression", "allocatedMinutes": 15},
				{"type": "metcon", "name": "Primary Conditioning (Engine / Metcon)", "allocatedMinutes": 15},
			},
			"movements":     []string{"Front Squat", "Thruster", "Pull-Up"},
			"primaryJoints": []string{"shoulders", "thoracic_spine", "hips", "knees", "wrists"},
			"stimulus":      "High-output functional strength paired with metabolic conditioning.",
		},
		"warmup": map[string]interface{}{
			"style":           warmupStyle,
			"durationMinutes": 12,
			"phases": []map[string]interface{}{
				{
					"name":            "Phase 1: PVC Broomstick Circle Drop & Switch (Reaction Game)",
					"durationMinutes": 4,
					"instructions":    "Athletes stand in a circle balancing a broomstick vertically. On Coach's cue ('LEFT', 'RIGHT', 'SPIN'), step and catch the next stick before it drops. Penalty = 3 synchronized jump squats!",
					"coachingGoal":    "Put athletes in focus with a fun non-repetitive reaction game while raising core temperature.",
				},
				{
					"name":            "Phase 2: Targeted Broomstick & Joint Prep",
					"durationMinutes": 8,
					"instructions":    "Coach-led progressive mobility flow targeting the WOD's primary joints.",
					"coachingGoal":    "Prepare shoulders, thoracic spine, hips, and knees for full range of motion.",
					"drills": []map[string]interface{}{
						{"name": "PVC Pass-Throughs + Sotts Press", "reps": "10 + 5 reps", "focus": "Thoracic extension & overhead stability"},
						{"name": "Broomstick Kang Squats", "reps": "8 slow reps", "focus": "Posterior chain activation + deep squat bottom posture"},
						{"name": "Scapular Pulls + Kip Swings", "reps": "10 + 10 reps", "focus": "Shoulder girdle control & lat activation"},
					},
				},
			},
		},
		"exercises": []map[string]interface{}{
			{
				"name":     "Thruster / Squat Pressing Pattern",
				"category": "Primary Movement",
				"cues": map[string]interface{}{
					"external": []string{
						"Point your elbow lasers straight at the front wall",
						"Launch the bar off your shoulders using violent hip extension",
						"Push the floor apart with your feet on the drive up",
					},
					"internal": []string{
						"Take a 360-degree breath into your belt before descending",
						"Squeeze glutes and quads hard before your arms press",
					},
					"commonFaults": []map[string]interface{}{
						{"fault": "Elbows dropping at the bottom of the squat", "fix": "Cue: 'Chest proud, drive elbows toward the ceiling first'"},
						{"fault": "Pressing early before hips finish extending", "fix": "Cue: 'Jump the bar off your shoulders—legs first, arms second'"},
					},
				},
				"scaling": map[string]interface{}{
					"rxPlus":         "Heavier loading or unbroken set requirement with 2-sec pause on first rep",
					"scaled":         "Lighter barbell (65/45 lb) or Dual Dumbbell Thrusters (30/20 lb)",
					"seniorsMasters": "Squat to 15\" Box target + Dual DB Push Press (joint-friendly depth & wrist angle)",
					"injured": map[string]interface{}{
						"wristOrShoulder": "Single-Arm DB Thruster (uninjured side) or Goblet Squat + Russian KB Swing",
						"kneePain":        "Push Press from rack or Tempo Box Squat above parallel (vertical shins)",
						"lowerBack":       "Light Dual DB Front Squat to Box with upright torso focus",
					},
				},
			},
			{
				"name":     "Pull-Up / Gymnastics Pulling",
				"category": "Gymnastics",
				"cues": map[string]interface{}{
					"external": []string{
						"Push the bar away at the top so you swing right into the next rep",
						"Glue your shoes together—press your chest through the window",
					},
					"internal": []string{
						"Initiate from depressed shoulder blades (active lats), not bent knees",
					},
					"commonFaults": []map[string]interface{}{
						{"fault": "Dropping straight down from the bar and losing kip rhythm", "fix": "Cue: 'Push away from the bar at the top like a pendulum'"},
					},
				},
				"scaling": map[string]interface{}{
					"rxPlus":         "Chest-to-Bar Pull-Ups or Bar Muscle-Ups",
					"scaled":         "Banded Pull-Ups or Jumping Pull-Ups with 2-sec eccentric lower",
					"seniorsMasters": "Strict Ring Rows (feet below rings, controlled tempo)",
					"injured": map[string]interface{}{
						"wristOrShoulder": "Neutral-Grip Ring Rows or Single-Arm DB Bent-Over Rows",
						"kneePain":        "Strict/Banded Pull-Ups (step onto a box to dismount—no jumping impact)",
						"lowerBack":       "Chest-Supported Incline Dumbbell Rows",
					},
				},
			},
		},
		"timeline": map[string]interface{}{
			"totalMinutes":      dur,
			"feasibilityStatus": "OPTIMAL",
			"coachWarnings": []string{
				"Keep equipment transitions under 4 minutes by staging barbells and bands during the post-warmup water break.",
			},
			"blocks": []map[string]interface{}{
				{"startMin": 0, "endMin": 4, "label": "Whiteboard Brief, Stimulus & Scaling Check", "type": "brief", "notes": "Check for any wrist, shoulder, or knee limitations right away."},
				{"startMin": 4, "endMin": 16, "label": warmupStyle, "type": "warmup", "notes": "4m reaction game + 8m broomstick joint prep."},
				{"startMin": 16, "endMin": 20, "label": "Transition: Barbell / Station Setup", "type": "transition", "notes": "Assign 2 athletes per rack."},
				{"startMin": 20, "endMin": 35, "label": "Skill / Strength Block (15 min)", "type": "strength", "notes": "Every 2:30 x 6 working sets."},
				{"startMin": 35, "endMin": 40, "label": "Transition: Metcon Weight Load & 1-Round Rehearsal", "type": "transition", "notes": "Verify everyone's scaling option can be completed within the time cap."},
				{"startMin": 40, "endMin": dur - 6, "label": "Engine / Metcon Block", "type": "metcon", "notes": "Coach the room: external cues, pacing, and lumbar safety."},
				{"startMin": dur - 6, "endMin": dur, "label": "Cooldown, Fist Bumps & Equipment Reset", "type": "cooldown", "notes": "Guided breathing + thoracic/lat stretch."},
			},
		},
		"agentTraces": []map[string]interface{}{
			{"agent": "WodParserAgent", "provider": routes["parser"].Provider, "model": routes["parser"].Model, "status": "completed"},
			{"agent": "WarmupPlannerAgent", "provider": routes["warmup"].Provider, "model": routes["warmup"].Model, "mcpTool": "query_warmup_drills", "status": "completed"},
			{"agent": "ScalingAndCuesAgent", "provider": routes["scaling"].Provider, "model": routes["scaling"].Model, "mcpTool": "get_movement_substitutions", "status": "completed"},
			{"agent": "TimelineLogisticsAgent", "provider": routes["timeline"].Provider, "model": routes["timeline"].Model, "mcpTool": "validate_class_timeline", "status": "completed"},
		},
	}
}
