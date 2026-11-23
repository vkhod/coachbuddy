package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// agentsByPath stands in for agents-python with both endpoints. regenerate
// gets the decoded /api/v1/regenerate request and writes the answer.
func agentsByPath(t *testing.T, regenerate func(w http.ResponseWriter, req map[string]interface{})) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Gateway-Token") != "test-internal-token" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch r.URL.Path {
		case "/api/v1/orchestrate":
			_ = json.NewEncoder(w).Encode(finalPlan(t, req))
		case "/api/v1/regenerate":
			regenerate(w, req)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newWarmup is what the Warm-up agent answers to "Use bands".
var newWarmup = map[string]interface{}{
	"minutes": 12,
	"targets": []interface{}{map[string]interface{}{"area": "Shoulders", "why": "Rack position in A"}},
	"blocks":  []interface{}{map[string]interface{}{"name": "Band pull-aparts", "minutes": 12, "items": []interface{}{"3 x 15"}}},
}

// warmupChanged answers a warm-up regeneration: the stored plan at the new
// revision with the new warm-up, as agents-python returns it.
func warmupChanged(w http.ResponseWriter, req map[string]interface{}) {
	doc := copyDoc(req["plan"].(map[string]interface{}))
	doc["revision"] = req["revision"]
	doc["warmup"] = newWarmup
	doc["agentRuns"] = []interface{}{map[string]interface{}{"agent": "WarmupPlannerAgent", "section": "warmup"}}
	_ = json.NewEncoder(w).Encode(doc)
}

func builtPlan(t *testing.T, api *APIHandler) string {
	t.Helper()
	_, id := startPlan(t, api, "coach-alpha", createBody("ankle injury"))
	api.jobs.Wait()
	return id
}

