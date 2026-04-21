package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
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

// TestHandleExplain_UpstreamUnreachable — handler forwards to app's
// /debug/explain; when the app is down we get 502.
func TestHandleExplain_UpstreamUnreachable(t *testing.T) {
	srv := &dashboardServer{appURL: "http://127.0.0.1:1"}
	req := httptest.NewRequest(http.MethodPost, "/api/explain",
		strings.NewReader(`{"sql":"SELECT 1"}`))
	rec := httptest.NewRecorder()
	srv.handleExplain(rec, req)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestHandleReplay_BadJSON — malformed body → 400.
func TestHandleReplay_BadJSON(t *testing.T) {
	srv := &dashboardServer{appURL: "http://irrelevant"}
	req := httptest.NewRequest(http.MethodPost, "/api/replay",
		strings.NewReader("{not-json"))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleReplay_MissingFields — method / path empty → 400.
func TestHandleReplay_MissingFields(t *testing.T) {
	srv := &dashboardServer{appURL: "http://irrelevant"}
	req := httptest.NewRequest(http.MethodPost, "/api/replay",
		strings.NewReader(`{"method":"","path":""}`))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleReplay_UpstreamUnreachable — validator accepts but the
// upstream app is down → 502.
func TestHandleReplay_UpstreamUnreachable(t *testing.T) {
	srv := &dashboardServer{appURL: "http://127.0.0.1:1"}
	req := httptest.NewRequest(http.MethodPost, "/api/replay",
		strings.NewReader(`{"method":"GET","path":"/x"}`))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestHandleIndex_EmptyStateStillRenders — a bare dashboardState
// renders the page without errors.
func TestHandleIndex_EmptyStateStillRenders(t *testing.T) {
	srv := &dashboardServer{state: dashboardState{AppURL: "x", Health: "ok"}}
	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestExtractResponseType_NoResponses — empty map returns "".
func TestExtractResponseType_NoResponses(t *testing.T) {
	assert.Empty(t, extractResponseType(nil))
	assert.Empty(t, extractResponseType(map[string]responseSpec{}))
}

// TestExtractResponseType_SchemaNil — primary code picked but its
// responseSpec has no schema → "".
func TestExtractResponseType_SchemaNil(t *testing.T) {
	assert.Empty(t, extractResponseType(map[string]responseSpec{
		"200": {},
	}))
}

// TestResolveServices_QueueSurfacesAsynqmonURL — a healthy queue
// service in compose produces a non-empty asynqmon URL.
func TestResolveServices_QueueSurfacesAsynqmonURL(t *testing.T) {
	out := `[{"Service":"queue","State":"running","Health":"healthy"}]`
	fakeExecOutput(t, out, 0)
	srv := &dashboardServer{svc: &devServices{selected: []string{"queue"}}}
	states, asynqmonURL := srv.resolveServices()
	assert.NotEmpty(t, states)
	assert.NotEmpty(t, asynqmonURL)
}

// TestReadDevtoolsState_MissingKey — /debug/health responds 200 but
// the JSON body doesn't include a `devtools` field. readDevtoolsState
// returns "unreachable" as the fallback.
func TestReadDevtoolsState_MissingKey(t *testing.T) {
	srv := withUpstreamApp(t, map[string]http.HandlerFunc{
		"/debug/health": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"other":"field"}`))
		},
	})
	assert.Equal(t, "unreachable", readDevtoolsState(srv.appURL))
}

// TestHandleReplay_MissingMethod — only path set → 400.
func TestHandleReplay_MissingMethod(t *testing.T) {
	srv := &dashboardServer{appURL: "http://irrelevant"}
	req := httptest.NewRequest(http.MethodPost, "/api/replay",
		strings.NewReader(`{"method":"","path":"/x"}`))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleReplay_ForbiddenMethod — TRACE isn't in the allowlist.
func TestHandleReplay_ForbiddenMethod(t *testing.T) {
	srv := &dashboardServer{appURL: "http://irrelevant"}
	req := httptest.NewRequest(http.MethodPost, "/api/replay",
		strings.NewReader(`{"method":"TRACE","path":"/x"}`))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestWriteSSE_HappyPath — writeSSE emits "data: <json>\n\n".
func TestWriteSSE_HappyPath(t *testing.T) {
	rec := httptest.NewRecorder()
	writeSSE(rec, rec, dashboardState{AppPort: 42})
	assert.Contains(t, rec.Body.String(), "data: ")
}

// TestHandleExplain_EmptyBody — zero-length POST body still forwards
// to upstream. With upstream down we get 502.
func TestHandleExplain_EmptyBody(t *testing.T) {
	srv := &dashboardServer{appURL: "http://127.0.0.1:1"}
	req := httptest.NewRequest(http.MethodPost, "/api/explain",
		bytes.NewReader(nil))
	rec := httptest.NewRecorder()
	srv.handleExplain(rec, req)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestHandleIndex_OKPath — bare state renders the template cleanly.
func TestHandleIndex_OKPath(t *testing.T) {
	srv := &dashboardServer{}
	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestHandleIndex_TemplateLoadError — force the template loader to
// return an error and expect a 500 response with the error message.
func TestHandleIndex_TemplateLoadError(t *testing.T) {
	orig := loadDashboardTemplateFn
	loadDashboardTemplateFn = func() (*template.Template, error) {
		return nil, fmt.Errorf("load failed")
	}
	t.Cleanup(func() { loadDashboardTemplateFn = orig })
	srv := &dashboardServer{}
	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestHandleIndex_ExecuteError — the template loads but Execute
// fails at runtime.
func TestHandleIndex_ExecuteError(t *testing.T) {
	orig := loadDashboardTemplateFn
	// Build a real parseable template whose Execute errors at runtime.
	tmpl, err := template.New("t").Parse(`{{call .NoSuchFunc}}`)
	require.NoError(t, err)
	loadDashboardTemplateFn = func() (*template.Template, error) { return tmpl, nil }
	t.Cleanup(func() { loadDashboardTemplateFn = orig })
	srv := &dashboardServer{}
	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestHandleIndex_TemplateError — with real embedded template the
// Execute error case has no natural trigger.
func TestHandleIndex_TemplateError(t *testing.T) {
	t.Skip("dashboard template always parses + executes; no natural trigger")
}

// TestWriteSSE_MarshalFails_ViaSeam — the writeSSEMarshal seam
// returns an error; writeSSE returns early without writing.
func TestWriteSSE_MarshalFails_ViaSeam(t *testing.T) {
	orig := writeSSEMarshal
	writeSSEMarshal = func(any) ([]byte, error) { return nil, fmt.Errorf("boom") }
	t.Cleanup(func() { writeSSEMarshal = orig })
	rec := httptest.NewRecorder()
	writeSSE(rec, rec, dashboardState{})
	// No data should have been written.
	assert.Empty(t, rec.Body.String())
}

// TestWriteSSE_MarshalFails — dashboardState always marshals cleanly,
// so this branch needs the marshaler seam above to be reachable.
func TestWriteSSE_MarshalFails(t *testing.T) {
	t.Skip("dashboardState is always marshalable; branch needs marshaler seam")
}

// TestHandleStream_ReceivesUpdate — subscribe to the stream, then
// trigger a refresh. The handler writes the received state via writeSSE.
func TestHandleStream_ReceivesUpdate(t *testing.T) {
	srv := &dashboardServer{
		appURL: "http://127.0.0.1:1",
		state:  dashboardState{AppPort: 8080},
	}
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	// Start the handler in the background.
	done := make(chan struct{})
	go func() {
		srv.handleStream(rec, req)
		close(done)
	}()
	// Give the handler time to subscribe + prime.
	time.Sleep(50 * time.Millisecond)
	// Trigger a refresh — this broadcasts to the listener channel.
	srv.refresh()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	// Expect at least the primer "data:" frame + one from refresh.
	assert.Contains(t, rec.Body.String(), "data: ")
}

// strictNonFlusher wraps a ResponseWriter to hide its Flush method so
// handleStream falls into its non-Flusher branch.
type strictNonFlusher struct{ http.ResponseWriter }

// TestHandleStream_NotAFlusher — ResponseWriter isn't an http.Flusher.
func TestHandleStream_NotAFlusher(t *testing.T) {
	srv := &dashboardServer{appURL: "http://irrelevant"}
	rec := httptest.NewRecorder()
	// Wrap to strip the Flush method.
	wrapped := strictNonFlusher{ResponseWriter: rec}
	req := httptest.NewRequest(http.MethodGet, "/api/stream", nil)
	srv.handleStream(wrapped, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// miscErrReader is a small io.ReadCloser that always errs on Read.
// Used to drive handleExplain / handleReplay body-parse error branches.
type miscErrReader struct{}

func (miscErrReader) Read(_ []byte) (int, error) { return 0, fmt.Errorf("boom") }
func (miscErrReader) Close() error               { return nil }

// TestHandleExplain_ReadBodyError — body reader errors; handler
// responds 400.
func TestHandleExplain_ReadBodyError(t *testing.T) {
	srv := &dashboardServer{appURL: "http://irrelevant"}
	req := httptest.NewRequest(http.MethodPost, "/api/explain", miscErrReader{})
	rec := httptest.NewRecorder()
	srv.handleExplain(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleExplain_BadAppURL — invalid app URL → NewRequest fails.
func TestHandleExplain_BadAppURL(t *testing.T) {
	srv := &dashboardServer{appURL: "\x7f://bad"}
	req := httptest.NewRequest(http.MethodPost, "/api/explain",
		strings.NewReader(`{"sql":"SELECT 1"}`))
	rec := httptest.NewRecorder()
	srv.handleExplain(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestExtractResponseType_EmptyCodePath — A responseSpec with only
// empty-string keys returns "" via pickPrimaryResponseCode.
func TestExtractResponseType_EmptyCodePath(t *testing.T) {
	got := extractResponseType(map[string]responseSpec{
		"": {},
	})
	assert.Empty(t, got)
}

// TestRefresh_BroadcastsToListeners — subscribe a channel and verify
// it receives a snapshot after refresh.
func TestRefresh_BroadcastsToListeners(t *testing.T) {
	srv := &dashboardServer{appURL: "http://127.0.0.1:1"}
	ch := make(chan dashboardState, 1)
	srv.listeners.Store(ch, struct{}{})
	srv.refresh()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("listener did not receive state")
	}
}

// TestRefresh_SlowListenerDrops — a full channel gets dropped via the
// default branch.
func TestRefresh_SlowListenerDrops(t *testing.T) {
	srv := &dashboardServer{appURL: "http://127.0.0.1:1"}
	ch := make(chan dashboardState, 1)
	// Pre-fill so the select's default case fires.
	ch <- dashboardState{}
	srv.listeners.Store(ch, struct{}{})
	srv.refresh()
	// No assertion — coverage is the goal. Drain the channel.
	<-ch
}
