package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coachbuddy/gateway-go/internal/auth"
	"github.com/coachbuddy/gateway-go/internal/crypto"
	"github.com/coachbuddy/gateway-go/internal/models"
	"github.com/coachbuddy/gateway-go/internal/store"
)

func newTestAPIHandler(t *testing.T) (*APIHandler, *store.DataStore) {
	vault, err := crypto.NewVault("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("failed to create vault: %v", err)
	}
	ctx := context.Background()
	ds := store.NewDataStore(ctx, vault, "", "", "", "")
	tm := auth.NewTokenManager("test-jwt-secret-12345")
	api := NewAPIHandler(ds, tm, "http://localhost:8081", "http://localhost:8082", "http://localhost:3000/s")
	return api, ds
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
