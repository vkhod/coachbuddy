package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coachbuddy/gateway-go/internal/auth"
	"github.com/coachbuddy/gateway-go/internal/modelcatalog"
	"github.com/coachbuddy/gateway-go/internal/models"
	"github.com/coachbuddy/gateway-go/internal/store"
	"github.com/google/uuid"
)

// Store is what the handlers need from storage. *store.DataStore implements it
// against PostgreSQL, MongoDB and Redis; the tests use an in-memory fake.
type Store interface {
	Ping(ctx context.Context) map[string]error

	CreateCoach(ctx context.Context, email, password, displayName, boxName string, defaultDuration int) (*models.Coach, error)
	AuthenticateCoach(ctx context.Context, email, password string) (*models.Coach, error)
	GetCoach(ctx context.Context, coachID string) (*models.Coach, error)
	UpdateCoachProfile(ctx context.Context, coachID, displayName, boxName string, defaultDuration int) (*models.Coach, error)

	SaveAPIKey(ctx context.Context, coachID, provider, rawKey string, isPrimary bool) (*models.LLMAPIKey, error)
	ListAPIKeys(ctx context.Context, coachID string) ([]*models.LLMAPIKey, error)
	DeleteAPIKey(ctx context.Context, coachID, keyID string) error
	GetAgentPrefs(ctx context.Context, coachID string) ([]*models.AgentModelPref, error)
	SaveAgentPrefs(ctx context.Context, coachID string, prefs []models.AgentModelPref) ([]*models.AgentModelPref, error)
	BuildEphemeralRoutingConfig(ctx context.Context, coachID string) (map[string]models.EphemeralAgentRoute, error)
	Models() *modelcatalog.Catalog

	SaveSession(ctx context.Context, sessionID string, doc map[string]interface{}) error
	GetSession(ctx context.Context, sessionID string) (map[string]interface{}, error)
	ListSessions(ctx context.Context, coachID string) ([]map[string]interface{}, error)
	DeleteSession(ctx context.Context, sessionID string) error
	SessionEvents(ctx context.Context, sessionID string) (*store.EventStream, error)

	SaveSharedLink(ctx context.Context, link *models.SharedLink) error
	GetSharedLink(ctx context.Context, code string) (*models.SharedLink, error)
}

type APIHandler struct {
	Store              Store
	Tokens             *auth.TokenManager
	AgentsServiceURL   string
	McpRustURL         string
	PublicShareBaseURL string
	InternalToken      string // shared secret sent to agents-python as X-Internal-Gateway-Token
	HttpClient         *http.Client
}

func NewAPIHandler(ds Store, tm *auth.TokenManager, agentsURL, mcpRustURL, shareBaseURL, internalToken string) *APIHandler {
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

// writeStoreError logs an unexpected storage error and answers 500 without
// exposing database details to the client.
func writeStoreError(w http.ResponseWriter, op string, err error) {
	log.Printf("[gateway] %s: %v", op, err)
	writeError(w, http.StatusInternalServerError, op+" failed")
}

// --- Health Check ---

// Health reports each dependency the gateway cannot work without. It answers
// 503 if any of them is down, so the container is marked unhealthy. The body
// says which one failed but not why; the reason is logged.
func (h *APIHandler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	results := h.Store.Ping(ctx)
	results["mcp-rust"] = h.pingMCPRust(ctx)

	checks := make(map[string]string, len(results))
	healthy := true
	for name, err := range results {
		if err != nil {
			healthy = false
			checks[name] = "unavailable"
			log.Printf("[gateway] health: %s: %v", name, err)
			continue
		}
		checks[name] = "ok"
	}
	status, code := "ok", http.StatusOK
	if !healthy {
		status, code = "unavailable", http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]interface{}{
		"status":  status,
		"service": "gateway-go",
		"checks":  checks,
	})
}

