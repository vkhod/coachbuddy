package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coachbuddy/gateway-go/internal/auth"
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
