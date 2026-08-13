package commands

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func resetLastErrorFlags() {
	debugLastErrorWithTrace = true
	debugLastErrorWithLogs = true
}

func TestRunDebugLastError_HappyPath(t *testing.T) {
	traceID := "err-trace"
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedException{
				{Time: time.Now(), Method: "GET", Path: "/boom",
					Recovered: "nil pointer deref",
					Stack:     []string{"app.go:1 main"}, TraceID: traceID},
			})
		},
		"/debug/traces/" + traceID: func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, scrapedTrace{TraceID: traceID, RootName: "GET /boom",
				DurationMS: 50, SpanCount: 1,
				Spans: []scrapedSpan{{SpanID: "r", Name: "root", DurationMS: 50}}})
		},
		"/debug/logs": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedLog{{Message: "oops", Level: "ERROR", TraceID: traceID}})
		},
	})
	withDebugAppURL(t, url)
	resetLastErrorFlags()
	require.NoError(t, runDebugLastError())
}

func TestRunDebugLastError_NoExceptions(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedException{})
		},
	})
	withDebugAppURL(t, url)
	resetLastErrorFlags()
	require.NoError(t, runDebugLastError())
}

func TestRunDebugLastError_WithoutTraceOrLogs(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedException{{Recovered: "x", TraceID: "t1"}})
		},
	})
	withDebugAppURL(t, url)
	resetLastErrorFlags()
	debugLastErrorWithTrace = false
	debugLastErrorWithLogs = false
	t.Cleanup(resetLastErrorFlags)
	require.NoError(t, runDebugLastError())
}

func TestRunDebugLastError_FailedFetch(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	})
	withDebugAppURL(t, url)
	resetLastErrorFlags()
	require.Error(t, runDebugLastError())
}

// TestRunDebugLastError_DevtoolsError — unreachable app URL short-
// circuits the requireDevtools pre-check.
func TestRunDebugLastError_DevtoolsError(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	require.Error(t, runDebugLastError())
}

// TestDebugLastErrorCmd_RunE — exercises the Cobra RunE wrapper.
func TestDebugLastErrorCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugLastErrorCmd.RunE(debugLastErrorCmd, nil))
}
