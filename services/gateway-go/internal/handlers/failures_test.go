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
	if err := ds.CreatePlan(t.Context(), pendingPlan(id, coach)); err != nil {
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
			api.CreateShareLink(rr, requestAs(http.MethodPost, "/api/v1/plans/s1/share", "coach-alpha", "s1"))

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
	api.CreateShareLink(rr, requestAs(http.MethodPost, "/api/v1/plans/s1/share", "coach-alpha", "s1"))

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := decodeBody(t, rr)["qrSvg"]; got != `<svg id="from-rust"></svg>` {
		t.Errorf("expected the SVG from mcp-rust, got %v", got)
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
