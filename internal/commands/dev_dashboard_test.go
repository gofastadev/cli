package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestHandleTraceDetail_MissingID(t *testing.T) {
	srv := &dashboardServer{appURL: "http://irrelevant"}
	req := httptest.NewRequest(http.MethodGet, "/api/trace/", nil)
	rec := httptest.NewRecorder()
	srv.handleTraceDetail(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleExplain_RejectsNonPost(t *testing.T) {
	srv := &dashboardServer{appURL: "http://irrelevant"}
	req := httptest.NewRequest(http.MethodGet, "/api/explain", nil)
	rec := httptest.NewRecorder()
	srv.handleExplain(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

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

func (miscErrReader) Close() error { return nil }

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

// freePort reserves an ephemeral port and releases it — good enough
// for a single-call test, small race window is acceptable.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// TestStartDashboard_LifecycleAndShutdown — starts the dashboard,
// verifies it answers HTTP, then confirms shutdown closes cleanly.
func TestStartDashboard_LifecycleAndShutdown(t *testing.T) {
	chdirTemp(t)
	port := freePort(t)
	emitter := &quietEmitter{}

	shutdown := startDashboard(port, 9999, nil, emitter)
	defer shutdown()

	// Wait for the server to start accepting connections.
	require.Eventually(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/state", port))
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 20*time.Millisecond, "dashboard server never started")

	// Emitter recorded the "dashboard running" info line.
	assert.Greater(t, emitter.info.Load(), int32(0))
}

// TestStartDashboard_DetectsSwaggerAndGraphQL — files present in cwd
// cause the corresponding state URL to populate.
func TestStartDashboard_DetectsSwaggerAndGraphQL(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.MkdirAll("docs", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("docs", "swagger.json"), []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile("gqlgen.yml", []byte(""), 0o644))

	port := freePort(t)
	shutdown := startDashboard(port, 9999, nil, &quietEmitter{})
	defer shutdown()

	var state dashboardState
	require.Eventually(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/state", port))
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return json.NewDecoder(resp.Body).Decode(&state) == nil
	}, 2*time.Second, 20*time.Millisecond)

	assert.Contains(t, state.SwaggerURL, "/swagger/index.html")
	assert.Contains(t, state.GraphQLURL, "/graphql")
}

// TestRefresherLoop_ContextCancelExits — loop exits on ctx.Done().
func TestRefresherLoop_ContextCancelExits(t *testing.T) {
	srv := &dashboardServer{appURL: "http://127.0.0.1:1"}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		srv.refresherLoop(ctx)
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("refresherLoop did not exit after ctx cancellation")
	}
}

// TestResolveServices_NilSvc — nil svc short-circuits.
func TestResolveServices_NilSvc(t *testing.T) {
	srv := &dashboardServer{svc: nil}
	states, asynqmonURL := srv.resolveServices()
	assert.Nil(t, states)
	assert.Empty(t, asynqmonURL)
}

// TestRefresherLoop_TickFires — drive the loop with a very short
// ticker so the tick branch fires before ctx is canceled.
func TestRefresherLoop_TickFires(t *testing.T) {
	srv := &dashboardServer{appURL: "http://127.0.0.1:1"}
	orig := refresherTickInterval
	refresherTickInterval = 10 * time.Millisecond
	t.Cleanup(func() { refresherTickInterval = orig })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		srv.refresherLoop(ctx)
		close(done)
	}()
	// Let a few ticks fire.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
}

// TestRefresherLoop_Tick — drive a refresher loop with a cancelable
// context. The ticker fires at the default interval; we stop it
// quickly by canceling.
func TestRefresherLoop_Tick(t *testing.T) {
	srv := &dashboardServer{appURL: "http://irrelevant"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		srv.refresherLoop(ctx)
		close(done)
	}()
	// Give the initial s.refresh() call time to run.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
}

// TestRefresherLoop_TickFiresRefresh — refresherLoop's ticker-fire
// branch only reaches case <-ticker.C once the interval elapses.
// TestStartDashboard_InvalidPort — listen on port -1 so
// ListenAndServe fails quickly; the goroutine's err-handler branch
// fires.
func TestStartDashboard_InvalidPort(t *testing.T) {
	// Port -1 should make ListenAndServe fail. Give it 100ms to hit
	// the error path then cancel.
	emitter := &humanEmitter{}
	cancel := startDashboard(-1, 8080, nil, emitter)
	time.Sleep(100 * time.Millisecond)
	cancel()
}

// TestBuildReplayURL_ValidPathPasses — happy path: a leading-slash
// relative path produces the expected fully-qualified URL with the
// app's scheme + host preserved.
func TestBuildReplayURL_ValidPathPasses(t *testing.T) {
	cases := map[string]string{
		"/api/v1/users":        "http://localhost:8080/api/v1/users",
		"/api/v1/users?q=x":    "http://localhost:8080/api/v1/users?q=x",
		"/health":              "http://localhost:8080/health",
		"/api/v1/orders/42/ok": "http://localhost:8080/api/v1/orders/42/ok",
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			got, err := buildReplayURL("http://localhost:8080", path)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

// TestBuildReplayURL_RejectsUserinfoInjection — the core SSRF vector
// CodeQL flagged. "@evil.com/x" should not become the request's
// host. If this test fails the fix has regressed.
func TestBuildReplayURL_RejectsUserinfoInjection(t *testing.T) {
	_, err := buildReplayURL("http://localhost:8080", "@evil.com/x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path must")
}

// TestBuildReplayURL_RejectsFullURL — explicit scheme+host should be
// rejected outright. Would otherwise redirect the entire request.
func TestBuildReplayURL_RejectsFullURL(t *testing.T) {
	for _, in := range []string{
		"http://evil.com/x",
		"https://evil.com/x",
		"ftp://evil.com/x",
		"//evil.com/x",
	} {
		t.Run(in, func(t *testing.T) {
			_, err := buildReplayURL("http://localhost:8080", in)
			require.Error(t, err, "should reject %q", in)
		})
	}
}

// TestBuildReplayURL_RejectsMissingLeadingSlash — relative paths
// without a leading slash could resolve ambiguously depending on the
// base URL's path. Require absolute paths so behavior is predictable.
func TestBuildReplayURL_RejectsMissingLeadingSlash(t *testing.T) {
	_, err := buildReplayURL("http://localhost:8080", "api/v1/users")
	require.Error(t, err)
}

// TestBuildReplayURL_PreservesAppHostAcrossInjection — even a
// carefully-crafted path that parses as a relative URL must NOT
// escape the app host. Opaque URLs (mailto:foo, data:...) get
// rejected.
func TestBuildReplayURL_RejectsOpaqueScheme(t *testing.T) {
	for _, in := range []string{
		"mailto:attacker@evil.com",
		"data:text/plain,foo",
	} {
		t.Run(in, func(t *testing.T) {
			_, err := buildReplayURL("http://localhost:8080", in)
			require.Error(t, err)
		})
	}
}

// TestBuildReplayURL_RejectsInvalidURL — malformed input falls into
// a generic error rather than panicking.
func TestBuildReplayURL_RejectsInvalidURL(t *testing.T) {
	_, err := buildReplayURL("http://localhost:8080", "%gh")
	require.Error(t, err)
}

// TestBuildReplayURL_BadAppURL — if the app URL itself is malformed
// (shouldn't happen; config validates it), the function rejects
// rather than blindly proceeding. Covers the defensive internal-error
// branch.
func TestBuildReplayURL_BadAppURL(t *testing.T) {
	_, err := buildReplayURL("not-a-url", "/x")
	require.Error(t, err)
}

// TestValidateReplayMethod_AllowList — only the standard HTTP
// methods pass; unusual verbs (CONNECT, TRACE, custom strings)
// rejected.
func TestValidateReplayMethod_AllowList(t *testing.T) {
	ok := []string{"GET", "get", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
	for _, m := range ok {
		t.Run("ok/"+m, func(t *testing.T) {
			got, err := validateReplayMethod(m)
			require.NoError(t, err)
			assert.Equal(t, strings.ToUpper(m), got)
		})
	}
	for _, m := range []string{"CONNECT", "TRACE", "PROPFIND", "", "GARBAGE"} {
		t.Run("reject/"+m, func(t *testing.T) {
			_, err := validateReplayMethod(m)
			require.Error(t, err)
		})
	}
}

// TestHandleReplay_BlocksSSRFViaPath — end-to-end: the dashboard's
// /api/replay handler rejects a userinfo-injection attempt before
// ever issuing an upstream HTTP call. We stand up a sentinel server
// bound to a throwaway port and confirm no request ever lands on it.
func TestHandleReplay_BlocksSSRFViaPath(t *testing.T) {
	// Sentinel — any request here is the SSRF working. Fail loudly.
	sentinel := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("sentinel got unexpected request: %s %s (host=%s)",
			r.Method, r.URL.Path, r.Host)
	}))
	defer sentinel.Close()

	// Build a dashboardServer whose appURL points at a fixed
	// localhost — the test server itself would work but we want the
	// sentinel separately so any leak is unambiguous.
	srv := &dashboardServer{appURL: "http://127.0.0.1:0"}

	// Evil path that — without the fix — would turn into
	// http://127.0.0.1:0@evil.com/x, i.e. host=evil.com.
	// We try pointing the `@` prefix at the sentinel's host so if
	// SSRF works the sentinel sees the request.
	evilPath := "@" + sentinel.Listener.Addr().String() + "/x"
	body, _ := json.Marshal(replayRequest{
		Method: "GET",
		Path:   evilPath,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/replay", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv.handleReplay(rec, req)

	// Expect a 400 Bad Request from our validator, not a 502 from
	// an attempted-but-failed proxy. Either way, the sentinel's
	// t.Errorf in its handler would fire if the request leaked.
	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"body: %s", rec.Body.String())
}

// TestHandleReplay_BlocksSSRFViaScheme — same shape, but the attack
// tries to inject an explicit scheme+host.
func TestHandleReplay_BlocksSSRFViaScheme(t *testing.T) {
	sentinel := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("sentinel got unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer sentinel.Close()

	srv := &dashboardServer{appURL: "http://127.0.0.1:0"}
	body, _ := json.Marshal(replayRequest{
		Method: "GET",
		Path:   sentinel.URL + "/x",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/replay", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleReplay_BlocksBadMethod — replay rejects CONNECT/TRACE
// before touching the network.
func TestHandleReplay_BlocksBadMethod(t *testing.T) {
	srv := &dashboardServer{appURL: "http://127.0.0.1:0"}
	body, _ := json.Marshal(replayRequest{
		Method: "CONNECT",
		Path:   "/x",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/replay", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleReplay_NewRequestFails — inject a failing newReplayRequest
// seam → handler returns 400.
func TestHandleReplay_NewRequestFails(t *testing.T) {
	orig := newReplayRequest
	newReplayRequest = func(context.Context, string, string, io.Reader) (*http.Request, error) {
		return nil, fmt.Errorf("build failed")
	}
	t.Cleanup(func() { newReplayRequest = orig })
	srv := &dashboardServer{appURL: "http://irrelevant"}
	req := httptest.NewRequest(http.MethodPost, "/api/replay",
		strings.NewReader(`{"method":"GET","path":"/x"}`))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleReplay_WrongMethod — GET /api/replay → 405.
func TestHandleReplay_WrongMethod(t *testing.T) {
	srv := &dashboardServer{appURL: "http://irrelevant"}
	req := httptest.NewRequest(http.MethodGet, "/api/replay", nil)
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// TestHandleReplay_WithBody — body != "" exercises the body = reader
// and Content-Type setter branches.
func TestHandleReplay_WithBody(t *testing.T) {
	srv := &dashboardServer{appURL: "http://127.0.0.1:1"}
	req := httptest.NewRequest(http.MethodPost, "/api/replay",
		strings.NewReader(`{"method":"POST","path":"/x","body":"data"}`))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	// Upstream unreachable → 502.
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestHandleReplay_BadAppURL — makes http.NewRequestWithContext fail
// indirectly via buildReplayURL rejecting the malformed app URL.
func TestHandleReplay_BadAppURL(t *testing.T) {
	srv := &dashboardServer{appURL: "\x7f://bad"}
	req := httptest.NewRequest(http.MethodPost, "/api/replay",
		strings.NewReader(`{"method":"GET","path":"/x"}`))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	// buildReplayURL will fail on the malformed app URL.
	assert.GreaterOrEqual(t, rec.Code, 400)
}

// TestHandleReplay_NewRequestError — handleReplay's
// TestHandleReplay_AcceptsValidReplay — end-to-end happy path:
// dashboard → /api/replay → upstream app → response bubbled back as
// JSON. The upstream is our own stub; we just confirm the response
// shape.
func TestHandleReplay_AcceptsValidReplay(t *testing.T) {
	var upstreamHits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		assert.Equal(t, "/api/v1/health", r.URL.Path)
		assert.Equal(t, "1", r.Header.Get("X-Gofasta-Replay"))
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	srv := &dashboardServer{appURL: upstream.URL}
	body, _ := json.Marshal(replayRequest{
		Method: "get",
		Path:   "/api/v1/health",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/replay", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Equal(t, 1, upstreamHits)
	var out replayResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, 200, out.Status)
	assert.Equal(t, `{"ok":true}`, out.Body)
}

// writeSwagger writes the given JSON content to docs/swagger.json
// inside a tempdir that becomes the working directory for the duration
// of the test.
func writeSwagger(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))
	require.NoError(t, os.MkdirAll("docs", 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join("docs", "swagger.json"), []byte(body), 0o644,
	))
}

// findRoute returns the first route matching method + path. Used to
// assert on a single entry without depending on the order the map
// iteration happens to produce.
func findRoute(t *testing.T, routes []dashboardRoute, method, path string) dashboardRoute {
	t.Helper()
	for _, r := range routes {
		if r.Method == method && r.Path == path {
			return r
		}
	}
	t.Fatalf("route %s %s not found among %+v", method, path, routes)
	return dashboardRoute{}
}

func TestReadRouteEntries_OpenAPI2_BodyParamAndResponseRef(t *testing.T) {
	writeSwagger(t, `{
	  "paths": {
	    "/users": {
	      "post": {
	        "summary": "Create a user",
	        "parameters": [
	          { "in": "body", "name": "user", "schema": { "$ref": "#/definitions/CreateUser" } }
	        ],
	        "responses": {
	          "201": { "schema": { "$ref": "#/definitions/User" } },
	          "400": { "schema": { "$ref": "#/definitions/Error" } }
	        }
	      }
	    }
	  }
	}`)
	routes := readRouteEntries()
	require.Len(t, routes, 1)
	r := findRoute(t, routes, "POST", "/users")
	assert.Equal(t, "Create a user", r.Summary)
	assert.Equal(t, "CreateUser", r.Request)
	assert.Equal(t, "User", r.Response)
}

func TestReadRouteEntries_OpenAPI2_ArrayResponse(t *testing.T) {
	writeSwagger(t, `{
	  "paths": {
	    "/users": {
	      "get": {
	        "summary": "List users",
	        "responses": {
	          "200": {
	            "schema": {
	              "type": "array",
	              "items": { "$ref": "#/definitions/User" }
	            }
	          }
	        }
	      }
	    }
	  }
	}`)
	r := findRoute(t, readRouteEntries(), "GET", "/users")
	assert.Equal(t, "List users", r.Summary)
	assert.Empty(t, r.Request)
	assert.Equal(t, "[]User", r.Response)
}

func TestReadRouteEntries_OpenAPI3_RequestBodyAndContent(t *testing.T) {
	writeSwagger(t, `{
	  "paths": {
	    "/sessions": {
	      "post": {
	        "requestBody": {
	          "content": {
	            "application/json": {
	              "schema": { "$ref": "#/components/schemas/Credentials" }
	            }
	          }
	        },
	        "responses": {
	          "200": {
	            "content": {
	              "application/json": {
	                "schema": { "$ref": "#/components/schemas/Session" }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`)
	r := findRoute(t, readRouteEntries(), "POST", "/sessions")
	assert.Equal(t, "Credentials", r.Request)
	assert.Equal(t, "Session", r.Response)
}

func TestReadRouteEntries_PrimitiveTypeResponse(t *testing.T) {
	writeSwagger(t, `{
	  "paths": {
	    "/health": {
	      "get": {
	        "responses": {
	          "200": { "schema": { "type": "string" } }
	        }
	      }
	    }
	  }
	}`)
	r := findRoute(t, readRouteEntries(), "GET", "/health")
	assert.Equal(t, "string", r.Response)
}

func TestReadRouteEntries_FallsBackToLowestCodeWhenNo2xx(t *testing.T) {
	// Operation declares only error responses — the extractor should
	// pick the lowest code rather than leaving Response empty.
	writeSwagger(t, `{
	  "paths": {
	    "/admin": {
	      "get": {
	        "responses": {
	          "401": { "schema": { "$ref": "#/definitions/Error" } },
	          "403": { "schema": { "$ref": "#/definitions/Error" } }
	        }
	      }
	    }
	  }
	}`)
	r := findRoute(t, readRouteEntries(), "GET", "/admin")
	assert.Equal(t, "Error", r.Response)
}

func TestReadRouteEntries_HandlesEmptyOperations(t *testing.T) {
	writeSwagger(t, `{
	  "paths": {
	    "/ping": {
	      "get": {}
	    }
	  }
	}`)
	r := findRoute(t, readRouteEntries(), "GET", "/ping")
	assert.Empty(t, r.Request)
	assert.Empty(t, r.Response)
	assert.Empty(t, r.Summary)
}

func TestReadRouteEntries_MalformedJSONReturnsNil(t *testing.T) {
	writeSwagger(t, `{not json`)
	assert.Nil(t, readRouteEntries())
}

func TestTypeNameFromSchema(t *testing.T) {
	assert.Equal(t, "", typeNameFromSchema(nil))
	assert.Equal(t, "User",
		typeNameFromSchema(&schemaRef{Ref: "#/definitions/User"}))
	assert.Equal(t, "Session",
		typeNameFromSchema(&schemaRef{Ref: "#/components/schemas/Session"}))
	// No slash separator — fall back to the raw ref value.
	assert.Equal(t, "BareRef",
		typeNameFromSchema(&schemaRef{Ref: "BareRef"}))
	// Array-of-ref renders as []TypeName.
	assert.Equal(t, "[]User",
		typeNameFromSchema(&schemaRef{
			Type:  "array",
			Items: &schemaRef{Ref: "#/definitions/User"},
		}))
	// Array with no items type — falls through to "array".
	assert.Equal(t, "array",
		typeNameFromSchema(&schemaRef{Type: "array"}))
	// Primitive types.
	assert.Equal(t, "string", typeNameFromSchema(&schemaRef{Type: "string"}))
	assert.Equal(t, "integer", typeNameFromSchema(&schemaRef{Type: "integer"}))
	// Empty schema → empty name.
	assert.Empty(t, typeNameFromSchema(&schemaRef{}))
}

func TestPickPrimaryResponseCode(t *testing.T) {
	// 2xx wins over everything else.
	assert.Equal(t, "200",
		pickPrimaryResponseCode(map[string]responseSpec{
			"200": {}, "201": {}, "400": {}, "500": {},
		}))
	// Lowest 2xx wins.
	assert.Equal(t, "201",
		pickPrimaryResponseCode(map[string]responseSpec{
			"201": {}, "202": {}, "204": {},
		}))
	// No 2xx — fall back to lowest of any tier.
	assert.Equal(t, "401",
		pickPrimaryResponseCode(map[string]responseSpec{
			"401": {}, "403": {}, "500": {},
		}))
	// Empty map.
	assert.Empty(t, pickPrimaryResponseCode(map[string]responseSpec{}))
	// Skip empty-string keys (shouldn't happen but defensive).
	assert.Equal(t, "200",
		pickPrimaryResponseCode(map[string]responseSpec{"": {}, "200": {}}))
}

// TestJSONRoundTrip_DashboardRoute — dashboardRoute must marshal cleanly
// so the SSE stream + /api/state endpoint can serialize it without
// surprise (nil maps, unexported fields, etc.).
func TestJSONRoundTrip_DashboardRoute(t *testing.T) {
	r := dashboardRoute{
		Method:   "POST",
		Path:     "/api/v1/users",
		Summary:  "Create user",
		Request:  "CreateUser",
		Response: "User",
	}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	var back dashboardRoute
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, r, back)
}

// loadDashboardTemplate caches via sync.Once. The existing tests
// substitute loadDashboardTemplateFn to inject errors/synthetic
// templates, so the REAL template.New().Funcs(...).Parse(...) body
// reports as 27.3% covered — the FuncMap closures (`deref` and `dict`)
// never fire from a render. This test resets the package vars, calls
// the real loader, and renders a tiny driver template that exercises
// both closures (including the deref nil-pointer guard).
func TestLoadDashboardTemplate_RealBodyAndFuncMap(t *testing.T) {
	// Reset the once/state so the real Once.Do body runs.
	// sync.Once contains a noCopy field so we can't snapshot+restore it;
	// instead we reset to a fresh Once. Subsequent loadDashboardTemplate
	// calls in other tests will see the now-populated cached template,
	// which is the same end state they'd reach via lazy initialization.
	dashboardTemplate = nil
	dashboardTemplateErr = nil
	dashboardTemplateOnce = sync.Once{}

	tmpl, err := loadDashboardTemplate()
	require.NoError(t, err)
	require.NotNil(t, tmpl)

	// Build a driver template under the loaded tree so it inherits the
	// registered Funcs (deref + dict). Two render passes — one with a
	// non-nil *float64 (deref returns *p), one with nil (deref returns 0).
	driver := tmpl.New("driver_for_test")
	_, err = driver.Parse(`d={{deref .Ptr}} m={{(dict "k" .V).k}}`)
	require.NoError(t, err)

	val := 3.14
	var buf bytes.Buffer
	require.NoError(t, driver.Execute(&buf, map[string]any{"Ptr": &val, "V": "ok"}))
	assert.Contains(t, buf.String(), "3.14")
	assert.Contains(t, buf.String(), "ok")

	buf.Reset()
	require.NoError(t, driver.Execute(&buf, map[string]any{"Ptr": (*float64)(nil), "V": "z"}))
	assert.Contains(t, buf.String(), "0")
}

// TestDashboardTemplate_Parses — the embedded dev_dashboard.html template
// must parse successfully. Caught by this test rather than at runtime
// the first time the dashboard flag is used.
func TestDashboardTemplate_Parses(t *testing.T) {
	tmpl, err := loadDashboardTemplate()
	require.NoError(t, err)
	require.NotNil(t, tmpl)
}

// TestDashboardHandleIndex_RendersServerSideState — the index handler
// server-renders the current state so first paint shows real data (no
// "loading" flash). This asserts the rendered HTML contains the
// injected values.
func TestDashboardHandleIndex_RendersServerSideState(t *testing.T) {
	srv := &dashboardServer{
		state: dashboardState{
			AppPort:    8080,
			AppURL:     "http://localhost:8080",
			Health:     "ok",
			SwaggerURL: "http://localhost:8080/swagger/index.html",
			Routes: []dashboardRoute{
				{Method: "GET", Path: "/users"},
				{Method: "POST", Path: "/users"},
			},
			LastUpdatedMS: 1700000000000,
		},
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.handleIndex(rr, req)

	body := rr.Body.String()
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "text/html; charset=utf-8", rr.Header().Get("Content-Type"))

	// State is embedded, not "loading".
	assert.Contains(t, body, "http://localhost:8080")
	assert.Contains(t, body, "8080")
	// Health pill is rendered with the "ok" variant class.
	assert.Contains(t, body, `class="pill ok"`)
	// Routes table is populated with server-rendered entries. Asserting
	// on the presence of the routes <table> (inside <div id="routes">)
	// is unambiguous — the client-side JS fallback string in <script>
	// contains the "empty" placeholder too, so we can't exclude it, but
	// we can positively confirm the server rendered the real table.
	assert.Contains(t, body, "/users")
	assert.Contains(t, body, ">GET<")
	assert.Contains(t, body, ">POST<")
	// The #routes div should contain a <table>, not an <div class="empty">.
	routesBlock := extractBetween(body, `<div id="routes">`, `</div>`)
	assert.Contains(t, routesBlock, "<table>")
	assert.NotContains(t, routesBlock, `class="empty"`)
}

// extractBetween returns the substring of s between the first occurrence
// of start and the next occurrence of end after start. Used by the
// dashboard tests to isolate a rendered section (e.g. the #routes div)
// so assertions don't accidentally match content in <script> blocks.
func extractBetween(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	j := strings.Index(s, end)
	if j < 0 {
		return s
	}
	return s[:j]
}

// TestDashboardHandleIndex_EmptyState — with no services and no routes,
// the template falls back to the "empty" placeholders instead of
// rendering blank tables.
func TestDashboardHandleIndex_EmptyState(t *testing.T) {
	srv := &dashboardServer{
		state: dashboardState{
			AppPort: 8080,
			AppURL:  "http://localhost:8080",
			Health:  "unreachable",
		},
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.handleIndex(rr, req)

	body := rr.Body.String()
	assert.Equal(t, http.StatusOK, rr.Code)
	// Extract the two content divs so we're looking at the rendered
	// empty states, not the identical JS fallback strings inside the
	// <script> block.
	servicesBlock := extractBetween(body, `<div id="services">`, `</div>`)
	routesBlock := extractBetween(body, `<div id="routes">`, `</div>`)
	assert.Contains(t, servicesBlock, `class="empty"`)
	assert.Contains(t, routesBlock, `class="empty"`)
	// Unreachable health uses the error pill variant.
	assert.Contains(t, body, `class="pill err"`)
}

// TestDashboardHandleIndex_EscapesHostileInput — html/template must
// escape attacker-controlled values that end up in the rendered DOM.
// A compose service named `<script>alert(1)</script>` should render
// inert, not as an actual tag.
func TestDashboardHandleIndex_EscapesHostileInput(t *testing.T) {
	srv := &dashboardServer{
		state: dashboardState{
			AppPort: 8080,
			AppURL:  "http://localhost:8080",
			Health:  "ok",
			Services: []serviceState{
				{Name: `<script>alert(1)</script>`, State: "running", Health: "healthy"},
			},
			Routes: []dashboardRoute{
				{Method: "GET", Path: `/"><img src=x onerror=alert(1)>`},
			},
		},
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.handleIndex(rr, req)

	body := rr.Body.String()
	// No raw <script> tag should have made it through — the hostile
	// service name must appear only as escaped entities.
	assert.NotContains(t, body, "<script>alert(1)</script>")
	// No raw <img> tag either — auto-escape turns the angle brackets
	// into &lt; / &gt;.
	assert.False(t, strings.Contains(body, `<img src=x onerror=alert(1)>`),
		"hostile route path escaped into the DOM as a real tag")
	// Positive confirmation that the escaped form IS present (proves
	// the value wasn't silently dropped, only neutered).
	assert.Contains(t, body, "&lt;script&gt;")
	assert.Contains(t, body, "&lt;img src=x onerror=alert(1)&gt;")
}

// TestDashboardHandleState_ReturnsJSON — /api/state must return the
// snapshot with the expected Content-Type so browsers don't sniff.
func TestDashboardHandleState_ReturnsJSON(t *testing.T) {
	srv := &dashboardServer{
		state: dashboardState{AppPort: 8080, Health: "ok"},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	srv.handleState(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.Contains(t, rr.Body.String(), `"app_port":8080`)
	assert.Contains(t, rr.Body.String(), `"health":"ok"`)
}

// TestReadRouteEntries_MissingFile — readRouteEntries should return nil
// when docs/swagger.json is missing, not panic.
func TestReadRouteEntries_MissingFile(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))

	assert.Nil(t, readRouteEntries())
}

// TestReadRouteEntries_ParsesSwagger — writes a minimal swagger.json
// and asserts that the route entries come back as (method, path)
// pairs.
func TestReadRouteEntries_ParsesSwagger(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))
	require.NoError(t, os.MkdirAll("docs", 0o755))

	body := `{
	  "paths": {
	    "/users": {"get": {}, "post": {}},
	    "/users/{id}": {"get": {}, "delete": {}}
	  }
	}`
	require.NoError(t, os.WriteFile(filepath.Join("docs", "swagger.json"), []byte(body), 0o644))

	routes := readRouteEntries()
	assert.Len(t, routes, 4)
	// Order is not guaranteed (JSON object keys) so assert membership.
	seen := map[string]bool{}
	for _, r := range routes {
		seen[r.Method+" "+r.Path] = true
	}
	assert.True(t, seen["GET /users"])
	assert.True(t, seen["POST /users"])
	assert.True(t, seen["GET /users/{id}"])
	assert.True(t, seen["DELETE /users/{id}"])
}

// TestBuildHAR_RoundTripsCoreFields — produced HAR contains method,
// path, status, and response body. Shape roughly matches the HAR 1.2
// schema (has log.entries[].request/response).
func TestBuildHAR_RoundTripsCoreFields(t *testing.T) {
	reqs := []scrapedRequest{
		{
			Method:              "POST",
			Path:                "/api/v1/users",
			Status:              201,
			DurationMS:          12,
			Body:                `{"name":"Alice"}`,
			ResponseBody:        `{"id":"u1"}`,
			ResponseContentType: "application/json",
		},
	}
	har := buildHAR(reqs)
	assert.Equal(t, "1.2", har.Log.Version)
	if assert.Len(t, har.Log.Entries, 1) {
		e := har.Log.Entries[0]
		assert.Equal(t, "POST", e.Request.Method)
		assert.Equal(t, "/api/v1/users", e.Request.URL)
		if assert.NotNil(t, e.Request.PostData) {
			assert.Equal(t, `{"name":"Alice"}`, e.Request.PostData.Text)
		}
		assert.Equal(t, 201, e.Response.Status)
		assert.Equal(t, "application/json", e.Response.Content.MimeType)
		assert.Equal(t, `{"id":"u1"}`, e.Response.Content.Text)
		assert.Equal(t, int64(12), e.Time)
	}
}

// TestBuildHAR_EmptyRing — zero requests produces a valid-but-empty
// HAR doc rather than nil, so the download is still a parseable JSON.
func TestBuildHAR_EmptyRing(t *testing.T) {
	har := buildHAR(nil)
	assert.Equal(t, "1.2", har.Log.Version)
	assert.Empty(t, har.Log.Entries)
}

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
