package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A 1x1 PNG, as the browser would send it.
const tinyPNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func createBody(comment string) string {
	b, _ := json.Marshal(map[string]interface{}{"imageBase64": tinyPNG, "comment": comment, "classMinutes": 60})
	return string(b)
}

// pendingPlan is a plan as the gateway saves it before the 202.
func pendingPlan(id, coach string) map[string]interface{} {
	status := map[string]interface{}{}
	for _, s := range planSections {
		status[s] = map[string]interface{}{"state": "pending"}
	}
	return map[string]interface{}{
		"planId": id, "coachId": coach, "revision": 1,
		"input":         map[string]interface{}{"imageRef": "img_" + id, "classMinutes": 60},
		"sectionStatus": status,
		"createdAt":     "2026-11-20T09:00:00.000Z",
	}
}

// examplePlan loads a valid plan from schema/examples, the data every service checks.
func examplePlan(t *testing.T, name string) map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "schema", "examples", "plan", "valid", name))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func withStatus(doc map[string]interface{}, section, state string) map[string]interface{} {
	cp := copyDoc(doc)
	cp["sectionStatus"].(map[string]interface{})[section] = map[string]interface{}{"state": state}
	return cp
}

func sectionEvent(planID, section, state string, plan map[string]interface{}) []byte {
	b, _ := json.Marshal(map[string]interface{}{"planId": planID, "revision": 1, "section": section, "state": state, "plan": plan})
	return b
}

// fakeAgents stands in for agents-python. handle gets the decoded orchestrate
// request and writes the answer; it may publish section events through ds first.
func fakeAgents(t *testing.T, handle func(w http.ResponseWriter, req map[string]interface{})) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Gateway-Token") != "test-internal-token" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)
		handle(w, req)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// finalPlan builds what agents-python returns: the example plan for this request, plus agentRuns.
func finalPlan(t *testing.T, req map[string]interface{}) map[string]interface{} {
	doc := examplePlan(t, "2026-11-18-senior-two-injuries.json")
	doc["planId"] = req["planId"]
	doc["coachId"] = "someone-else" // the gateway must not trust this
	doc["input"].(map[string]interface{})["imageRef"] = req["imageRef"]
	doc["createdAt"] = "2030-01-01T00:00:00.000Z" // nor this
	doc["agentRuns"] = []interface{}{map[string]interface{}{"agent": "WodParserAgent", "model": "gemini/gemini-3.8-flash"}}
	doc["orchestrationLatencyMs"] = 4200
	return doc
}

func startPlan(t *testing.T, api *APIHandler, coach, body string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	req := requestAs(http.MethodPost, "/api/v1/plans", coach, "")
	req.Body = io.NopCloser(strings.NewReader(body))
	rr := httptest.NewRecorder()
	api.CreatePlan(rr, req)
	id, _ := decodeBody(t, rr)["planId"].(string)
	return rr, id
}

func states(doc map[string]interface{}) map[string]string {
	out := map[string]string{}
	for k, v := range doc["sectionStatus"].(map[string]interface{}) {
		out[k], _ = v.(map[string]interface{})["state"].(string)
	}
	return out
}

func lastEvent(t *testing.T, ds *fakeStore, id string) map[string]interface{} {
	t.Helper()
	ds.mu.Lock()
	defer ds.mu.Unlock()
	evs := ds.events[id]
	if len(evs) == 0 {
		t.Fatal("no events published")
	}
	var e map[string]interface{}
	_ = json.Unmarshal([]byte(evs[len(evs)-1]), &e)
	return e
}

