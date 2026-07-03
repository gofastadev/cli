package commands

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunDebugGoroutines_DevtoolsError — unreachable app URL short-
// circuits the requireDevtools pre-check.
func TestRunDebugGoroutines_DevtoolsError(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	require.Error(t, runDebugGoroutines())
}

// TestRunDebugGoroutines_EmptyStates — a goroutine dump whose state
// line is empty renders the "—" placeholder without error.
func TestRunDebugGoroutines_EmptyStates(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/pprof/goroutine": func(w http.ResponseWriter, _ *http.Request) {
			// Just one goroutine with empty-ish state; parseGoroutines
			// tolerates this.
			_, _ = w.Write([]byte("goroutine 1 []:\nmain.x()\n"))
		},
	})
	withDebugAppURL(t, url)
	debugGoroutinesFilter = ""
	debugGoroutinesMinCount = 0
	t.Cleanup(func() { debugGoroutinesFilter = ""; debugGoroutinesMinCount = 0 })
	require.NoError(t, runDebugGoroutines())
}

// TestRunDebugGoroutines_MinCountFilters — an impossibly high --min
// filters every group out; the empty-result render path fires.
func TestRunDebugGoroutines_MinCountFilters(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/pprof/goroutine": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("goroutine 1 [running]:\nmain.x()\n"))
		},
	})
	withDebugAppURL(t, url)
	debugGoroutinesMinCount = 100 // impossibly high → all filtered out
	debugGoroutinesFilter = ""
	t.Cleanup(func() { debugGoroutinesFilter = ""; debugGoroutinesMinCount = 0 })
	require.NoError(t, runDebugGoroutines())
}

// TestRunDebugGoroutines_FetchErrCoverage — /debug/health passes so
// requireDevtools succeeds, then the goroutine endpoint hijacks and
// closes the connection with no response, so the subsequent
// client.Get fails deterministically (the Get-error branch).
func TestRunDebugGoroutines_FetchErrCoverage(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/pprof/goroutine": func(w http.ResponseWriter, _ *http.Request) {
			hj, ok := w.(http.Hijacker)
			if !ok {
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				return
			}
			_ = conn.Close() // no HTTP response → client Get errors
		},
	})
	withDebugAppURL(t, url)
	debugGoroutinesFilter = ""
	debugGoroutinesMinCount = 0
	t.Cleanup(func() { debugGoroutinesFilter = ""; debugGoroutinesMinCount = 0 })
	require.Error(t, runDebugGoroutines())
}

// TestDebugGoroutinesCmd_RunE — exercises the Cobra RunE wrapper.
func TestDebugGoroutinesCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugGoroutinesCmd.RunE(debugGoroutinesCmd, nil))
}
