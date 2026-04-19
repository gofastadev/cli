package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for debug_watch. The top-level runDebugWatch runs an
// indefinite poll loop gated by os.Interrupt / SIGTERM — we test the
// helpers directly (pollChannel, watchMarks, watchEmitter,
// wrapWatchEvent) to drive coverage up without wrestling with
// goroutine lifetime.
// ─────────────────────────────────────────────────────────────────────

// resetWatchFlags puts every --with-* flag back to its init() default
// so tests don't leak into one another.
func resetWatchFlags() {
	debugWatchInterval = "1s"
	debugWatchRequests = true
	debugWatchErrors = true
	debugWatchSQL = false
	debugWatchCache = false
	debugWatchTrace = false
}

// TestWrapWatchEvent_AddsDiscriminator — the composer injects a
// top-level `event` field so jq consumers can branch on kind.
func TestWrapWatchEvent_AddsDiscriminator(t *testing.T) {
	payload := scrapedRequest{Method: "GET", Path: "/x"}
	got := wrapWatchEvent("request", payload)
	assert.Equal(t, "request", got["event"])
	assert.Equal(t, "GET", got["method"])
}

// TestWrapWatchEvent_NilPayload — unmarshal yields nil map; helper
// still emits a valid object with only the event discriminator.
func TestWrapWatchEvent_NilPayload(t *testing.T) {
	got := wrapWatchEvent("heartbeat", nil)
	assert.Equal(t, "heartbeat", got["event"])
}

// TestWatchEmitter_EmitEachKind — every emit* method writes one valid
// NDJSON line containing its own kind's discriminator.
func TestWatchEmitter_EmitEachKind(t *testing.T) {
	var buf bytes.Buffer
	e := newWatchEmitter(&buf)

	e.emitRequest(scrapedRequest{Method: "GET", Path: "/x"})
	e.emitSQL(scrapedQuery{SQL: "SELECT 1"})
	e.emitError(scrapedException{Recovered: "boom"})
	e.emitCache(scrapedCache{Op: "get"})
	e.emitTrace(scrapedTrace{TraceID: "t1"})
	e.heartbeat()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 6)
	kinds := []string{"request", "sql", "error", "cache", "trace", "heartbeat"}
	for i, line := range lines {
		var obj map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(line), &obj), "line=%s", line)
		assert.Equal(t, kinds[i], obj["event"])
	}
}

// TestPollChannel_EmitsOnlyNewerEntries — entries with Time older or
// equal to the high-water mark are skipped; the mark advances to the
// newest emitted entry's time.
func TestPollChannel_EmitsOnlyNewerEntries(t *testing.T) {
	t0 := time.Now().Add(-10 * time.Second)
	entries := []scrapedRequest{
		{Time: t0.Add(4 * time.Second), Method: "NEW2"},
		{Time: t0.Add(3 * time.Second), Method: "NEW1"},
		{Time: t0.Add(2 * time.Second), Method: "OLD"},
	}
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, entries)
		},
	})
	var emitted []string
	newMark := pollChannel(url, "/debug/requests",
		t0.Add(2*time.Second+500*time.Millisecond),
		func(r scrapedRequest) time.Time { return r.Time },
		func(r scrapedRequest) { emitted = append(emitted, r.Method) },
	)
	// Only NEW1 and NEW2 should emit; OLD equals mark (and is < by ε).
	assert.Equal(t, []string{"NEW1", "NEW2"}, emitted)
	assert.True(t, newMark.After(t0.Add(3*time.Second)))
}

// TestPollChannel_EndpointErrorKeepsMark — on HTTP failure the mark
// is preserved so the next tick tries again from the same cursor.
func TestPollChannel_EndpointErrorKeepsMark(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	})
	mark := time.Now()
	got := pollChannel(url, "/debug/requests", mark,
		func(r scrapedRequest) time.Time { return r.Time },
		func(r scrapedRequest) { t.Fatalf("emit should not have fired") },
	)
	assert.True(t, mark.Equal(got))
}

// TestWatchMarks_Baseline — each enabled channel gets its mark set
// to the newest entry currently in its ring.
func TestWatchMarks_Baseline(t *testing.T) {
	t0 := time.Now().Add(-1 * time.Hour)
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedRequest{
				{Time: t0.Add(5 * time.Minute)},
				{Time: t0},
			})
		},
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedException{{Time: t0.Add(10 * time.Minute)}})
		},
		"/debug/sql": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedQuery{{Time: t0.Add(2 * time.Minute)}})
		},
		"/debug/cache": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedCache{{Time: t0.Add(3 * time.Minute)}})
		},
		"/debug/traces": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedTrace{{Time: t0.Add(4 * time.Minute)}})
		},
	})
	resetWatchFlags()
	debugWatchSQL = true
	debugWatchCache = true
	debugWatchTrace = true
	t.Cleanup(resetWatchFlags)

	m := watchMarks{}
	m.baseline(url)

	// time.Time.Equal ignores the monotonic clock reading so the
	// JSON-round-tripped value compares equal to the constructed one.
	assert.True(t, t0.Add(5*time.Minute).Equal(m.request))
	assert.True(t, t0.Add(10*time.Minute).Equal(m.errorsMark))
	assert.True(t, t0.Add(2*time.Minute).Equal(m.sql))
	assert.True(t, t0.Add(3*time.Minute).Equal(m.cacheMark))
	assert.True(t, t0.Add(4*time.Minute).Equal(m.trace))
}

