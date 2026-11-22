package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coachbuddy/gateway-go/internal/models"
)

// resolveShare saves the example plan with a share link and resolves it as an
// anonymous visitor would. It returns the plan part of the answer and the raw body.
func resolveShare(t *testing.T, example string, coachView bool) (map[string]interface{}, string) {
	t.Helper()
	api, ds := newTestAPIHandler(t)
	doc := examplePlan(t, example)
	if err := ds.CreatePlan(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	if err := ds.SaveSharedLink(t.Context(), &models.SharedLink{
		ShortCode: "cb-test", PlanID: doc["planId"].(string), CoachID: doc["coachId"].(string), IncludeCoachNotes: coachView,
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/share/cb-test", nil)
	req.SetPathValue("code", "cb-test")
	rr := httptest.NewRecorder()
	api.ResolveShareLink(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	plan, _ := decodeBody(t, rr)["plan"].(map[string]interface{})
	return plan, rr.Body.String()
}

// Words from the example's comment and injury blocks. None may reach a share
// link in either view, except the injury blocks in the coach view.
var commentWords = []string{"senior athlete today. ankle injury", "img_91c"}

func TestShare_AthleteViewHasOnlyTheWodTimelineAndTiers(t *testing.T) {
	plan, body := resolveShare(t, "2026-11-18-senior-two-injuries.json", false)

	for _, k := range []string{"input", "injuries", "today", "warmup", "coachId", "agentRuns"} {
		if _, has := plan[k]; has {
			t.Errorf("athlete view must not contain %q", k)
		}
	}
	for _, w := range append(commentWords, "Ankle", "Shoulder", "Suggestion only") {
		if strings.Contains(body, w) {
			t.Errorf("athlete view leaks %q", w)
		}
	}
	for _, k := range []string{"wod", "parts", "timeline", "sectionStatus"} {
		if plan[k] == nil {
			t.Errorf("athlete view needs %q", k)
		}
	}
	part := plan["parts"].([]interface{})[0].(map[string]interface{})
	if _, has := part["cues"]; has {
		t.Error("cues are for coaches")
	}
	if part["tiers"].(map[string]interface{})["rx"].(map[string]interface{})["line"] == "" {
		t.Error("tier lines must be shared")
	}
	if blocks := plan["timeline"].(map[string]interface{})["blocks"].([]interface{}); len(blocks) == 0 {
		t.Error("timeline blocks must be shared")
	}
}

func TestShare_CoachViewAddsCoachingButNeverTheInput(t *testing.T) {
	plan, body := resolveShare(t, "2026-11-18-senior-two-injuries.json", true)

	for _, k := range []string{"injuries", "today", "warmup"} {
		if plan[k] == nil {
			t.Errorf("coach view needs %q", k)
		}
	}
	if part := plan["parts"].([]interface{})[0].(map[string]interface{}); part["cues"] == nil {
		t.Error("coach view keeps the cues")
	}
	if _, has := plan["input"]; has {
		t.Error("the comment and screenshot reference are never shared")
	}
	for _, w := range commentWords {
		if strings.Contains(body, w) {
			t.Errorf("coach view leaks %q", w)
		}
	}
}

func TestShare_SectionErrorsAreNotShared(t *testing.T) {
	plan, body := resolveShare(t, "2026-11-18-warmup-failed.json", true)
	st := plan["sectionStatus"].(map[string]interface{})
	if st["warmup"].(map[string]interface{})["state"] != "failed" {
		t.Errorf("states are shared: %v", st)
	}
	if strings.Contains(body, "repair retry") {
		t.Error("section errors must not be shared")
	}
}

func TestCreateShareLink_DefaultsToTheAthleteView(t *testing.T) {
	for body, want := range map[string]bool{"": false, `{}`: false, `{"includeCoachNotes":true}`: true} {
		api, ds := newTestAPIHandler(t)
		api.McpRustURL = mcpServer(t, http.StatusOK, `{"svg":"<svg/>"}`).URL
		saveOwned(t, ds, "p1", "coach-alpha")
		req := requestAs(http.MethodPost, "/api/v1/plans/p1/share", "coach-alpha", "p1")
		req.Body = io.NopCloser(strings.NewReader(body))
		rr := httptest.NewRecorder()
		api.CreateShareLink(rr, req)
		var link models.SharedLink
		if err := json.Unmarshal(rr.Body.Bytes(), &link); err != nil || rr.Code != http.StatusCreated {
			t.Fatalf("%q: %d %s", body, rr.Code, rr.Body.String())
		}
		if link.IncludeCoachNotes != want || link.PlanID != "p1" {
			t.Errorf("body %q: includeCoachNotes %v, want %v", body, link.IncludeCoachNotes, want)
		}
	}
}
