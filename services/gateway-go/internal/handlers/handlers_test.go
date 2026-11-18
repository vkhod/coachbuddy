package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
func requestAs(method, target, coachID, sessionID string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.SetPathValue("id", sessionID)
	return req.WithContext(context.WithValue(req.Context(), auth.CoachIDContextKey, coachID))
}

func TestSessionEndpoints_RejectOtherCoaches(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	ctx := context.Background()
	sessionID := "session-owned-by-alpha"
	if err := ds.SaveSession(ctx, sessionID, map[string]interface{}{
		"_id": sessionID, "sessionId": sessionID, "coachId": "coach-alpha", "title": "Alpha WOD",
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	cases := []struct {
		name    string
		method  string
		handler http.HandlerFunc
	}{
		{"get", http.MethodGet, api.GetSession},
		{"share", http.MethodPost, api.CreateShareLink},
		{"delete", http.MethodDelete, api.DeleteSession},
	}
	for _, c := range cases {
		rr := httptest.NewRecorder()
		c.handler(rr, requestAs(c.method, "/api/v1/sessions/"+sessionID, "coach-beta", sessionID))
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s by another coach: expected 404, got %d", c.name, rr.Code)
		}
	}

	if _, err := ds.GetSession(ctx, sessionID); err != nil {
		t.Fatalf("session must still exist after another coach tried to delete it: %v", err)
	}

	rr := httptest.NewRecorder()
	api.GetSession(rr, requestAs(http.MethodGet, "/api/v1/sessions/"+sessionID, "coach-alpha", sessionID))
	if rr.Code != http.StatusOK {
		t.Errorf("get by owner: expected 200, got %d", rr.Code)
	}
}

func TestResolveShareLink_SanitizesAthleteMode(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	ctx := context.Background()

	sessionID := "session-test-share-123"
	_ = ds.SaveSession(ctx, sessionID, map[string]interface{}{
		"_id":       sessionID,
		"sessionId": sessionID,
		"coachId":   "coach-1",
		"title":     "Strict Muscle-Up Progression",
		"agentTraces": map[string]interface{}{
			"promptTokens": 1500,
			"rawOutput":    "internal debug text",
		},
		"timeline": map[string]interface{}{
			"coachWarnings": []interface{}{
				"Watch athlete shoulder mobility on high rings",
			},
			"blocks": []interface{}{
				map[string]interface{}{
					"startMin": 0,
					"endMin":   10,
					"label":    "Warmup",
					"notes":    "Coach note: prioritize false grip prep",
				},
			},
		},
		"scaling": map[string]interface{}{
			"exercises": []interface{}{
				map[string]interface{}{
					"name": "Strict Muscle-Up",
					"cues": "Aggressive head-through transition",
				},
			},
		},
	})

	// 1. Create Athlete Share Link (IncludeCoachNotes = false)
	linkCode := "athlete-code-123"
	_ = ds.SaveSharedLink(ctx, &models.SharedLink{
		ShortCode:         linkCode,
		MongoSessionID:    sessionID,
		CoachID:           "coach-1",
		IncludeCoachNotes: false,
		ViewCount:         0,
		CreatedAt:         time.Now(),
	})

	req := httptest.NewRequest("GET", "/api/v1/share/"+linkCode, nil)
	req.SetPathValue("code", linkCode)
	rr := httptest.NewRecorder()

	api.ResolveShareLink(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Share   models.SharedLink      `json:"share"`
		Session map[string]interface{} `json:"session"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	session := resp.Session

	// A. agentTraces MUST be stripped
	if _, exists := session["agentTraces"]; exists {
		t.Errorf("SECURITY: agentTraces was not stripped in Athlete mode")
	}

	// B. coachWarnings MUST be stripped
	tl := session["timeline"].(map[string]interface{})
	if _, exists := tl["coachWarnings"]; exists {
		t.Errorf("SECURITY: timeline.coachWarnings was not stripped in Athlete mode")
	}

	// C. block notes MUST be stripped
	blocks := tl["blocks"].([]interface{})
	block0 := blocks[0].(map[string]interface{})
	if _, exists := block0["notes"]; exists {
		t.Errorf("SECURITY: block.notes was not stripped in Athlete mode")
	}

	// D. exercise cues MUST be stripped
	scaling := session["scaling"].(map[string]interface{})
	exercises := scaling["exercises"].([]interface{})
	ex0 := exercises[0].(map[string]interface{})
	if _, exists := ex0["cues"]; exists {
		t.Errorf("SECURITY: exercise cues was not stripped in Athlete mode")
	}
}

func TestResolveShareLink_PreservesCoachModeNotes(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	ctx := context.Background()

	sessionID := "session-test-share-coach"
	_ = ds.SaveSession(ctx, sessionID, map[string]interface{}{
		"_id":       sessionID,
		"sessionId": sessionID,
		"coachId":   "coach-1",
		"title":     "Coach View Session",
		"timeline": map[string]interface{}{
			"coachWarnings": []interface{}{
				"Ensure rings are securely anchored",
			},
			"blocks": []interface{}{
				map[string]interface{}{
					"startMin": 0,
					"endMin":   10,
					"label":    "Warmup",
					"notes":    "Keep heart rates low",
				},
			},
		},
	})

	linkCode := "coach-code-789"
	_ = ds.SaveSharedLink(ctx, &models.SharedLink{
		ShortCode:         linkCode,
		MongoSessionID:    sessionID,
		CoachID:           "coach-1",
		IncludeCoachNotes: true, // Coach mode!
		ViewCount:         0,
		CreatedAt:         time.Now(),
	})

	req := httptest.NewRequest("GET", "/api/v1/share/"+linkCode, nil)
	req.SetPathValue("code", linkCode)
	rr := httptest.NewRecorder()

	api.ResolveShareLink(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}

	var resp struct {
		Session map[string]interface{} `json:"session"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	tl := resp.Session["timeline"].(map[string]interface{})
	if _, exists := tl["coachWarnings"]; !exists {
		t.Errorf("expected coachWarnings to be retained in Coach mode")
	}
}

func TestStreamSessionEvents_RejectsOtherCoachesAndUnknownSessions(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	ctx := context.Background()
	sessionID := "session-events-alpha"
	if err := ds.SaveSession(ctx, sessionID, map[string]interface{}{
		"_id": sessionID, "sessionId": sessionID, "coachId": "coach-alpha",
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	cases := []struct {
		name, coachID, sessionID string
	}{
		{"another coach", "coach-beta", sessionID},
		{"unknown session", "coach-alpha", "session-does-not-exist"},
	}
	for _, c := range cases {
		rr := httptest.NewRecorder()
		api.StreamSessionEvents(rr, requestAs(http.MethodGet, "/api/v1/sessions/"+c.sessionID+"/events", c.coachID, c.sessionID))
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", c.name, rr.Code)
		}
		if ct := rr.Header().Get("Content-Type"); ct == "text/event-stream" {
			t.Errorf("%s: the event stream must not open, got Content-Type %q", c.name, ct)
		}
	}

	rr := httptest.NewRecorder()
	api.StreamSessionEvents(rr, requestAs(http.MethodGet, "/api/v1/sessions/"+sessionID+"/events", "coach-alpha", sessionID))
	if rr.Code != http.StatusOK {
		t.Errorf("owner: expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("owner: expected text/event-stream, got %q", ct)
	}
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

func TestShareForAthletes_DropsAgentRuns(t *testing.T) {
	doc := map[string]interface{}{"agentRuns": []interface{}{map[string]interface{}{"agent": "WodParserAgent"}}, "wod": map[string]interface{}{}}
	sanitizeForAthletes(doc)
	if _, ok := doc["agentRuns"]; ok {
		t.Error("agent runs must not reach athletes")
	}
}
