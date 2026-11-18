package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests pin down D3 and D11: when a database, agents-python or mcp-rust
// fails, the gateway reports the failure. It never draws a QR code, builds a
// plan, or pretends a stream finished.

var errDBDown = errors.New("connection refused")

func saveOwned(t *testing.T, ds *fakeStore, id, coach string) {
	t.Helper()
	if err := ds.SaveSession(t.Context(), id, map[string]interface{}{"coachId": coach, "title": "WOD"}); err != nil {
		t.Fatal(err)
	}
}

func httpBody(s string) io.ReadCloser { return io.NopCloser(strings.NewReader(s)) }

func decodeBody(t *testing.T, rr *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not JSON: %v: %s", err, rr.Body.String())
	}
	return out
}

func mcpServer(t *testing.T, qrStatus int, qrBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/tools/qr":
			w.WriteHeader(qrStatus)
			_, _ = w.Write([]byte(qrBody))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCreateShareLink_MCPRustFailureReturns502AndSavesNothing(t *testing.T) {
	cases := []struct {
		name   string
		mcpURL func(t *testing.T) string
	}{
		{"unreachable", func(*testing.T) string { return "http://127.0.0.1:1" }},
		{"500", func(t *testing.T) string { return mcpServer(t, http.StatusInternalServerError, `{"error":"boom"}`).URL }},
		{"no svg in body", func(t *testing.T) string { return mcpServer(t, http.StatusOK, `{"svg":""}`).URL }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api, ds := newTestAPIHandler(t)
			api.McpRustURL = c.mcpURL(t)
			saveOwned(t, ds, "s1", "coach-alpha")

			rr := httptest.NewRecorder()
			api.CreateShareLink(rr, requestAs(http.MethodPost, "/api/v1/sessions/s1/share", "coach-alpha", "s1"))

			if rr.Code != http.StatusBadGateway {
				t.Fatalf("expected 502, got %d: %s", rr.Code, rr.Body.String())
			}
			if strings.Contains(rr.Body.String(), "<svg") {
				t.Error("a failed QR call must not return any SVG")
			}
			if len(ds.links) != 0 {
				t.Errorf("no share link may be saved without a real QR code, got %d", len(ds.links))
			}
		})
	}
}

func TestCreateShareLink_UsesMCPRustQR(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	api.McpRustURL = mcpServer(t, http.StatusOK, `{"svg":"<svg id=\"from-rust\"></svg>"}`).URL
	saveOwned(t, ds, "s1", "coach-alpha")

	rr := httptest.NewRecorder()
	api.CreateShareLink(rr, requestAs(http.MethodPost, "/api/v1/sessions/s1/share", "coach-alpha", "s1"))

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := decodeBody(t, rr)["qrSvg"]; got != `<svg id="from-rust"></svg>` {
		t.Errorf("expected the SVG from mcp-rust, got %v", got)
	}
}

func TestGenerateSession_AgentsFailureReturns502AndSavesNothing(t *testing.T) {
	agents := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model quota exceeded", http.StatusInternalServerError)
	}))
	t.Cleanup(agents.Close)

	for name, url := range map[string]string{"unreachable": "http://127.0.0.1:1", "500": agents.URL} {
		t.Run(name, func(t *testing.T) {
			api, ds := newTestAPIHandler(t)
			api.AgentsServiceURL = url

			req := requestAs(http.MethodPost, "/api/v1/sessions/generate", "coach-alpha", "")
			req.Body = httpBody(`{"rawText":"6RFT: 10 Thrusters, 10 Pull-ups"}`)
			rr := httptest.NewRecorder()
			api.GenerateSession(rr, req)

			if rr.Code != http.StatusBadGateway {
				t.Fatalf("expected 502, got %d: %s", rr.Code, rr.Body.String())
			}
			if _, has := decodeBody(t, rr)["session"]; has {
				t.Error("a failed orchestration must not return a plan")
			}
			if len(ds.sessions) != 0 {
				t.Errorf("nothing may be saved, got %d sessions", len(ds.sessions))
			}
		})
	}
}