// TestWatchMarks_PollAndEmit_Integration — wire the full pipeline:
// baseline against empty rings, then deliver new entries and confirm
// they all emit with the right discriminator.
func TestWatchMarks_PollAndEmit_Integration(t *testing.T) {
	// Mutable backing slices so we can swap the ring contents between
	// baseline and the emit call.
	var reqs []scrapedRequest
	var errs []scrapedException
	var sqls []scrapedQuery
	var caches []scrapedCache
	var traces []scrapedTrace

	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, reqs) },
		"/debug/errors":   func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, errs) },
		"/debug/sql":      func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, sqls) },
		"/debug/cache":    func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, caches) },
		"/debug/traces":   func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, traces) },
	})

	resetWatchFlags()
	debugWatchSQL = true
	debugWatchCache = true
	debugWatchTrace = true
	t.Cleanup(resetWatchFlags)

	m := watchMarks{}
	m.baseline(url) // every channel empty → marks stay at zero time

	// Now populate the rings with one entry each, all strictly newer
	// than the zero baseline.
	now := time.Now()
	reqs = []scrapedRequest{{Time: now, Method: "GET", Path: "/x"}}
	errs = []scrapedException{{Time: now, Recovered: "boom"}}
	sqls = []scrapedQuery{{Time: now, SQL: "SELECT 1"}}
	caches = []scrapedCache{{Time: now, Op: "get"}}
	traces = []scrapedTrace{{Time: now, TraceID: "t1"}}

	var buf bytes.Buffer
	emitter := newWatchEmitter(&buf)
	m.pollAndEmit(url, emitter)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 5)

	// Each line should carry the expected event discriminator.
	seen := map[string]bool{}
	for _, line := range lines {
		var obj map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(line), &obj))
		seen[obj["event"].(string)] = true
	}
	for _, kind := range []string{"request", "error", "sql", "cache", "trace"} {
		assert.True(t, seen[kind], "missing event kind %q", kind)
	}
}

// TestWatchMarks_PollAndEmit_FailuresPreserveMarks — every channel's
// endpoint fails; marks stay put so the next poll retries from the
// same cursor.
func TestWatchMarks_PollAndEmit_FailuresPreserveMarks(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) },
		"/debug/errors":   func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) },
	})
	resetWatchFlags()
	t.Cleanup(resetWatchFlags)

	original := time.Now()
	m := watchMarks{request: original, errorsMark: original}
	var buf bytes.Buffer
	emitter := newWatchEmitter(&buf)
	m.pollAndEmit(url, emitter)

	assert.True(t, original.Equal(m.request))
	assert.True(t, original.Equal(m.errorsMark))
	assert.Empty(t, buf.String())
}

// TestRunDebugWatch_UnreachableApp — the outer function should
// surface requireDevtools's error without entering the poll loop.
func TestRunDebugWatch_UnreachableApp(t *testing.T) {
	withDebugAppURL(t, "http://127.0.0.1:1")
	resetWatchFlags()
	require.Error(t, runDebugWatch())
}

// TestRunDebugWatch_BadInterval — rejects bogus --interval with
// DEBUG_BAD_DURATION.
func TestRunDebugWatch_BadInterval(t *testing.T) {
	url := debugFixture(t, nil)
	withDebugAppURL(t, url)
	resetWatchFlags()
	debugWatchInterval = "not-a-duration"
	t.Cleanup(resetWatchFlags)
	require.Error(t, runDebugWatch())
}

// TestWatchMarks_BaselineEmptyRings — every channel's ring is
// empty so baseline leaves every mark at zero time.
func TestWatchMarks_BaselineEmptyRings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/debug/health" {
			_, _ = w.Write([]byte(`{"devtools":"enabled"}`))
			return
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	resetWatchFlags()
	debugWatchSQL = true
	debugWatchCache = true
	debugWatchTrace = true
	t.Cleanup(resetWatchFlags)

	m := watchMarks{}
	m.baseline(srv.URL)
	assert.True(t, m.request.IsZero())
	assert.True(t, m.sql.IsZero())
	assert.True(t, m.cacheMark.IsZero())
	assert.True(t, m.trace.IsZero())
}