func TestCreatePlan_Answers202AndSavesTheOwnerBeforeGenerating(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	release := make(chan struct{})
	api.AgentsServiceURL = fakeAgents(t, func(w http.ResponseWriter, req map[string]interface{}) {
		<-release
		_ = json.NewEncoder(w).Encode(finalPlan(t, req))
	}).URL

	rr, id := startPlan(t, api, "coach-alpha", createBody("ankle injury"))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.HasPrefix(id, "pl_") || decodeBody(t, rr)["eventsUrl"] != "/api/v1/plans/"+id+"/events" {
		t.Errorf("202 body: %s", rr.Body.String())
	}

	// While the agents are still working: the plan exists, owned by the caller, all pending.
	doc, err := ds.GetPlan(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if doc["coachId"] != "coach-alpha" {
		t.Errorf("owner: %v", doc["coachId"])
	}
	for s, st := range states(doc) {
		if st != "pending" {
			t.Errorf("%s: %s, want pending", s, st)
		}
	}
	input := doc["input"].(map[string]interface{})
	ref, _ := input["imageRef"].(string)
	if img := ds.images[ref]; img == nil || img.MimeType != "image/png" || len(img.Data) == 0 || img.CoachID != "coach-alpha" {
		t.Errorf("screenshot not stored under input.imageRef %q: %+v", ref, img)
	}
	if input["comment"] != "ankle injury" {
		t.Errorf("comment: %v", input["comment"])
	}
	if strings.Contains(rr.Body.String(), "iVBOR") {
		t.Error("the 202 must not echo the screenshot")
	}

	// The owner check on events already holds while generating.
	other := httptest.NewRecorder()
	api.StreamPlanEvents(other, requestAs(http.MethodGet, "/api/v1/plans/"+id+"/events", "coach-beta", id))
	if other.Code != http.StatusNotFound {
		t.Errorf("events for another coach while generating: %d, want 404", other.Code)
	}

	close(release)
	api.jobs.Wait()
}

func TestCreatePlan_BadInputIs400AndSavesNothing(t *testing.T) {
	cases := map[string]string{
		"not json":        `{`,
		"no screenshot":   `{"classMinutes":60}`,
		"class too short": `{"imageBase64":"` + tinyPNG + `","classMinutes":10}`,
		"unknown field":   `{"imageBase64":"` + tinyPNG + `","classMinutes":60,"rawText":"EMOM 12"}`,
		"gif":             `{"imageBase64":"data:image/gif;base64,R0lGODlh","classMinutes":60}`,
		"not base64":      `{"imageBase64":"data:image/png;base64,***","classMinutes":60}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			api, ds := newTestAPIHandler(t)
			rr, _ := startPlan(t, api, "coach-alpha", body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
			}
			if len(ds.plans) != 0 || len(ds.images) != 0 {
				t.Error("nothing may be saved for bad input")
			}
		})
	}
}

func TestCreatePlan_NoUsableKeyIs400(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	ds.fail = errDBDown // BuildEphemeralRoutingConfig fails first
	rr, _ := startPlan(t, api, "coach-alpha", createBody(""))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestCreatePlan_DatabaseErrorIs500AndStartsNothing(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	called := false
	api.AgentsServiceURL = fakeAgents(t, func(w http.ResponseWriter, _ map[string]interface{}) { called = true }).URL
	ds.createPlanErr = errDBDown
	rr, _ := startPlan(t, api, "coach-alpha", createBody(""))
	api.jobs.Wait()
	if rr.Code != http.StatusInternalServerError || strings.Contains(rr.Body.String(), errDBDown.Error()) {
		t.Fatalf("expected a plain 500, got %d: %s", rr.Code, rr.Body.String())
	}
	if called {
		t.Error("no generation may start when the plan was not saved")
	}
}

func TestGenerate_StoresProgressThenTheFinalPlanAndAgentRuns(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	var orchestrate map[string]interface{}
	api.AgentsServiceURL = fakeAgents(t, func(w http.ResponseWriter, req map[string]interface{}) {
		orchestrate = req
		id := req["planId"].(string)
		// The parser finished: agents-python publishes a snapshot with the WOD.
		snap := pendingPlan(id, "coach-alpha")
		snap["input"].(map[string]interface{})["imageRef"] = req["imageRef"]
		snap["wod"] = examplePlan(t, "2026-11-18-senior-two-injuries.json")["wod"]
		snap = withStatus(snap, "wod", "done")
		_ = ds.PublishPlanEvent(context.Background(), id, sectionEvent(id, "wod", "done", snap))
		// Wait until the gateway stored it, as it would between two sections.
		deadline := time.Now().Add(2 * time.Second)
		for {
			ds.mu.Lock()
			n := len(ds.savedPlans)
			ds.mu.Unlock()
			if n > 0 || time.Now().After(deadline) {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		_ = json.NewEncoder(w).Encode(finalPlan(t, req))
	}).URL

	_, id := startPlan(t, api, "coach-alpha", createBody("senior athlete today"))
	api.jobs.Wait()

	// The orchestrate request: the contract's CreatePlanRequest plus the internal fields.
	in, _ := orchestrate["input"].(map[string]interface{})
	if orchestrate["planId"] != id || orchestrate["coachId"] != "coach-alpha" || in["imageBase64"] != tinyPNG || in["classMinutes"] != float64(60) {
		t.Errorf("orchestrate request: %v", orchestrate)
	}
	if _, ok := orchestrate["routingConfig"].(map[string]interface{})["global"]; !ok {
		t.Error("routingConfig missing")
	}

	if len(ds.savedPlans) < 2 {
		t.Fatalf("expected a progress save and the final save, got %d", len(ds.savedPlans))
	}
	progress := ds.savedPlans[0]
	if states(progress)["wod"] != "done" || progress["wod"] == nil || progress["coachId"] != "coach-alpha" {
		t.Errorf("progress snapshot: %v", progress["sectionStatus"])
	}

	doc, _ := ds.GetPlan(t.Context(), id)
	if doc["coachId"] != "coach-alpha" {
		t.Errorf("final owner must be the caller, got %v", doc["coachId"])
	}
	if doc["createdAt"] != progress["createdAt"] || doc["createdAt"] == "2030-01-01T00:00:00.000Z" {
		t.Errorf("createdAt is the gateway's: %v", doc["createdAt"])
	}
	if _, has := doc["agentRuns"]; has {
		t.Error("agentRuns must move to agent_runs, not stay in the plan")
	}
	if _, has := doc["orchestrationLatencyMs"]; has {
		t.Error("orchestrationLatencyMs belongs with the agent runs")
	}
	if err := validPlan(doc); err != nil {
		t.Errorf("stored plan does not match the contract: %v", err)
	}
	runs := ds.runs[id]
	if runs == nil || runs.CoachID != "coach-alpha" || runs.Revision != 1 || runs.OrchestrationLatencyMs != float64(4200) {
		t.Errorf("agent runs: %+v", runs)
	}

	end := lastEvent(t, ds, id)
	if end["section"] != "plan" || end["state"] != "done" || end["plan"] == nil {
		t.Errorf("closing event: %v", end)
	}
	if _, has := end["plan"].(map[string]interface{})["agentRuns"]; has {
		t.Error("the closing event must not carry agent runs")
	}
}

func TestGenerate_FailureMarksUnfinishedSectionsFailed(t *testing.T) {
	cases := []struct {
		name, want string
		handle     func(t *testing.T, ds *fakeStore) func(http.ResponseWriter, map[string]interface{})
		agentsURL  string
		timeout    time.Duration
	}{
		{name: "unreachable", want: "the plan service is unreachable", agentsURL: "http://127.0.0.1:1"},
		{name: "500 hides the body", want: "the plan service failed (status 500)", handle: func(*testing.T, *fakeStore) func(http.ResponseWriter, map[string]interface{}) {
			return func(w http.ResponseWriter, _ map[string]interface{}) {
				http.Error(w, "traceback with internals", http.StatusInternalServerError)
			}
		}},
		{name: "400 says why", want: "no API key for the warmup role", handle: func(*testing.T, *fakeStore) func(http.ResponseWriter, map[string]interface{}) {
			return func(w http.ResponseWriter, _ map[string]interface{}) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"detail":"no API key for the warmup role"}`))
			}
		}},
		{name: "invalid plan", want: "does not match the contract", handle: func(t *testing.T, _ *fakeStore) func(http.ResponseWriter, map[string]interface{}) {
			return func(w http.ResponseWriter, req map[string]interface{}) {
				doc := finalPlan(t, req)
				doc["parts"].([]interface{})[0].(map[string]interface{})["cues"] = []interface{}{"a", "b", "c", "d"}
				_ = json.NewEncoder(w).Encode(doc)
			}
		}},
		{name: "timeout", want: "took longer than", timeout: 50 * time.Millisecond, handle: func(*testing.T, *fakeStore) func(http.ResponseWriter, map[string]interface{}) {
			return func(w http.ResponseWriter, _ map[string]interface{}) { time.Sleep(500 * time.Millisecond) }
		}},
		{name: "after the parser", want: "the plan service failed (status 502)", handle: func(t *testing.T, ds *fakeStore) func(http.ResponseWriter, map[string]interface{}) {
			return func(w http.ResponseWriter, req map[string]interface{}) {
				id := req["planId"].(string)
				doc, _ := ds.GetPlan(context.Background(), id)
				doc["wod"] = examplePlan(t, "2026-11-18-senior-two-injuries.json")["wod"]
				_ = ds.SavePlan(context.Background(), id, withStatus(doc, "wod", "done"))
				w.WriteHeader(http.StatusBadGateway)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api, ds := newTestAPIHandler(t)
			if c.timeout > 0 {
				api.GenerateTimeout = c.timeout
			}
			api.AgentsServiceURL = c.agentsURL
			if c.handle != nil {
				api.AgentsServiceURL = fakeAgents(t, c.handle(t, ds)).URL
			}
			_, id := startPlan(t, api, "coach-alpha", createBody(""))
			api.jobs.Wait()

			doc, _ := ds.GetPlan(t.Context(), id)
			st := doc["sectionStatus"].(map[string]interface{})
			for _, s := range planSections {
				if c.name == "after the parser" && s == "wod" {
					if states(doc)["wod"] != "done" || doc["wod"] == nil {
						t.Error("a section that finished must be kept")
					}
					continue
				}
				m := st[s].(map[string]interface{})
				if m["state"] != "failed" || !strings.Contains(m["error"].(string), c.want) {
					t.Errorf("%s: %v, want failed with %q", s, m, c.want)
				}
				if strings.Contains(m["error"].(string), "traceback") {
					t.Error("the agents' error body must not reach the coach")
				}
			}
			if _, has := doc["warmup"]; has {
				t.Error("no section content may appear for a failed generation")
			}
			end := lastEvent(t, ds, id)
			if end["section"] != "plan" || end["state"] != "failed" || !strings.Contains(end["error"].(string), c.want) {
				t.Errorf("closing event: %v", end)
			}
		})
	}
}

