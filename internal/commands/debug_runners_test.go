package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// End-to-end exercises for every runDebug<X> command. Each test
// stands up an httptest server that serves the /debug/* endpoints
// the command reads, flips debugAppURL, invokes the run function,
// and asserts on return status. Nothing here fakes cobra — the
// runners are called directly.
//
// Tests are clustered by command to keep navigation easy.
// ─────────────────────────────────────────────────────────────────────

// debugFixture stands up a test server that serves every /debug/*
// endpoint using the caller-supplied handler map. An entry for
// /debug/health is prepended so requireDevtools passes unless the
// caller overrides it.
func debugFixture(t *testing.T, handlers map[string]http.HandlerFunc) (url string) {
	t.Helper()
	mux := http.NewServeMux()
	// Default /debug/health → enabled unless the caller overrode it.
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
	return srv.URL
}

// withDebugAppURL sets the global --app-url for the duration of a
// test. Keeps test isolation without plumbing a real cobra cmd.
func withDebugAppURL(t *testing.T, url string) {
	t.Helper()
	saved := debugAppURL
	debugAppURL = url
	t.Cleanup(func() { debugAppURL = saved })
}

// writeJSON is a convenience so fixture handlers don't have to
// remember to set Content-Type.
func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// ── runDebugHealth ───────────────────────────────────────────────────

// TestRunDebugHealth_End2End — exercises the full function including
// the text renderer branch.
func TestRunDebugHealth_End2End(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/sql":      func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/traces":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/logs":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/errors":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/cache":    func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/pprof/":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) },
	})
	withDebugAppURL(t, url)
	require.NoError(t, runDebugHealth())
}

// ── runDebugRequests ──────────────────────────────────────────────────

func TestRunDebugRequests_HappyPath(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, sampleRequests())
		},
	})
	withDebugAppURL(t, url)
	resetRequestFlags()
	require.NoError(t, runDebugRequests())
}

func TestRunDebugRequests_EmptyRing(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedRequest{})
		},
	})
	withDebugAppURL(t, url)
	resetRequestFlags()
	require.NoError(t, runDebugRequests())
}

func TestRunDebugRequests_DevtoolsOff(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/health": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"devtools":"stub"}`))
		},
	})
	withDebugAppURL(t, url)
	resetRequestFlags()
	err := runDebugRequests()
	require.Error(t, err)
}

func TestRunDebugRequests_BadFilterPropagates(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, []scrapedRequest{}) },
	})
	withDebugAppURL(t, url)
	resetRequestFlags()
	debugRequestsSlowerThan = "not-a-duration"
	t.Cleanup(resetRequestFlags)
	require.Error(t, runDebugRequests())
}

func TestRunDebugRequests_LimitSlices(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, sampleRequests())
		},
	})
	withDebugAppURL(t, url)
	resetRequestFlags()
	debugRequestsLimit = 2
	t.Cleanup(resetRequestFlags)
	require.NoError(t, runDebugRequests())
}

// ── runDebugSQL ───────────────────────────────────────────────────────

func TestRunDebugSQL_HappyPath(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/sql": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, sampleQueries()) },
	})
	withDebugAppURL(t, url)
	resetSQLFlags()
	require.NoError(t, runDebugSQL())
}

func TestRunDebugSQL_ErrorsOnlyFilter(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/sql": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, sampleQueries()) },
	})
	withDebugAppURL(t, url)
	resetSQLFlags()
	debugSQLErrorsOnly = true
	t.Cleanup(resetSQLFlags)
	require.NoError(t, runDebugSQL())
}

func TestRunDebugSQL_BadDuration(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/sql": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, []scrapedQuery{}) },
	})
	withDebugAppURL(t, url)
	resetSQLFlags()
	debugSQLSlowerThan = "xyz"
	t.Cleanup(resetSQLFlags)
	require.Error(t, runDebugSQL())
}

// ── runDebugTracesList + runDebugTraceDetail ──────────────────────────

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

// ── runDebugLogs ──────────────────────────────────────────────────────

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

// ── runDebugErrors ────────────────────────────────────────────────────

func TestRunDebugErrors_HappyPath(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedException{
				{Time: time.Now(), Method: "GET", Path: "/boom",
					Recovered: "nil pointer deref",
					Stack:     []string{"app.go:1 main"}, TraceID: "t1"},
			})
		},
	})
	withDebugAppURL(t, url)
	debugErrorsLimit = 0
	require.NoError(t, runDebugErrors())
}

func TestRunDebugErrors_ContainsFilter(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedException{
				{Recovered: "nil pointer"},
				{Recovered: "divide by zero"},
			})
		},
	})
	withDebugAppURL(t, url)
	debugErrorsContains = "divide"
	debugErrorsLimit = 0
	t.Cleanup(func() { debugErrorsContains = ""; debugErrorsLimit = 0 })
	require.NoError(t, runDebugErrors())
}

func TestRunDebugErrors_Empty(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, []scrapedException{}) },
	})
	withDebugAppURL(t, url)
	require.NoError(t, runDebugErrors())
}

// ── runDebugCache ─────────────────────────────────────────────────────

