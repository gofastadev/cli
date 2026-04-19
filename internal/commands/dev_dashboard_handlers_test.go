package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for the /api/* handlers on the dashboard server that the
// earlier TestDashboard* suite didn't reach: handleHAR,
// handleTraceDetail, handleLogs, handleExplain, handleStream, plus
// the refresh / scrapeDevtools / resolveServices helpers.
// ─────────────────────────────────────────────────────────────────────

// withUpstreamApp stands up a minimal "app" server and returns a
// dashboardServer pointing at it. Handlers are caller-provided so
// each test serves exactly the endpoints its handler needs. The
// server itself is kept alive via t.Cleanup — callers don't need a
// handle.
func withUpstreamApp(t *testing.T, handlers map[string]http.HandlerFunc) *dashboardServer {
	t.Helper()
	mux := http.NewServeMux()
	// Default /debug/health so requireDevtools-like probes pass.
	if _, set := handlers["/debug/health"]; !set {
		mux.HandleFunc("/debug/health", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"devtools":"enabled"}`))
		})
	}
	for path, h := range handlers {
		mux.HandleFunc(path, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &dashboardServer{appURL: srv.URL}
}

// ── handleHAR ────────────────────────────────────────────────────────

func TestHandleHAR_SerializesRingAsHAR(t *testing.T) {
	srv := &dashboardServer{
		appURL: "http://irrelevant",
		state: dashboardState{
			RecentRequests: []scrapedRequest{
				{Time: time.Now(), Method: "GET", Path: "/x",
					Status: 200, DurationMS: 10,
					ResponseBody:        `{"ok":true}`,
					ResponseContentType: "application/json",
				},
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/har", nil)
	rec := httptest.NewRecorder()
	srv.handleHAR(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "gofasta-dev.har")
	var har harDoc
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &har))
	require.Len(t, har.Log.Entries, 1)
	assert.Equal(t, "GET", har.Log.Entries[0].Request.Method)
}

// ── handleTraceDetail ────────────────────────────────────────────────

func TestHandleTraceDetail_Forwards(t *testing.T) {
	srv := withUpstreamApp(t, map[string]http.HandlerFunc{
		"/debug/traces/t1": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"trace_id":"t1"}`))
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/trace/t1", nil)
	rec := httptest.NewRecorder()
	srv.handleTraceDetail(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"trace_id":"t1"`)
}

func TestHandleTraceDetail_MissingID(t *testing.T) {
	srv := &dashboardServer{appURL: "http://irrelevant"}
	req := httptest.NewRequest(http.MethodGet, "/api/trace/", nil)
	rec := httptest.NewRecorder()
	srv.handleTraceDetail(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleTraceDetail_UpstreamMiss(t *testing.T) {
	srv := withUpstreamApp(t, map[string]http.HandlerFunc{
		"/debug/traces/missing": func(w http.ResponseWriter, _ *http.Request) {
			http.NotFound(w, nil)
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/trace/missing", nil)
	rec := httptest.NewRecorder()
	srv.handleTraceDetail(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ── handleLogs ───────────────────────────────────────────────────────

func TestHandleLogs_ForwardsQueryParams(t *testing.T) {
	var seenQuery string
	srv := withUpstreamApp(t, map[string]http.HandlerFunc{
		"/debug/logs": func(w http.ResponseWriter, r *http.Request) {
			seenQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`[]`))
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/logs?trace_id=abc&level=WARN", nil)
	rec := httptest.NewRecorder()
	srv.handleLogs(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, seenQuery, "trace_id=abc")
	assert.Contains(t, seenQuery, "level=WARN")
}

// ── handleExplain ────────────────────────────────────────────────────

func TestHandleExplain_ProxiesToApp(t *testing.T) {
	srv := withUpstreamApp(t, map[string]http.HandlerFunc{
		"/debug/explain": func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodPost, r.Method)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"plan":"Seq Scan"}`))
		},
	})
	body := `{"sql":"SELECT 1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/explain", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleExplain(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Seq Scan")
}

func TestHandleExplain_RejectsNonPost(t *testing.T) {
	srv := &dashboardServer{appURL: "http://irrelevant"}
	req := httptest.NewRequest(http.MethodGet, "/api/explain", nil)
	rec := httptest.NewRecorder()
	srv.handleExplain(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestHandleExplain_PropagatesUpstreamStatus(t *testing.T) {
	srv := withUpstreamApp(t, map[string]http.HandlerFunc{
		"/debug/explain": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("only SELECT"))
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/explain", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.handleExplain(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ── handleStream ─────────────────────────────────────────────────────

// TestHandleStream_PrimesClient — the SSE handler must send the
// current state on connect, then close cleanly when the client
// cancels. We use a cancellable context to exit the handler
// deterministically.
func TestHandleStream_PrimesClient(t *testing.T) {
	srv := &dashboardServer{
		appURL: "http://irrelevant",
		state:  dashboardState{AppPort: 8080, Health: "ok"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.handleStream(rec, req)
		close(done)
	}()
	// Cancel the context to trigger the handler's exit path.
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handleStream did not return after context cancellation")
	}
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "data: ")
}

// TestWriteSSE — smoke test the framing helper directly so the 0%
// coverage entry for writeSSE lifts.
func TestWriteSSE(t *testing.T) {
	rec := httptest.NewRecorder()
	flusher := rec
	writeSSE(rec, flusher, dashboardState{AppPort: 9090})
	assert.Contains(t, rec.Body.String(), `"app_port":9090`)
	assert.True(t, strings.HasPrefix(rec.Body.String(), "data: "))
	assert.True(t, strings.HasSuffix(rec.Body.String(), "\n\n"))
}

// ── refresh + scrapeDevtools + resolveServices ──────────────────────

func TestScrapeDevtools_DevtoolsOff(t *testing.T) {
	srv := &dashboardServer{appURL: "http://irrelevant"}
	got := srv.scrapeDevtools(false)
	assert.Empty(t, got.requests)
	assert.Empty(t, got.queries)
	assert.Empty(t, got.traces)
	assert.Empty(t, got.exceptions)
	assert.Empty(t, got.cacheOps)
	assert.Equal(t, 0, got.goroutines.Total)
}

func TestScrapeDevtools_HappyPath(t *testing.T) {
	srv := withUpstreamApp(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]scrapedRequest{{Method: "GET"}})
		},
		"/debug/sql": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("[]"))
		},
		"/debug/traces": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/cache":  func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/pprof/goroutine": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("goroutine 1 [running]:\nmain.x()\n"))
		},
	})
	got := srv.scrapeDevtools(true)
	assert.Len(t, got.requests, 1)
	assert.Equal(t, 1, got.goroutines.Total)
}

// TestAsynqmonURLFor — name + health matrix exhaustively covered.
func TestAsynqmonURLFor(t *testing.T) {
	cases := []struct {
		state serviceState
		want  string
	}{
		{serviceState{Name: "db", Health: "healthy"}, ""},
		{serviceState{Name: "queue", Health: "healthy"}, "http://localhost:8081"},
		{serviceState{Name: "app_queue", State: "running"}, "http://localhost:8081"},
		{serviceState{Name: "queue", Health: "starting"}, ""},
	}
	for _, c := range cases {
		t.Run(c.state.Name, func(t *testing.T) {
			assert.Equal(t, c.want, asynqmonURLFor(c.state))
		})
	}
}

// TestProbeHealth_OK — 2xx → "ok".
func TestProbeHealth_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	assert.Equal(t, "ok", probeHealth(srv.URL+"/health"))
}

// TestProbeHealth_Unhealthy — 5xx → "unhealthy".
func TestProbeHealth_Unhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	assert.Equal(t, "unhealthy", probeHealth(srv.URL+"/health"))
}

// TestProbeHealth_Unreachable — closed server → "unreachable".
func TestProbeHealth_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close()
	assert.Equal(t, "unreachable", probeHealth(url+"/health"))
}

// ── buildHAR edge cases ──────────────────────────────────────────────

func TestBuildHAR_MissingContentType(t *testing.T) {
	har := buildHAR([]scrapedRequest{
		{Method: "GET", Path: "/x", Status: 204, DurationMS: 5},
	})
	require.Len(t, har.Log.Entries, 1)
	// Default content-type when upstream didn't set one.
	assert.Equal(t, "application/octet-stream", har.Log.Entries[0].Response.Content.MimeType)
}

// TestFlattenHeaders — multi-value headers collapse to the first;
// empty values yield no entry.
func TestFlattenHeaders(t *testing.T) {
	h := http.Header{}
	h.Add("X-Foo", "a")
	h.Add("X-Foo", "b")
	h["X-Empty"] = nil
	got := flattenHeaders(h)
	assert.Equal(t, "a", got["X-Foo"])
	_, ok := got["X-Empty"]
	assert.False(t, ok)
}

// ensure the fixture file's imports stay used if some tests are
// pruned later — keeping bytes.Buffer satisfied.
var _ bytes.Buffer