func TestGenerateSession_OwnerIsTheCaller(t *testing.T) {
	agents := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"coachId":"someone-else","title":"Plan"}`))
	}))
	t.Cleanup(agents.Close)
	api, ds := newTestAPIHandler(t)
	api.AgentsServiceURL = agents.URL

	req := requestAs(http.MethodPost, "/api/v1/sessions/generate", "coach-alpha", "")
	req.Body = httpBody(`{"rawText":"EMOM 12"}`)
	rr := httptest.NewRecorder()
	api.GenerateSession(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	for _, doc := range ds.sessions {
		if doc["coachId"] != "coach-alpha" {
			t.Errorf("saved owner must be the caller, got %v", doc["coachId"])
		}
	}
}

func TestGenerateSession_SaveFailureReturns500(t *testing.T) {
	agents := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"title":"Plan"}`))
	}))
	t.Cleanup(agents.Close)
	api, ds := newTestAPIHandler(t)
	api.AgentsServiceURL = agents.URL

	ds.saveSessionErr = errDBDown

	req := requestAs(http.MethodPost, "/api/v1/sessions/generate", "coach-alpha", "")
	req.Body = httpBody(`{"rawText":"EMOM 12"}`)
	rr := httptest.NewRecorder()
	api.GenerateSession(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), errDBDown.Error()) {
		t.Error("database error details must not reach the client")
	}
}

func TestSessionRead_DatabaseErrorIs500Not404(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	ds.fail = errDBDown

	for name, h := range map[string]http.HandlerFunc{
		"get": api.GetSession, "delete": api.DeleteSession, "events": api.StreamSessionEvents, "share": api.CreateShareLink,
	} {
		rr := httptest.NewRecorder()
		h(rr, requestAs(http.MethodGet, "/api/v1/sessions/s1", "coach-alpha", "s1"))
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("%s: expected 500 when the database is down, got %d", name, rr.Code)
		}
	}
}

func TestStreamSessionEvents_RedisFailureIsNotAStream(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	saveOwned(t, ds, "s1", "coach-alpha")
	ds.eventsErr = errDBDown

	rr := httptest.NewRecorder()
	api.StreamSessionEvents(rr, requestAs(http.MethodGet, "/api/v1/sessions/s1/events", "coach-alpha", "s1"))

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct == "text/event-stream" {
		t.Error("the stream must not open when Redis is down")
	}
	if strings.Contains(rr.Body.String(), "completed") {
		t.Error("a Redis failure must not be reported as a completed session")
	}
}

func TestStreamSessionEvents_ReplaysBufferedUntilCompleted(t *testing.T) {
	api, ds := newTestAPIHandler(t)
	saveOwned(t, ds, "s1", "coach-alpha")
	ds.events["s1"] = []string{`{"step":"parser"}`, `{"step":"completed"}`, `{"step":"after"}`}

	rr := httptest.NewRecorder()
	api.StreamSessionEvents(rr, requestAs(http.MethodGet, "/api/v1/sessions/s1/events", "coach-alpha", "s1"))

	want := "data: {\"step\":\"parser\"}\n\ndata: {\"step\":\"completed\"}\n\n"
	if rr.Body.String() != want {
		t.Errorf("stream body:\n got %q\nwant %q", rr.Body.String(), want)
	}
}

func TestHealth(t *testing.T) {
	t.Run("all up", func(t *testing.T) {
		api, _ := newTestAPIHandler(t)
		api.McpRustURL = mcpServer(t, http.StatusOK, "").URL
		rr := httptest.NewRecorder()
		api.Health(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	cases := map[string]func(api *APIHandler, ds *fakeStore){
		"postgres": func(_ *APIHandler, ds *fakeStore) { ds.pingErrs["postgres"] = errDBDown },
		"mongodb":  func(_ *APIHandler, ds *fakeStore) { ds.pingErrs["mongodb"] = errDBDown },
		"redis":    func(_ *APIHandler, ds *fakeStore) { ds.pingErrs["redis"] = errDBDown },
		"mcp-rust": func(*APIHandler, *fakeStore) {},
	}
	for down, setup := range cases {
		t.Run(down+" down", func(t *testing.T) {
			api, ds := newTestAPIHandler(t)
			if down != "mcp-rust" {
				api.McpRustURL = mcpServer(t, http.StatusOK, "").URL
			}
			setup(api, ds)
			rr := httptest.NewRecorder()
			api.Health(rr, httptest.NewRequest(http.MethodGet, "/health", nil))

			if rr.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503, got %d", rr.Code)
			}
			checks, _ := decodeBody(t, rr)["checks"].(map[string]interface{})
			if checks[down] != "unavailable" {
				t.Errorf("expected %s unavailable, got %v", down, checks)
			}
			if strings.Contains(rr.Body.String(), errDBDown.Error()) {
				t.Error("the public health body must not carry error details")
			}
		})
	}
}