func (h *APIHandler) pingMCPRust(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.McpRustURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := h.HttpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
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
	switch {
	case errors.Is(err, store.ErrEmailTaken):
		writeError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, store.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		writeStoreError(w, "register", err)
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
	if errors.Is(err, store.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		writeStoreError(w, "login", err)
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
	if errors.Is(err, store.ErrCoachNotFound) {
		writeError(w, http.StatusNotFound, "coach not found")
		return
	}
	if err != nil {
		writeStoreError(w, "get coach", err)
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
		writeStoreError(w, "update profile", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"coach": coach})
}

// --- BYOK Encrypted API Key Vault & Agent Routing Settings ---

func (h *APIHandler) ListKeys(w http.ResponseWriter, r *http.Request) {
	coachID := auth.GetCoachID(r.Context())
	keys, err := h.Store.ListAPIKeys(r.Context(), coachID)
	if err != nil {
		writeStoreError(w, "list keys", err)
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
		req.Provider = h.Store.Models().Default().Provider
	}
	keyRec, err := h.Store.SaveAPIKey(r.Context(), coachID, req.Provider, req.APIKey, req.IsPrimary)
	if errors.Is(err, store.ErrInvalidInput) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeStoreError(w, "save key", err)
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
	err := h.Store.DeleteAPIKey(r.Context(), coachID, keyID)
	if errors.Is(err, store.ErrKeyNotFound) {
		writeError(w, http.StatusNotFound, "key not found")
		return
	}
	if err != nil {
		writeStoreError(w, "delete key", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *APIHandler) GetAgentPrefs(w http.ResponseWriter, r *http.Request) {
	coachID := auth.GetCoachID(r.Context())
	prefs, err := h.Store.GetAgentPrefs(r.Context(), coachID)
	if err != nil {
		writeStoreError(w, "get agent preferences", err)
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
	if errors.Is(err, store.ErrInvalidInput) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeStoreError(w, "save agent preferences", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"preferences": prefs})
}

// ListModels returns the providers and models a coach can choose (internal/modelcatalog).
func (h *APIHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.Store.Models())
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

	// The owner is always the authenticated coach, whatever the orchestrator sent.
	sessionDoc["coachId"] = coachID
	if err := h.Store.SaveSession(r.Context(), sessionID, sessionDoc); err != nil {
		writeStoreError(w, "save session", err)
		return
	}
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
		writeStoreError(w, "list sessions", err)
		return
	}
	if sessions == nil {
		sessions = []map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": sessions})
}

// getOwnedSession loads a session and checks that it belongs to the authenticated
// coach. A session owned by someone else is reported as not found, so its
// existence is not revealed. It writes the error response itself and returns
// false when the caller should stop.
func (h *APIHandler) getOwnedSession(w http.ResponseWriter, r *http.Request, sessionID string) (map[string]interface{}, bool) {
	coachID := auth.GetCoachID(r.Context())
	if coachID == "" {
		writeError(w, http.StatusNotFound, "session not found")
		return nil, false
	}
	doc, err := h.Store.GetSession(r.Context(), sessionID)
	if errors.Is(err, store.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, "session not found")
		return nil, false
	}
	if err != nil {
		writeStoreError(w, "get session", err)
		return nil, false
	}
	if owner, _ := doc["coachId"].(string); owner != coachID {
		writeError(w, http.StatusNotFound, "session not found")
		return nil, false
	}
	return doc, true
}

func (h *APIHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	doc, ok := h.getOwnedSession(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"session": doc})
}

func (h *APIHandler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if _, ok := h.getOwnedSession(w, r, sessionID); !ok {
		return
	}
	err := h.Store.DeleteSession(r.Context(), sessionID)
	if errors.Is(err, store.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if err != nil {
		writeStoreError(w, "delete session", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- SSE Progress Stream (Redis Pub/Sub) ---

func isCompletedEvent(evt string) bool {
	return strings.Contains(evt, `"step":"completed"`) || strings.Contains(evt, `"step": "completed"`)
}

func (h *APIHandler) StreamSessionEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	// Same rule as read, delete and share: another coach's session, or an unknown
	// one, is reported as not found before the stream opens.
	if _, ok := h.getOwnedSession(w, r, sessionID); !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	// Open the subscription before the stream, so a Redis failure is a plain
	// error response rather than an event stream that never says anything.
	events, err := h.Store.SessionEvents(ctx, sessionID)
	if err != nil {
		log.Printf("[gateway] session events %s: %v", sessionID, err)
		writeError(w, http.StatusServiceUnavailable, "session events unavailable")
		return
	}
	defer events.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for _, evt := range events.Buffered {
		fmt.Fprintf(w, "data: %s\n\n", evt)
		flusher.Flush()
		if isCompletedEvent(evt) {
			return
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-events.Live:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", evt)
			flusher.Flush()
			if isCompletedEvent(evt) {
				return
			}
		}
	}
}

// --- Share Session & Rust MCP SVG QR Code ---

func (h *APIHandler) CreateShareLink(w http.ResponseWriter, r *http.Request) {
	coachID := auth.GetCoachID(r.Context())
	sessionID := r.PathValue("id")

	if _, ok := h.getOwnedSession(w, r, sessionID); !ok {
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

	qrSVG, err := h.fetchQRFromRustMCP(r.Context(), shareURL)
	if err != nil {
		log.Printf("[gateway] QR code for %s: %v", sessionID, err)
		writeError(w, http.StatusBadGateway, "QR code service unavailable, try again")
		return
	}

	link := &models.SharedLink{
		ShortCode:         shortCode,
		ShareURL:          shareURL,
		MongoSessionID:    sessionID,
		CoachID:           coachID,
		IncludeCoachNotes: includeNotes,
		QRSVG:             qrSVG,
	}
	if err := h.Store.SaveSharedLink(r.Context(), link); err != nil {
		writeStoreError(w, "save share link", err)
		return
	}
	writeJSON(w, http.StatusCreated, link)
}

func (h *APIHandler) ResolveShareLink(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	link, err := h.Store.GetSharedLink(r.Context(), code)
	if errors.Is(err, store.ErrLinkNotFound) {
		writeError(w, http.StatusNotFound, "shared link expired or not found")
		return
	}
	if err != nil {
		writeStoreError(w, "resolve share link", err)
		return
	}
	link.ShareURL = fmt.Sprintf("%s/%s", h.PublicShareBaseURL, link.ShortCode)

	sessionDoc, err := h.Store.GetSession(r.Context(), link.MongoSessionID)
	if errors.Is(err, store.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, "underlying WOD session not found")
		return
	}
	if err != nil {
		writeStoreError(w, "resolve share link", err)
		return
	}

	// Work on a JSON copy so private coaching notes can be removed for athlete links.
	raw, err := json.Marshal(sessionDoc)
	if err != nil {
		writeStoreError(w, "resolve share link", err)
		return
	}
	var filtered map[string]interface{}
	if err := json.Unmarshal(raw, &filtered); err != nil {
		writeStoreError(w, "resolve share link", err)
		return
	}
	if !link.IncludeCoachNotes {
		sanitizeForAthletes(filtered)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"share":   link,
		"session": filtered,
	})
}

// sanitizeForAthletes removes private coaching content from a session shared
// with athletes: agent traces, coach warnings, block notes and exercise cues.
// Step 10 moves this to the new contract.
func sanitizeForAthletes(doc map[string]interface{}) {
	delete(doc, "agentTraces")
	delete(doc, "agentRuns")
	if tl, ok := doc["timeline"].(map[string]interface{}); ok {
		delete(tl, "coachWarnings")
		if blocks, ok := tl["blocks"].([]interface{}); ok {
			for _, b := range blocks {
				if bm, ok := b.(map[string]interface{}); ok {
					delete(bm, "notes")
				}
			}
		}
	}
	stripCues := func(list interface{}) {
		if exercises, ok := list.([]interface{}); ok {
			for _, ex := range exercises {
				if em, ok := ex.(map[string]interface{}); ok {
					delete(em, "cues")
				}
			}
		}
	}
	if scaling, ok := doc["scaling"].(map[string]interface{}); ok {
		stripCues(scaling["exercises"])
	}
	stripCues(doc["exercises"])
}

// fetchQRFromRustMCP asks mcp-rust for the QR code of a share URL. Any failure
// is returned; the gateway never draws a QR code itself.
func (h *APIHandler) fetchQRFromRustMCP(ctx context.Context, shareURL string) (string, error) {
	reqBody, err := json.Marshal(map[string]interface{}{"data": shareURL, "size": 256})
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.McpRustURL+"/tools/qr", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.HttpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mcp-rust /tools/qr: status %d", resp.StatusCode)
	}
	var parsed struct {
		SVG string `json:"svg"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("mcp-rust /tools/qr: %w", err)
	}
	if !strings.Contains(parsed.SVG, "<svg") {
		return "", errors.New("mcp-rust /tools/qr: response has no SVG")
	}
	return parsed.SVG, nil
}
