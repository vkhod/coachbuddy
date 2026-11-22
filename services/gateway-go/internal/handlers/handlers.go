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
	"sync"
	"time"

	"github.com/coachbuddy/gateway-go/internal/auth"
	"github.com/coachbuddy/gateway-go/internal/modelcatalog"
	"github.com/coachbuddy/gateway-go/internal/models"
	"github.com/coachbuddy/gateway-go/internal/store"
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

	CreatePlan(ctx context.Context, doc map[string]interface{}) error
	SavePlan(ctx context.Context, planID string, doc map[string]interface{}) error
	AdvancePlan(ctx context.Context, planID string, from int, doc map[string]interface{}) error
	GetPlan(ctx context.Context, planID string) (map[string]interface{}, error)
	ListPlans(ctx context.Context, coachID string) ([]map[string]interface{}, error)
	DeletePlan(ctx context.Context, planID string) error
	SavePlanImage(ctx context.Context, img *models.PlanImage) error
	SaveAgentRuns(ctx context.Context, runs *models.AgentRuns) error
	SavePlanRevision(ctx context.Context, rev *models.PlanRevision) error
	ListPlanRevisions(ctx context.Context, planID string) ([]*models.PlanRevision, error)
	GetPlanRevision(ctx context.Context, planID string, revision int) (*models.PlanRevision, error)
	PlanEvents(ctx context.Context, planID string) (*store.EventStream, error)
	PublishPlanEvent(ctx context.Context, planID string, event []byte) error

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
	// GenerateTimeout bounds one plan generation in the background (the
	// orchestrator call). The parser alone has taken up to 100 s live.
	GenerateTimeout time.Duration

	jobs       sync.WaitGroup
	jobsCtx    context.Context
	cancelJobs context.CancelFunc
}

func NewAPIHandler(ds Store, tm *auth.TokenManager, agentsURL, mcpRustURL, shareBaseURL, internalToken string) *APIHandler {
	jobsCtx, cancel := context.WithCancel(context.Background())
	return &APIHandler{
		Store:              ds,
		Tokens:             tm,
		InternalToken:      internalToken,
		AgentsServiceURL:   strings.TrimRight(agentsURL, "/"),
		McpRustURL:         strings.TrimRight(mcpRustURL, "/"),
		PublicShareBaseURL: strings.TrimRight(shareBaseURL, "/"),
		// Short calls only (mcp-rust). The orchestrator call is bounded by GenerateTimeout.
		HttpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		GenerateTimeout: 5 * time.Minute,
		jobsCtx:         jobsCtx,
		cancelJobs:      cancel,
	}
}

// Shutdown stops the plan generations still running and waits until each has
// recorded its plan as failed, or until ctx ends. Called when the gateway stops,
// so no plan is left "pending" for ever.
func (h *APIHandler) Shutdown(ctx context.Context) error {
	h.cancelJobs()
	done := make(chan struct{})
	go func() { h.jobs.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
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

// --- Rust MCP SVG QR Code ---

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