func call(api *APIHandler, handler func(http.ResponseWriter, *http.Request), method, target, coach, id string, path map[string]string, body string) *httptest.ResponseRecorder {
	if target == "" {
		target = "/"
	}
	req := requestAs(method, target, coach, id)
	for k, v := range path {
		req.SetPathValue(k, v)
	}
	if body != "" {
		req.Body = httpBody(body)
	}
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func regenerate(api *APIHandler, coach, id, section, body string) *httptest.ResponseRecorder {
	return call(api, api.RegenerateSection, http.MethodPost, "/api/v1/plans/"+id+"/sections/"+section+"/regenerate", coach, id, map[string]string{"section": section}, body)
}

func restore(api *APIHandler, coach, id, n string) *httptest.ResponseRecorder {
	return call(api, api.RestoreRevision, http.MethodPost, "/api/v1/plans/"+id+"/revisions/"+n+"/restore", coach, id, map[string]string{"n": n}, "")
}

func revisions(t *testing.T, api *APIHandler, id string) []map[string]interface{} {
	t.Helper()
	rr := call(api, api.ListRevisions, http.MethodGet, "/api/v1/plans/"+id+"/revisions", "coach-alpha", id, nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("revisions: %d %s", rr.Code, rr.Body.String())
	}
	var out struct {
		Revisions []map[string]interface{} `json:"revisions"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	return out.Revisions
}

func TestRegenerate_WarmupThenRestoreVersionOne(t *testing.T) {
	// Done-when check: a warm-up refinement changes only the warm-up, and v1 can be restored.
	api, ds := newTestAPIHandler(t)
	var sent map[string]interface{}
	api.AgentsServiceURL = agentsByPath(t, func(w http.ResponseWriter, req map[string]interface{}) {
		sent = req
		warmupChanged(w, req)
	}).URL
	id := builtPlan(t, api)
	v1, _ := ds.GetPlan(t.Context(), id)

	rr := regenerate(api, "coach-alpha", id, "warmup", `{"instruction":"  Use bands "}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}
	if body := decodeBody(t, rr); body["revision"] != float64(2) || body["eventsUrl"] != "/api/v1/plans/"+id+"/events" {
		t.Errorf("202 body: %v", body)
	}
	api.jobs.Wait()

	// agents-python got the plan as it was at revision 1, and the trimmed instruction.
	if sent["section"] != "warmup" || sent["instruction"] != "Use bands" || sent["revision"] != float64(2) || sent["coachId"] != "coach-alpha" {
		t.Errorf("regenerate request: %v", sent)
	}
	if p := sent["plan"].(map[string]interface{}); p["revision"] != float64(1) || p["warmup"] == nil {
		t.Errorf("the stored plan at revision 1 must be sent, got revision %v", p["revision"])
	}
	if _, ok := sent["routingConfig"].(map[string]interface{})["global"]; !ok {
		t.Error("routingConfig missing")
	}

	v2, _ := ds.GetPlan(t.Context(), id)
	if v2["revision"] != float64(2) || v2["warmup"].(map[string]interface{})["blocks"].([]interface{})[0].(map[string]interface{})["name"] != "Band pull-aparts" {
		t.Fatalf("revision 2: %v", v2["warmup"])
	}
	for _, k := range []string{"wod", "parts", "injuries", "timeline", "input", "createdAt", "coachId", "sectionStatus"} {
		a, _ := json.Marshal(v1[k])
		b, _ := json.Marshal(v2[k])
		if string(a) != string(b) {
			t.Errorf("%s changed with the warm-up", k)
		}
	}
	if e := lastEvent(t, ds, id); e["section"] != "plan" || e["state"] != "done" || e["revision"] != float64(2) {
		t.Errorf("closing event: %v", e)
	}
	if ds.runs[id].Revision != 2 {
		t.Errorf("agent runs saved for revision %d", ds.runs[id].Revision)
	}

	revs := revisions(t, api, id)
	if len(revs) != 2 {
		t.Fatalf("revisions: %v", revs)
	}
	if c := revs[0]["change"].(map[string]interface{}); c["kind"] != "create" || revs[0]["state"] != "done" || len(revs[0]["sections"].([]interface{})) != 5 {
		t.Errorf("revision 1: %v", revs[0])
	}
	c := revs[1]["change"].(map[string]interface{})
	if c["kind"] != "regenerate" || c["section"] != "warmup" || c["instruction"] != "Use bands" || revs[1]["state"] != "done" {
		t.Errorf("revision 2: %v", revs[1])
	}
	if s := revs[1]["sections"].([]interface{}); len(s) != 1 || s[0] != "warmup" {
		t.Errorf("revision 2 sections: %v", s)
	}

	// Revision 1 can be read as it was.
	rr = call(api, api.GetRevision, http.MethodGet, "/api/v1/plans/"+id+"/revisions/1", "coach-alpha", id, map[string]string{"n": "1"}, "")
	if rr.Code != http.StatusOK || decodeBody(t, rr)["plan"].(map[string]interface{})["revision"] != float64(1) {
		t.Fatalf("get revision 1: %d %s", rr.Code, rr.Body.String())
	}

	// Restoring version 1 makes revision 3, with version 1's warm-up; nothing is lost.
	rr = restore(api, "coach-alpha", id, "1")
	if rr.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", rr.Code, rr.Body.String())
	}
	v3, _ := ds.GetPlan(t.Context(), id)
	a, _ := json.Marshal(v1["warmup"])
	b, _ := json.Marshal(v3["warmup"])
	if v3["revision"] != float64(3) || string(a) != string(b) {
		t.Errorf("restored plan: revision %v, warm-up %s", v3["revision"], b)
	}
	if err := validPlan(v3); err != nil {
		t.Errorf("restored plan does not match the contract: %v", err)
	}
	revs = revisions(t, api, id)
	if len(revs) != 3 {
		t.Fatalf("revisions after restore: %v", revs)
	}
	if c := revs[2]["change"].(map[string]interface{}); c["kind"] != "restore" || c["restoredFrom"] != float64(1) {
		t.Errorf("revision 3: %v", revs[2])
	}
	if e := lastEvent(t, ds, id); e["revision"] != float64(3) || e["state"] != "done" {
		t.Errorf("restore closing event: %v", e)
	}
}

func TestRegenerate_FailureKeepsTheOtherSectionsAndVersionOne(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	srv := agentsByPath(t, func(w http.ResponseWriter, req map[string]interface{}) {})
	api.AgentsServiceURL = srv.URL
	id := builtPlan(t, api)
	srv.Close() // agents-python goes down before the change

	if rr := regenerate(api, "coach-alpha", id, "parts", `{"instruction":"Simpler"}`); rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
	api.jobs.Wait()

	doc, _ := ds.GetPlan(t.Context(), id)
	st := states(doc)
	if st["parts"] != "failed" || st["warmup"] != "done" || st["timeline"] != "done" {
		t.Fatalf("states after a failed change: %v", st)
	}
	if _, ok := doc["parts"]; ok {
		t.Error("no stale or substitute scaling may stay in a failed section")
	}
	reason := doc["sectionStatus"].(map[string]interface{})["parts"].(map[string]interface{})["error"].(string)
	if !strings.Contains(reason, "unreachable") {
		t.Errorf("reason: %q", reason)
	}
	revs := revisions(t, api, id)
	if len(revs) != 2 || revs[1]["state"] != "failed" {
		t.Errorf("revisions: %v", revs)
	}
	if rr := restore(api, "coach-alpha", id, "1"); rr.Code != http.StatusOK {
		t.Fatalf("restore after a failed change: %d %s", rr.Code, rr.Body.String())
	}
	doc, _ = ds.GetPlan(t.Context(), id)
	if states(doc)["parts"] != "done" || doc["parts"] == nil {
		t.Errorf("version 1's scaling must be back: %v", states(doc))
	}
}

