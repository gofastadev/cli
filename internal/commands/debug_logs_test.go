package commands

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPadLevel_WidthFive — level strings right-padded to 5 chars so
// the message column stays aligned across log records.
func TestPadLevel_WidthFive(t *testing.T) {
	assert.Equal(t, "INFO ", padLevel("INFO"))
	assert.Equal(t, "WARN ", padLevel("WARN"))
	assert.Equal(t, "ERROR", padLevel("ERROR"))
	assert.Equal(t, "DEBUG", padLevel("DEBUG"))
	// Already >= 5 — truncate to 5 so we never bloat the column.
	assert.Equal(t, "LONGE", padLevel("LONGERLEVEL"))
	// Empty → five spaces.
	assert.Equal(t, "     ", padLevel(""))
}

// TestFormatAttrs_SortedKeys — attrs render as key=value, sorted so
// output is deterministic across runs.
func TestFormatAttrs_SortedKeys(t *testing.T) {
	attrs := map[string]string{"b": "2", "a": "1", "c": "3"}
	got := formatAttrs(attrs)
	// Strip ANSI color codes for the assertion.
	plain := stripANSI(got)
	assert.Contains(t, plain, "a=1, b=2, c=3")
}

// TestFormatAttrs_Empty — empty map returns empty string (no
// trailing braces / whitespace).
func TestFormatAttrs_Empty(t *testing.T) {
	assert.Equal(t, "", formatAttrs(nil))
	assert.Equal(t, "", formatAttrs(map[string]string{}))
}

// TestRunDebugLogs_DevtoolsError — unreachable app URL short-circuits
// the requireDevtools pre-check.
func TestRunDebugLogs_DevtoolsError(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	require.Error(t, runDebugLogs())
}

// TestRunDebugLogs_GetJSONError — /debug/logs returns 500.
func TestRunDebugLogs_GetJSONError(t *testing.T) {
	url := debug500(t, "/debug/logs")
	withDebugAppURL(t, url)
	require.Error(t, runDebugLogs())
}

// TestDebugLogsCmd_RunE — exercises the Cobra RunE wrapper.
func TestDebugLogsCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugLogsCmd.RunE(debugLogsCmd, nil))
}

func TestRunDebugLogs_HappyPath(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/logs": func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "abc", r.URL.Query().Get("trace_id"))
			writeJSON(w, []scrapedLog{
				{Time: time.Now(), Level: "INFO", Message: "hi",
					Attrs: map[string]string{"k": "v"}, TraceID: "abc"},
			})
		},
	})
	withDebugAppURL(t, url)
	debugLogsTrace = "abc"
	t.Cleanup(func() { debugLogsTrace = ""; debugLogsLevel = ""; debugLogsContains = "" })
	require.NoError(t, runDebugLogs())
}

func TestRunDebugLogs_Empty(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/logs": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, []scrapedLog{}) },
	})
	withDebugAppURL(t, url)
	debugLogsTrace = ""
	require.NoError(t, runDebugLogs())
}

func TestRunDebugLogs_ContainsFilter(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/logs": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedLog{
				{Time: time.Now(), Level: "INFO", Message: "hello world"},
				{Time: time.Now(), Level: "WARN", Message: "nothing here"},
			})
		},
	})
	withDebugAppURL(t, url)
	debugLogsContains = "hello"
	t.Cleanup(func() { debugLogsContains = "" })
	require.NoError(t, runDebugLogs())
}