func TestRunDebugCache_HappyPath(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/cache": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, sampleCacheOps()) },
	})
	withDebugAppURL(t, url)
	resetCacheFlags()
	require.NoError(t, runDebugCache())
}

func TestRunDebugCache_Empty(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/cache": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, []scrapedCache{}) },
	})
	withDebugAppURL(t, url)
	resetCacheFlags()
	require.NoError(t, runDebugCache())
}

func TestRunDebugCache_BadFilter(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/cache": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, []scrapedCache{}) },
	})
	withDebugAppURL(t, url)
	resetCacheFlags()
	debugCacheOp = "fubar"
	t.Cleanup(resetCacheFlags)
	require.Error(t, runDebugCache())
}

// ── runDebugGoroutines ────────────────────────────────────────────────

func TestRunDebugGoroutines_HappyPath(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/pprof/goroutine": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("goroutine 1 [running]:\nmain.x()\n"))
		},
	})
	withDebugAppURL(t, url)
	debugGoroutinesFilter = ""
	debugGoroutinesMinCount = 0
	require.NoError(t, runDebugGoroutines())
}

func TestRunDebugGoroutines_Filtered(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/pprof/goroutine": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("goroutine 1 [running]:\nmain.x()\n"))
		},
	})
	withDebugAppURL(t, url)
	debugGoroutinesFilter = "sync"
	debugGoroutinesMinCount = 5
	t.Cleanup(func() { debugGoroutinesFilter = ""; debugGoroutinesMinCount = 0 })
	require.NoError(t, runDebugGoroutines())
}

func TestRunDebugGoroutines_EndpointError(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/pprof/goroutine": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	})
	withDebugAppURL(t, url)
	require.Error(t, runDebugGoroutines())
}

// ── runDebugExplain ───────────────────────────────────────────────────

func TestRunDebugExplain_HappyPath(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/explain": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, explainResponse{Plan: "Seq Scan on users"})
		},
	})
	withDebugAppURL(t, url)
	debugExplainVars = []string{"42"}
	t.Cleanup(func() { debugExplainVars = nil })
	require.NoError(t, runDebugExplain("SELECT * FROM users WHERE id = ?"))
}

func TestRunDebugExplain_RejectsNonSelect(t *testing.T) {
	url := debugFixture(t, nil)
	withDebugAppURL(t, url)
	err := runDebugExplain("UPDATE users SET x = 1")
	require.Error(t, err)
}

func TestRunDebugExplain_AppRejects(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/explain": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		},
	})
	withDebugAppURL(t, url)
	require.Error(t, runDebugExplain("SELECT 1"))
}

// ── runDebugNPlusOne ──────────────────────────────────────────────────

func TestRunDebugNPlusOne_WithFindings(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/sql": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedQuery{
				{TraceID: "t", SQL: "SELECT * FROM x WHERE id = 1"},
				{TraceID: "t", SQL: "SELECT * FROM x WHERE id = 2"},
				{TraceID: "t", SQL: "SELECT * FROM x WHERE id = 3"},
			})
		},
	})
	withDebugAppURL(t, url)
	require.NoError(t, runDebugNPlusOne())
}

func TestRunDebugNPlusOne_NoFindings(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/sql": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, []scrapedQuery{}) },
	})
	withDebugAppURL(t, url)
	require.NoError(t, runDebugNPlusOne())
}

// ── runDebugProfile ───────────────────────────────────────────────────

func TestRunDebugProfile_HappyPath(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/pprof/heap": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("profile-bytes"))
		},
	})
	withDebugAppURL(t, url)
	debugProfileDuration = ""
	debugProfileOutput = ""
	require.NoError(t, runDebugProfile("heap"))
}

func TestRunDebugProfile_WritesFile(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/pprof/heap": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("profile-bytes"))
		},
	})
	withDebugAppURL(t, url)
	tmp := t.TempDir() + "/heap.pprof"
	debugProfileOutput = tmp
	t.Cleanup(func() { debugProfileOutput = "" })
	require.NoError(t, runDebugProfile("heap"))
}

func TestRunDebugProfile_UnknownKind(t *testing.T) {
	err := runDebugProfile("nonexistent")
	require.Error(t, err)
}

func TestRunDebugProfile_BadDuration(t *testing.T) {
	url := debugFixture(t, nil)
	withDebugAppURL(t, url)
	debugProfileDuration = "xyz"
	t.Cleanup(func() { debugProfileDuration = "" })
	require.Error(t, runDebugProfile("cpu"))
}

// ── runDebugHar ───────────────────────────────────────────────────────

func TestRunDebugHar_HappyPath(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedRequest{
				{Method: "GET", Path: "/x", Status: 200},
			})
		},
	})
	withDebugAppURL(t, url)
	debugHarOutput = ""
	require.NoError(t, runDebugHar())
}

func TestRunDebugHar_WritesFile(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedRequest{{Method: "GET", Path: "/x", Status: 200}})
		},
	})
	withDebugAppURL(t, url)
	debugHarOutput = t.TempDir() + "/out.har"
	t.Cleanup(func() { debugHarOutput = "" })
	require.NoError(t, runDebugHar())
}
