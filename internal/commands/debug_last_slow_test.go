package commands

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntToStr — exercised here so the helper isn't orphaned if
// callers go away.
func TestIntToStr(t *testing.T) {
	cases := map[int]string{
		0:   "0",
		9:   "9",
		10:  "10",
		123: "123",
		-1:  "-1",
		-42: "-42",
	}
	for in, want := range cases {
		assert.Equal(t, want, intToStr(in), "input=%d", in)
	}
}

// resetLastSlowFlags restores defaults so tests don't leak into
// neighbors. Matches the init() defaults in debug_last_slow.go.
func resetLastSlowFlags() {
	debugLastSlowThreshold = "200ms"
	debugLastSlowWithTrace = true
	debugLastSlowWithLogs = true
	debugLastSlowWithSQL = true
	debugLastSlowWithStack = false
}

// lastSlowFixture returns an app URL that serves a consistent picture:
// one slow request (600ms), matching trace + logs + SQL.
func lastSlowFixture(t *testing.T) string {
	t.Helper()
	traceID := "abc123"
	handlers := map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedRequest{
				{Time: time.Now(), Method: "POST", Path: "/api/v1/orders",
					Status: 200, DurationMS: 612, TraceID: traceID},
				{Time: time.Now(), Method: "GET", Path: "/fast",
					Status: 200, DurationMS: 10, TraceID: "other"},
			})
		},
		"/debug/traces/" + traceID: func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, scrapedTrace{
				TraceID: traceID, RootName: "POST /api/v1/orders",
				DurationMS: 612, SpanCount: 3,
				Spans: []scrapedSpan{
					{SpanID: "r", Name: "root", DurationMS: 612},
					{SpanID: "c", ParentID: "r", Name: "child", DurationMS: 100,
						Stack: []string{"app/svc.go:1 fn"}},
				},
			})
		},
		"/debug/logs": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("trace_id") != traceID {
				writeJSON(w, []scrapedLog{})
				return
			}
			writeJSON(w, []scrapedLog{
				{Time: time.Now(), Level: "INFO", Message: "hi",
					TraceID: traceID, Attrs: map[string]string{"user": "u42"}},
			})
		},
		"/debug/sql": func(w http.ResponseWriter, _ *http.Request) {
			// Include one query for the trace (to surface in SQL) plus
			// three duplicates for N+1 detection coverage.
			writeJSON(w, []scrapedQuery{
				{TraceID: traceID, SQL: "SELECT * FROM users WHERE id = 1", DurationMS: 5},
				{TraceID: traceID, SQL: "SELECT * FROM users WHERE id = 2", DurationMS: 5},
				{TraceID: traceID, SQL: "SELECT * FROM users WHERE id = 3", DurationMS: 5},
				{TraceID: "other", SQL: "SELECT 1", DurationMS: 1},
			})
		},
	}
	return debugFixture(t, handlers)
}

func TestRunDebugLastSlow_HappyPath(t *testing.T) {
	url := lastSlowFixture(t)
	withDebugAppURL(t, url)
	resetLastSlowFlags()
	require.NoError(t, runDebugLastSlow())
}

func TestRunDebugLastSlow_WithStacks(t *testing.T) {
	url := lastSlowFixture(t)
	withDebugAppURL(t, url)
	resetLastSlowFlags()
	debugLastSlowWithStack = true
	t.Cleanup(resetLastSlowFlags)
	require.NoError(t, runDebugLastSlow())
}

func TestRunDebugLastSlow_OnlyRequest(t *testing.T) {
	// With every --with-* disabled, only the request is fetched and
	// bundled — covers the short-circuit branches in enrich.
	url := lastSlowFixture(t)
	withDebugAppURL(t, url)
	resetLastSlowFlags()
	debugLastSlowWithTrace = false
	debugLastSlowWithLogs = false
	debugLastSlowWithSQL = false
	t.Cleanup(resetLastSlowFlags)
	require.NoError(t, runDebugLastSlow())
}

func TestRunDebugLastSlow_NoSlowRequests(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedRequest{
				{Method: "GET", Path: "/fast", Status: 200, DurationMS: 5},
			})
		},
	})
	withDebugAppURL(t, url)
	resetLastSlowFlags()
	require.NoError(t, runDebugLastSlow())
}

