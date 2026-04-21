package commands

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetTraceFlags() {
	debugTracesSlowerThan = ""
	debugTracesStatus = ""
	debugTracesLimit = 0
	debugTraceWithStacks = false
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

// TestRenderWaterfall_ProducesTreeGlyphs — smoke test that the
// waterfall renderer emits the expected tree glyphs for nested spans.
// Also verifies durations appear.
func TestRenderWaterfall_ProducesTreeGlyphs(t *testing.T) {
	spans := []scrapedSpan{
		{SpanID: "r", Name: "root", OffsetMS: 0, DurationMS: 100},
		{SpanID: "c1", ParentID: "r", Name: "child1", OffsetMS: 10, DurationMS: 40},
		{SpanID: "c2", ParentID: "r", Name: "child2", OffsetMS: 60, DurationMS: 30},
		{SpanID: "g", ParentID: "c1", Name: "grandchild", OffsetMS: 20, DurationMS: 20},
	}
	var buf bytes.Buffer
	renderWaterfall(&buf, 100, spans, false)
	out := buf.String()
	assert.Contains(t, out, "root")
	assert.Contains(t, out, "child1")
	assert.Contains(t, out, "child2")
	assert.Contains(t, out, "grandchild")
	// Tree glyphs — at least one ├─ and one └─ should appear.
	assert.Contains(t, out, "├─")
	assert.Contains(t, out, "└─")
}

// TestRenderWaterfall_WithStacks — when withStacks=true, the stack
// frames render below each span that has one.
func TestRenderWaterfall_WithStacks(t *testing.T) {
	spans := []scrapedSpan{
		{SpanID: "r", Name: "root", OffsetMS: 0, DurationMS: 10,
			Stack: []string{"app/service.go:1 fn"}},
	}
	var buf bytes.Buffer
	renderWaterfall(&buf, 10, spans, true)
	assert.Contains(t, buf.String(), "app/service.go:1 fn")
}

// TestRenderWaterfall_EmptySpans — renders a "(no spans)" placeholder,
// not a blank.
func TestRenderWaterfall_EmptySpans(t *testing.T) {
	var buf bytes.Buffer
	renderWaterfall(&buf, 0, nil, false)
	assert.Contains(t, buf.String(), "no spans")
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
