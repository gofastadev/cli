package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for dev_dashboard.go lifecycle — startDashboard,
// refresherLoop, refresh, resolveServices. Goroutine-heavy; tests
// use context cancellation + t.Cleanup to keep the runtime tidy.
// ─────────────────────────────────────────────────────────────────────

// quietEmitter satisfies devEmitter, counting Info/Warn calls so
// tests can assert without reading a terminal.
type quietEmitter struct {
	info atomic.Int32
	warn atomic.Int32
}

func (q *quietEmitter) Preflight(_, _ string)                    {}
func (q *quietEmitter) ServiceStart(_ string)                    {}
func (q *quietEmitter) ServiceHealthy(_ string, _ time.Duration) {}
func (q *quietEmitter) ServiceUnhealthy(_, _ string)             {}
func (q *quietEmitter) MigrateOK(_ int)                          {}
func (q *quietEmitter) MigrateSkipped(_ string)                  {}
func (q *quietEmitter) Air(_ int, _ map[string]string)           {}
func (q *quietEmitter) Shutdown(_ string, _ int)                 {}
func (q *quietEmitter) Info(_ string)                            { q.info.Add(1) }
func (q *quietEmitter) Warn(_ string)                            { q.warn.Add(1) }

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

// TestRefresh_PopulatesHealthFromUpstream — refresh() probes /health
// and records the result.
func TestRefresh_PopulatesHealthFromUpstream(t *testing.T) {
	srv := withUpstreamApp(t, map[string]http.HandlerFunc{
		"/health":  func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		"/metrics": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("# metrics\n")) },
	})
	srv.refresh()
	assert.Equal(t, "ok", srv.state.Health)
}

// TestResolveServices_NilSvc — nil svc short-circuits.
func TestResolveServices_NilSvc(t *testing.T) {
	srv := &dashboardServer{svc: nil}
	states, asynqmonURL := srv.resolveServices()
	assert.Nil(t, states)
	assert.Empty(t, asynqmonURL)
}

// TestResolveServices_EmptySelected — selected=nil also short-circuits.
func TestResolveServices_EmptySelected(t *testing.T) {
	srv := &dashboardServer{svc: &devServices{selected: nil}}
	states, asynqmonURL := srv.resolveServices()
	assert.Nil(t, states)
	assert.Empty(t, asynqmonURL)
}

// TestResolveServices_QueryError — queryServiceStates exits non-zero.
func TestResolveServices_QueryError(t *testing.T) {
	withFakeExec(t, 1)
	srv := &dashboardServer{svc: &devServices{selected: []string{"db"}}}
	states, asynqmonURL := srv.resolveServices()
	assert.Nil(t, states)
	assert.Empty(t, asynqmonURL)
}