func TestRegenerate_OneChangeAtATime(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	release := make(chan struct{})
	api.AgentsServiceURL = agentsByPath(t, func(w http.ResponseWriter, req map[string]interface{}) {
		<-release
		warmupChanged(w, req)
	}).URL
	id := builtPlan(t, api)

	if rr := regenerate(api, "coach-alpha", id, "warmup", `{"instruction":"Shorter"}`); rr.Code != http.StatusAccepted {
		t.Fatalf("first change: %d", rr.Code)
	}
	// While it runs: the warm-up is pending, without its old content.
	doc, _ := ds.GetPlan(t.Context(), id)
	if states(doc)["warmup"] != "pending" || doc["warmup"] != nil || doc["revision"] != float64(2) {
		t.Errorf("while changing: %v, warmup %v", states(doc), doc["warmup"])
	}
	if revs := revisions(t, api, id); len(revs) != 2 || revs[1]["state"] != "running" {
		t.Errorf("running revision: %v", revs)
	}
	if rr := regenerate(api, "coach-alpha", id, "parts", `{"instruction":"Simpler"}`); rr.Code != http.StatusConflict {
		t.Errorf("second change while one runs: %d, want 409", rr.Code)
	}
	if rr := restore(api, "coach-alpha", id, "1"); rr.Code != http.StatusConflict {
		t.Errorf("restore while a change runs: %d, want 409", rr.Code)
	}
	if rr := call(api, api.GetRevision, http.MethodGet, "", "coach-alpha", id, map[string]string{"n": "2"}, ""); rr.Code != http.StatusNotFound {
		t.Errorf("a running revision has no plan yet: %d, want 404", rr.Code)
	}
	close(release)
	api.jobs.Wait()
	if doc, _ := ds.GetPlan(t.Context(), id); states(doc)["warmup"] != "done" {
		t.Errorf("after the change: %v", states(doc))
	}
}

func TestRegenerate_BadRequests(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	api.AgentsServiceURL = agentsByPath(t, warmupChanged).URL
	id := builtPlan(t, api)
	cases := []struct {
		name, coach, section, body string
		want                       int
	}{
		{"the parser", "coach-alpha", "wod", `{"instruction":"again"}`, http.StatusBadRequest},
		{"unknown section", "coach-alpha", "notes", `{"instruction":"x"}`, http.StatusBadRequest},
		{"no instruction", "coach-alpha", "warmup", `{}`, http.StatusBadRequest},
		{"blank instruction", "coach-alpha", "warmup", `{"instruction":"   "}`, http.StatusBadRequest},
		{"too long", "coach-alpha", "warmup", `{"instruction":"` + strings.Repeat("x", 501) + `"}`, http.StatusBadRequest},
		{"unknown field", "coach-alpha", "warmup", `{"instruction":"x","section":"wod"}`, http.StatusBadRequest},
		{"another coach", "coach-beta", "warmup", `{"instruction":"Shorter"}`, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rr := regenerate(api, c.coach, id, c.section, c.body); rr.Code != c.want {
				t.Errorf("got %d, want %d: %s", rr.Code, c.want, rr.Body.String())
			}
		})
	}
	api.jobs.Wait()
	if doc, _ := ds.GetPlan(t.Context(), id); doc["revision"] != float64(1) {
		t.Errorf("a refused change must not make a revision: %v", doc["revision"])
	}

	// Revisions and restore check the owner and the number too.
	if rr := call(api, api.ListRevisions, http.MethodGet, "", "coach-beta", id, nil, ""); rr.Code != http.StatusNotFound {
		t.Errorf("another coach's revisions: %d", rr.Code)
	}
	for n, want := range map[string]int{"1": http.StatusBadRequest, "0": http.StatusBadRequest, "x": http.StatusBadRequest, "7": http.StatusNotFound} {
		if rr := restore(api, "coach-alpha", id, n); rr.Code != want {
			t.Errorf("restore %s: %d, want %d", n, rr.Code, want)
		}
	}
	if rr := restore(api, "coach-beta", id, "1"); rr.Code != http.StatusNotFound {
		t.Errorf("another coach's restore: %d", rr.Code)
	}
}