func TestGenerate_PlanDeletedWhileRunningIsNotBroughtBack(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	api.AgentsServiceURL = fakeAgents(t, func(w http.ResponseWriter, req map[string]interface{}) {
		_ = ds.DeletePlan(context.Background(), req["planId"].(string))
		_ = json.NewEncoder(w).Encode(finalPlan(t, req))
	}).URL
	_, id := startPlan(t, api, "coach-alpha", createBody(""))
	api.jobs.Wait()
	if _, err := ds.GetPlan(t.Context(), id); err == nil {
		t.Error("a deleted plan must stay deleted")
	}
}

func TestShutdown_RecordsRunningPlansAsFailed(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	started := make(chan struct{})
	var once sync.Once
	api.AgentsServiceURL = fakeAgents(t, func(w http.ResponseWriter, _ map[string]interface{}) {
		once.Do(func() { close(started) })
		time.Sleep(2 * time.Second)
	}).URL
	_, id := startPlan(t, api, "coach-alpha", createBody(""))
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := api.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	doc, _ := ds.GetPlan(t.Context(), id)
	m := doc["sectionStatus"].(map[string]interface{})["timeline"].(map[string]interface{})
	if m["state"] != "failed" || !strings.Contains(m["error"].(string), "restarted") {
		t.Errorf("timeline after shutdown: %v", m)
	}
}

