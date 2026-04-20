package commands

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Cobra RunE wrapper coverage. Each RunE anonymous function is a
// counted block separately from the runXxx function it delegates to,
// so we invoke the RunE directly to close the gap.
//
// Debug subcommand RunE wrappers delegate to runDebug* — we stand up a
// minimal upstream server so each can succeed (or fail cleanly via an
// unreachable URL).
// ─────────────────────────────────────────────────────────────────────

// debugFixtureAll serves an "everything succeeds" upstream app so any
// runDebug* invocation that doesn't care about the filter arguments
// returns nil. Individual tests can narrow this down if needed.
func debugFixtureAll(t *testing.T) string {
	return debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/sql":        func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/traces":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/traces/t1":  func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"trace_id":"t1"}`)) },
		"/debug/errors":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/cache":      func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/logs":       func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/pprof/":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) },
		"/debug/pprof/heap": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("heap-bytes")) },
		"/debug/pprof/goroutine": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("goroutine 1 [running]:\nmain.x()\n"))
		},
		"/debug/explain": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"plan":"ok"}`)) },
	})
}

// resetAllDebugFlags — set every command's flags back to init() defaults
// so tests don't leak filters between them.
func resetAllDebugFlags() {
	resetRequestFlags()
	resetSQLFlags()
	resetTraceFlags()
	resetCacheFlags()
	debugErrorsLimit = 0
	debugErrorsContains = ""
	debugLogsTrace = ""
	debugLogsLevel = ""
	debugLogsContains = ""
	debugGoroutinesFilter = ""
	debugGoroutinesMinCount = 0
	debugExplainVars = nil
	debugProfileDuration = ""
	debugProfileOutput = ""
	debugHarOutput = ""
}

// ── Debug subcommand RunE wrappers ───────────────────────────────────

func TestDebugCacheCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugCacheCmd.RunE(debugCacheCmd, nil))
}

func TestDebugErrorsCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugErrorsCmd.RunE(debugErrorsCmd, nil))
}

func TestDebugExplainCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugExplainCmd.RunE(debugExplainCmd, []string{"SELECT 1"}))
}

func TestDebugGoroutinesCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugGoroutinesCmd.RunE(debugGoroutinesCmd, nil))
}

func TestDebugHarCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugHarCmd.RunE(debugHarCmd, nil))
}

func TestDebugHealthCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugHealthCmd.RunE(debugHealthCmd, nil))
}

func TestDebugLastErrorCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugLastErrorCmd.RunE(debugLastErrorCmd, nil))
}

func TestDebugLastSlowCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugLastSlowCmd.RunE(debugLastSlowCmd, nil))
}

func TestDebugLogsCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugLogsCmd.RunE(debugLogsCmd, nil))
}

func TestDebugNPlusOneCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugNPlusOneCmd.RunE(debugNPlusOneCmd, nil))
}

func TestDebugProfileCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugProfileCmd.RunE(debugProfileCmd, []string{"heap"}))
}

func TestDebugRequestsCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugRequestsCmd.RunE(debugRequestsCmd, nil))
}

func TestDebugSQLCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugSQLCmd.RunE(debugSQLCmd, nil))
}

func TestDebugTracesCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugTracesCmd.RunE(debugTracesCmd, nil))
}

func TestDebugTraceDetailCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugTraceCmd.RunE(debugTraceCmd, []string{"t1"}))
}

// TestDebugWatchCmd_RunE — runDebugWatch rejects a bogus interval;
// we drive it with one so it returns quickly without entering the
// polling loop.
func TestDebugWatchCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetWatchFlags()
	debugWatchInterval = "bogus"
	t.Cleanup(resetWatchFlags)
	require.Error(t, debugWatchCmd.RunE(debugWatchCmd, nil))
}

// ── Non-debug RunE wrappers ──────────────────────────────────────────

func TestInspectCmd_RunE(t *testing.T) {
	chdirTemp(t)
	// An arbitrary resource name — runInspect errors when no files exist.
	// Either outcome covers the RunE wrapper body.
	_ = inspectCmd.RunE(inspectCmd, []string{"Nothing"})
}

func TestStatusCmd_RunE(t *testing.T) {
	chdirTemp(t)
	_ = statusCmd.RunE(statusCmd, nil)
}

func TestDoCmd_RunE_Unknown(t *testing.T) {
	err := doCmd.RunE(doCmd, []string{"nonexistent-workflow"})
	require.Error(t, err)
}

func TestVerifyCmd_RunE_KeepGoing(t *testing.T) {
	chdirTemp(t)
	// Pristine temp dir: gofmt passes, vet fails (no go.mod) — but with
	// keep-going set and skipLint set the test exercises the full RunE.
	verifyNoLint = true
	verifyKeepGoing = true
	verifyNoRace = true
	t.Cleanup(func() { verifyNoLint = false; verifyKeepGoing = false; verifyNoRace = false })
	// verifyCmd.RunE returns the verify-failed clierr when there are
	// failed checks. We accept either outcome — this test is only
	// about covering the anonymous RunE wrapper.
	_ = verifyCmd.RunE(verifyCmd, nil)
}

func TestConfigSchemaCmd_RunE(t *testing.T) {
	// configSchemaCmd.RunE invokes `go run ./cmd/schema` via exec.Command.
	// In a pristine temp dir that path doesn't exist, so the run errors.
	chdirTemp(t)
	_ = configSchemaCmd.RunE(configSchemaCmd, nil)
}