func TestRegenerate_NotWhenTheWodFailed(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	id := builtPlan(t, api) // agents-python unreachable: every section failed
	if rr := regenerate(api, "coach-alpha", id, "warmup", `{"instruction":"Shorter"}`); rr.Code != http.StatusConflict {
		t.Errorf("got %d, want 409", rr.Code)
	}
	if doc, _ := ds.GetPlan(t.Context(), id); doc["revision"] != float64(1) {
		t.Error("no revision for a refused change")
	}
}

func TestStreamPlanEvents_FollowsTheCurrentRevisionOnly(t *testing.T) {
	// The replay list still holds revision 1's closing event while revision 2 runs;
	// it must not end the stream before revision 2 has finished.
	api, ds := newTestAPIHandler(t)
	doc := examplePlan(t, "2026-11-18-senior-two-injuries.json")
	doc["coachId"] = "coach-alpha"
	doc["revision"] = 2
	doc = withStatus(doc, "warmup", "running")
	delete(doc, "warmup")
	if err := ds.CreatePlan(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	id := doc["planId"].(string)
	ds.events[id] = []string{
		`{"planId":"` + id + `","revision":1,"section":"plan","state":"done"}`,
		`{"planId":"` + id + `","revision":2,"section":"warmup","state":"running"}`,
		`{"planId":"` + id + `","revision":2,"section":"plan","state":"done"}`,
	}
	rr := httptest.NewRecorder()
	api.StreamPlanEvents(rr, requestAs(http.MethodGet, "/api/v1/plans/"+id+"/events", "coach-alpha", id))
	want := "data: " + ds.events[id][1] + "\n\ndata: " + ds.events[id][2] + "\n\n"
	if rr.Body.String() != want {
		t.Errorf("stream:\n got %q\nwant %q", rr.Body.String(), want)
	}
}

func TestDeletePlan_RemovesItsRevisions(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	api.AgentsServiceURL = agentsByPath(t, warmupChanged).URL
	id := builtPlan(t, api)
	regenerate(api, "coach-alpha", id, "warmup", `{"instruction":"Shorter"}`)
	api.jobs.Wait()
	rr := call(api, api.DeletePlan, http.MethodDelete, "", "coach-alpha", id, nil, "")
	if rr.Code != http.StatusOK || len(ds.revisions[id]) != 0 {
		t.Errorf("delete: %d, revisions left %d", rr.Code, len(ds.revisions[id]))
	}
}

// A warm-up change that fails before agents-python answers leaves the plan
// without a warm-up, so the old timeline must be marked provisional, the same
// as when agents-python itself reports the failure.
func TestRegenerate_FailedWarmupMarksTheKeptTimelineProvisional(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	srv := agentsByPath(t, func(w http.ResponseWriter, req map[string]interface{}) {})
	api.AgentsServiceURL = srv.URL
	id := builtPlan(t, api)
	srv.Close() // agents-python goes down before the change

	before, _ := ds.GetPlan(t.Context(), id)
	if tl, _ := before["timeline"].(map[string]interface{}); tl == nil || tl["provisional"] == true {
		t.Fatalf("version 1 needs a final timeline: %v", before["timeline"])
	}
	if rr := regenerate(api, "coach-alpha", id, "warmup", `{"instruction":"Shorter"}`); rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
	api.jobs.Wait()

	doc, _ := ds.GetPlan(t.Context(), id)
	st := states(doc)
	if st["warmup"] != "failed" || st["timeline"] != "done" {
		t.Fatalf("states: %v", st)
	}
	tl, _ := doc["timeline"].(map[string]interface{})
	if tl == nil || tl["provisional"] != true {
		t.Errorf("the kept timeline must be provisional while the warm-up is missing: %v", doc["timeline"])
	}
	if err := validPlan(doc); err != nil {
		t.Errorf("failed plan must still match the contract: %v", err)
	}

	// Restoring version 1 brings back its warm-up and its final timeline.
	if rr := restore(api, "coach-alpha", id, "1"); rr.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", rr.Code, rr.Body.String())
	}
	doc, _ = ds.GetPlan(t.Context(), id)
	if tl, _ := doc["timeline"].(map[string]interface{}); tl == nil || tl["provisional"] == true {
		t.Errorf("version 1's timeline is final: %v", doc["timeline"])
	}
}