func TestRunDebugLastSlow_BadThreshold(t *testing.T) {
	url := lastSlowFixture(t)
	withDebugAppURL(t, url)
	resetLastSlowFlags()
	debugLastSlowThreshold = "not-a-duration"
	t.Cleanup(resetLastSlowFlags)
	require.Error(t, runDebugLastSlow())
}

func TestRunDebugLastSlow_SubFetchFailuresGracefullyDegrade(t *testing.T) {
	// Requests succeed but every sub-fetch returns 500; the command
	// should still return nil (partial data better than failure).
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedRequest{
				{Method: "POST", Path: "/x", Status: 200,
					DurationMS: 600, TraceID: "t1"},
			})
		},
		"/debug/traces/t1": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
		"/debug/logs": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
		"/debug/sql": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	})
	withDebugAppURL(t, url)
	resetLastSlowFlags()
	require.NoError(t, runDebugLastSlow())
}

func TestRunDebugLastSlow_RequestFetchFailureBubbles(t *testing.T) {
	// If /debug/requests itself fails, the command returns an error —
	// there's nothing to report against.
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	})
	withDebugAppURL(t, url)
	resetLastSlowFlags()
	require.Error(t, runDebugLastSlow())
}

func TestFindLatestSlowRequest_HappyPath(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedRequest{
				{DurationMS: 50, TraceID: "a"},
				{DurationMS: 300, TraceID: "b"},
			})
		},
	})
	picked, total, err := findLatestSlowRequest(url, 100*time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	if assert.NotNil(t, picked) {
		assert.Equal(t, "b", picked.TraceID)
	}
}

func TestFindLatestSlowRequest_NoMatch(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedRequest{{DurationMS: 5}})
		},
	})
	picked, total, err := findLatestSlowRequest(url, 100*time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Nil(t, picked)
}

func TestFindLatestSlowRequest_EndpointError(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	})
	_, _, err := findLatestSlowRequest(url, 100*time.Millisecond)
	require.Error(t, err)
}

func TestFetchTrace_HappyPath(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/traces/t1": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, scrapedTrace{TraceID: "t1"})
		},
	})
	tr := fetchTrace(url, "t1")
	require.NotNil(t, tr)
	assert.Equal(t, "t1", tr.TraceID)
}

func TestFetchTrace_Failure(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/traces/missing": func(w http.ResponseWriter, _ *http.Request) {
			http.NotFound(w, nil)
		},
	})
	assert.Nil(t, fetchTrace(url, "missing"))
}

func TestFetchLogsForTrace(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/logs": func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "t1", r.URL.Query().Get("trace_id"))
			writeJSON(w, []scrapedLog{{Message: "hi", TraceID: "t1"}})
		},
	})
	logs := fetchLogsForTrace(url, "t1")
	require.Len(t, logs, 1)
	assert.Equal(t, "hi", logs[0].Message)
}

func TestFetchSQLForTrace_FiltersClientSide(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/sql": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedQuery{
				{TraceID: "t1", SQL: "a"},
				{TraceID: "t2", SQL: "b"},
				{TraceID: "t1", SQL: "c"},
			})
		},
	})
	got := fetchSQLForTrace(url, "t1")
	require.Len(t, got, 2)
	for _, q := range got {
		assert.Equal(t, "t1", q.TraceID)
	}
}

func TestFetchSQLForTrace_EndpointFailure(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/sql": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	})
	assert.Nil(t, fetchSQLForTrace(url, "t1"))
}

// TestRunDebugLastSlow_DevtoolsError — unreachable app URL short-
// circuits the requireDevtools pre-check.
func TestRunDebugLastSlow_DevtoolsError(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	require.Error(t, runDebugLastSlow())
}

// TestEnrichLastSlowReport_NoTraceID — picked has no trace ID →
// function returns early without populating any sub-field.
func TestEnrichLastSlowReport_NoTraceID(t *testing.T) {
	report := &lastSlowReport{}
	picked := &scrapedRequest{} // no TraceID
	enrichLastSlowReport("http://irrelevant", report, picked)
	assert.Empty(t, report.Trace)
}

// TestDebugLastSlowCmd_RunE — exercises the Cobra RunE wrapper.
func TestDebugLastSlowCmd_RunE(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetAllDebugFlags()
	require.NoError(t, debugLastSlowCmd.RunE(debugLastSlowCmd, nil))
}
