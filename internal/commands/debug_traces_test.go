package commands

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunDebugTracesList_HappyPath(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/traces": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, sampleTraces()) },
	})
	withDebugAppURL(t, url)
	resetTraceFlags()
	require.NoError(t, runDebugTracesList())
}

func TestRunDebugTracesList_Filtered(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/traces": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, sampleTraces()) },
	})
	withDebugAppURL(t, url)
	resetTraceFlags()
	debugTracesStatus = "error"
	debugTracesLimit = 1
	t.Cleanup(resetTraceFlags)
	require.NoError(t, runDebugTracesList())
}

func TestRunDebugTracesList_BadDuration(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/traces": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, []scrapedTrace{}) },
	})
	withDebugAppURL(t, url)
	resetTraceFlags()
	debugTracesSlowerThan = "xyz"
	t.Cleanup(resetTraceFlags)
	require.Error(t, runDebugTracesList())
}

func TestRunDebugTraceDetail_HappyPath(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/traces/abc": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, scrapedTrace{
				TraceID: "abc", RootName: "GET /x", DurationMS: 10, SpanCount: 1,
				Time:  time.Now(),
				Spans: []scrapedSpan{{SpanID: "r", Name: "root", DurationMS: 10}},
			})
		},
	})
	withDebugAppURL(t, url)
	resetTraceFlags()
	debugTraceWithStacks = true
	t.Cleanup(resetTraceFlags)
	require.NoError(t, runDebugTraceDetail("abc"))
}

func TestRunDebugTraceDetail_NotFound(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/traces/missing": func(w http.ResponseWriter, _ *http.Request) {
			http.NotFound(w, nil)
		},
	})
	withDebugAppURL(t, url)
	require.Error(t, runDebugTraceDetail("missing"))
}

func sampleTraces() []scrapedTrace {
	now := time.Now()
	return []scrapedTrace{
		{TraceID: "t1", RootName: "GET /users", Time: now, DurationMS: 12, Status: "ok", SpanCount: 4},
		{TraceID: "t2", RootName: "POST /orders", Time: now, DurationMS: 612, Status: "ok", SpanCount: 23},
		{TraceID: "t3", RootName: "POST /reports", Time: now, DurationMS: 350, Status: "error", SpanCount: 9},
	}
}

// TestApplyTraceFilters_SlowerThan — duration filter.
func TestApplyTraceFilters_SlowerThan(t *testing.T) {
	resetTraceFlags()
	debugTracesSlowerThan = "200ms"
	got, err := applyTraceFilters(sampleTraces())
	require.NoError(t, err)
	assert.Len(t, got, 2) // 612ms and 350ms
}

// TestApplyTraceFilters_Status — error only.
func TestApplyTraceFilters_Status(t *testing.T) {
	resetTraceFlags()
	debugTracesStatus = "error"
	got, err := applyTraceFilters(sampleTraces())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "t3", got[0].TraceID)
}

// TestApplyTraceFilters_InvalidStatus — rejects anything other than
// ok/error with DEBUG_BAD_FILTER.
func TestApplyTraceFilters_InvalidStatus(t *testing.T) {
	resetTraceFlags()
	debugTracesStatus = "fubar"
	_, err := applyTraceFilters(sampleTraces())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fubar")
}

// TestRunDebugTracesList_DevtoolsError — unreachable app URL short-
// circuits the requireDevtools pre-check.
func TestRunDebugTracesList_DevtoolsError(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	resetTraceFlags()
	require.Error(t, runDebugTracesList())
}

// TestRunDebugTracesList_GetJSONError — /debug/traces returns 500.
func TestRunDebugTracesList_GetJSONError(t *testing.T) {
	url := debug500(t, "/debug/traces")
	withDebugAppURL(t, url)
	resetTraceFlags()
	require.Error(t, runDebugTracesList())
}

// TestRunDebugTracesList_LimitTrims — --limit shortens the output.
func TestRunDebugTracesList_LimitTrims(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/traces": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, sampleTraces()) },
	})
	withDebugAppURL(t, url)
	resetTraceFlags()
	debugTracesLimit = 1
	t.Cleanup(resetTraceFlags)
	require.NoError(t, runDebugTracesList())
}

// TestRunDebugTracesList_EmptyFiltered — no traces match but filters
// were present; renderer reports the empty set.
func TestRunDebugTracesList_EmptyFiltered(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/traces": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, []scrapedTrace{}) },
	})
	withDebugAppURL(t, url)
	resetTraceFlags()
	// Set a filter so the filters map is populated.
	debugTracesStatus = "error"
	t.Cleanup(resetTraceFlags)
	require.NoError(t, runDebugTracesList())
}

// TestRunDebugTraceDetail_DevtoolsError — unreachable app URL short-
// circuits the requireDevtools pre-check.
func TestRunDebugTraceDetail_DevtoolsError(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	require.Error(t, runDebugTraceDetail("t1"))
}

// TestDebugTracesCmd_RunE — exercises the Cobra RunE wrapper.
func TestDebugTracesCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugTracesCmd.RunE(debugTracesCmd, nil))
}

// TestDebugTraceDetailCmd_RunE — exercises the Cobra RunE wrapper.
func TestDebugTraceDetailCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugTraceCmd.RunE(debugTraceCmd, []string{"t1"}))
}

func resetTraceFlags() {
	debugTracesSlowerThan = ""
	debugTracesStatus = ""
	debugTracesLimit = 0
	debugTraceWithStacks = false
}
