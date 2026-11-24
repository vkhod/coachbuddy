package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coachbuddy/gateway-go/internal/auth"
	"github.com/coachbuddy/gateway-go/internal/models"
)

func newTestAPIHandler(t *testing.T) (*APIHandler, *fakeStore) {
	t.Helper()
	ds := newFakeStore()
	tm, err := auth.NewTokenManager("test-jwt-secret-at-least-32-characters")
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	// Nothing listens on port 1, so calls to agents-python and mcp-rust fail
	// unless a test points the URLs at an httptest server.
	api := NewAPIHandler(ds, tm, "http://127.0.0.1:1", "http://127.0.0.1:1", "http://localhost:3000/s", "test-internal-token")
	return api, ds
}

// requestAs builds a request that has passed auth.Middleware for the given coach.
func requestAs(method, target, coachID, planID string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.SetPathValue("id", planID)
	return req.WithContext(context.WithValue(req.Context(), auth.CoachIDContextKey, coachID))
}

func TestListModels_ReturnsTheConfiguredList(t *testing.T) {
	api, _ := newTestAPIHandler(t)
	rr := httptest.NewRecorder()
	api.ListModels(rr, requestAs(http.MethodGet, "/api/v1/settings/models", "coach-alpha", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body struct {
		Providers []struct{ ID string } `json:"providers"`
		Models    []struct {
			ID      string `json:"id"`
			Default bool   `json:"default"`
		} `json:"models"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Providers) != 3 || body.Providers[0].ID != "gemini" {
		t.Errorf("providers: %+v", body.Providers)
	}
	if len(body.Models) == 0 || body.Models[0].ID != "gemini/gemini-3.8-flash" || !body.Models[0].Default {
		t.Errorf("models: %+v", body.Models)
	}
}

func TestListAgentRuns_OwnRunsOnlyAndLimitChecked(t *testing.T) {
	api, fs := newTestAPIHandler(t)
	ctx := context.Background()
	runs := []interface{}{map[string]interface{}{"agent": "parser", "section": "wod", "model": "gemini/gemini-3.8-flash", "state": "done"}}
	if err := fs.SaveAgentRuns(ctx, &models.AgentRuns{PlanID: "pl-a", CoachID: "coach-alpha", Revision: 1, Runs: runs}); err != nil {
		t.Fatal(err)
	}
	if err := fs.SaveAgentRuns(ctx, &models.AgentRuns{PlanID: "pl-b", CoachID: "coach-beta", Revision: 1, Runs: runs}); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	api.ListAgentRuns(rr, requestAs(http.MethodGet, "/api/v1/settings/runs", "coach-alpha", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body)
	}
	var body struct {
		Runs []models.AgentRunsRecord `json:"runs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Runs) != 1 || body.Runs[0].PlanID != "pl-a" {
		t.Errorf("coach-alpha should see only pl-a: %+v", body.Runs)
	}

	for _, q := range []string{"0", "51", "x"} {
		rr := httptest.NewRecorder()
		api.ListAgentRuns(rr, requestAs(http.MethodGet, "/api/v1/settings/runs?limit="+q, "coach-alpha", ""))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("limit=%s: expected 400, got %d", q, rr.Code)
		}
	}

	fs.fail = errors.New("mongo down")
	rr = httptest.NewRecorder()
	api.ListAgentRuns(rr, requestAs(http.MethodGet, "/api/v1/settings/runs", "coach-alpha", ""))
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("store failure: expected 500, got %d", rr.Code)
	}
}