// --- Reading plans and events ---

func TestPlanEndpoints_RejectOtherCoaches(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	saveOwned(t, ds, "pl_alpha", "coach-alpha")

	for name, h := range map[string]http.HandlerFunc{
		"get": api.GetPlan, "share": api.CreateShareLink, "delete": api.DeletePlan, "events": api.StreamPlanEvents,
	} {
		rr := httptest.NewRecorder()
		h(rr, requestAs(http.MethodGet, "/api/v1/plans/pl_alpha", "coach-beta", "pl_alpha"))
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s by another coach: expected 404, got %d", name, rr.Code)
		}
		if rr.Header().Get("Content-Type") == "text/event-stream" {
			t.Errorf("%s: the event stream must not open", name)
		}
	}
	if _, err := ds.GetPlan(t.Context(), "pl_alpha"); err != nil {
		t.Fatal("the plan must survive another coach's delete")
	}

	rr := httptest.NewRecorder()
	api.GetPlan(rr, requestAs(http.MethodGet, "/api/v1/plans/pl_alpha", "coach-alpha", "pl_alpha"))
	if rr.Code != http.StatusOK || decodeBody(t, rr)["plan"].(map[string]interface{})["planId"] != "pl_alpha" {
		t.Errorf("get by owner: %d %s", rr.Code, rr.Body.String())
	}

	saveOwned(t, ds, "pl_beta", "coach-beta")
	rr = httptest.NewRecorder()
	api.ListPlans(rr, requestAs(http.MethodGet, "/api/v1/plans", "coach-alpha", ""))
	plans := decodeBody(t, rr)["plans"].([]interface{})
	if len(plans) != 1 || plans[0].(map[string]interface{})["planId"] != "pl_alpha" {
		t.Errorf("list must hold only the caller's plans: %v", plans)
	}
}

func TestPlanRead_DatabaseErrorIs500Not404(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	ds.fail = errDBDown
	for name, h := range map[string]http.HandlerFunc{
		"get": api.GetPlan, "delete": api.DeletePlan, "events": api.StreamPlanEvents, "share": api.CreateShareLink, "list": api.ListPlans,
	} {
		rr := httptest.NewRecorder()
		h(rr, requestAs(http.MethodGet, "/api/v1/plans/p1", "coach-alpha", "p1"))
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("%s: expected 500 when the database is down, got %d", name, rr.Code)
		}
	}
}

func TestStreamPlanEvents_RedisFailureIsNotAStream(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	saveOwned(t, ds, "p1", "coach-alpha")
	ds.eventsErr = errDBDown

	rr := httptest.NewRecorder()
	api.StreamPlanEvents(rr, requestAs(http.MethodGet, "/api/v1/plans/p1/events", "coach-alpha", "p1"))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
	if rr.Header().Get("Content-Type") == "text/event-stream" || strings.Contains(rr.Body.String(), `"done"`) {
		t.Error("a Redis failure must not open a stream or report the plan done")
	}
}

func TestStreamPlanEvents_ReplaysUntilThePlanEvent(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	saveOwned(t, ds, "p1", "coach-alpha")
	ds.events["p1"] = []string{
		`{"planId":"p1","revision":1,"section":"wod","state":"running"}`,
		`{"planId":"p1","revision":1,"section":"wod","state":"done"}`,
		`{"planId":"p1","revision":1,"section":"plan","state":"failed","error":"x"}`,
		`{"planId":"p1","revision":1,"section":"wod","state":"after"}`,
	}
	rr := httptest.NewRecorder()
	api.StreamPlanEvents(rr, requestAs(http.MethodGet, "/api/v1/plans/p1/events", "coach-alpha", "p1"))

	want := "data: " + ds.events["p1"][0] + "\n\ndata: " + ds.events["p1"][1] + "\n\ndata: " + ds.events["p1"][2] + "\n\n"
	if rr.Body.String() != want {
		t.Errorf("stream body:\n got %q\nwant %q", rr.Body.String(), want)
	}
	if rr.Header().Get("Content-Type") != "text/event-stream" || rr.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("headers: %v", rr.Header())
	}
}

func TestStreamPlanEvents_FinishedPlanSendsOneClosingEvent(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	doc := examplePlan(t, "2026-11-18-senior-two-injuries.json")
	doc["coachId"] = "coach-alpha"
	if err := ds.CreatePlan(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	ds.eventsErr = errDBDown // the replay list has expired; Redis is not needed

	rr := httptest.NewRecorder()
	id := doc["planId"].(string)
	api.StreamPlanEvents(rr, requestAs(http.MethodGet, "/api/v1/plans/"+id+"/events", "coach-alpha", id))
	body := strings.TrimSuffix(strings.TrimPrefix(rr.Body.String(), "data: "), "\n\n")
	var evt map[string]interface{}
	if err := json.Unmarshal([]byte(body), &evt); err != nil {
		t.Fatalf("one event expected, got %q", rr.Body.String())
	}
	if evt["section"] != "plan" || evt["state"] != "done" || evt["plan"].(map[string]interface{})["wod"] == nil {
		t.Errorf("closing event: %v", evt)
	}
}

func TestStreamPlanEvents_FinishedPlanReplaysTheRealClosingEvent(t *testing.T) {
	// Generation failed before the coach opened the stream: the replay list
	// still holds the gateway's "failed" event, and that is what the coach gets.
	api, ds := newTestAPIHandler(t)
	_, id := startPlan(t, api, "coach-alpha", createBody(""))
	api.jobs.Wait()

	rr := httptest.NewRecorder()
	api.StreamPlanEvents(rr, requestAs(http.MethodGet, "/api/v1/plans/"+id+"/events", "coach-alpha", id))
	if !strings.Contains(rr.Body.String(), `"section":"plan"`) || !strings.Contains(rr.Body.String(), `"state":"failed"`) {
		t.Errorf("stream: %s", rr.Body.String())
	}

	// Once the replay list has expired, the rebuilt event still says failed.
	ds.mu.Lock()
	delete(ds.events, id)
	ds.mu.Unlock()
	rr = httptest.NewRecorder()
	api.StreamPlanEvents(rr, requestAs(http.MethodGet, "/api/v1/plans/"+id+"/events", "coach-alpha", id))
	var evt map[string]interface{}
	_ = json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(rr.Body.String(), "data: "))), &evt)
	if evt["section"] != "plan" || evt["state"] != "failed" || !strings.Contains(evt["error"].(string), "unreachable") {
		t.Errorf("rebuilt closing event: %v", evt)
	}
}
