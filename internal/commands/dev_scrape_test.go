package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAppendTag_NoExistingGOFLAGS — fresh env, no GOFLAGS set. Returns
// a new GOFLAGS= value containing just the tag.
func TestAppendTag_NoExistingGOFLAGS(t *testing.T) {
	got := appendTag("", "devtools")
	assert.Equal(t, "GOFLAGS=-tags=devtools", got)
}

// TestAppendTag_WithOtherFlags — existing GOFLAGS has non-tag flags;
// we append a fresh -tags= fragment.
func TestAppendTag_WithOtherFlags(t *testing.T) {
	got := appendTag("-mod=mod", "devtools")
	assert.Equal(t, "GOFLAGS=-mod=mod -tags=devtools", got)
}

// TestAppendTag_WithExistingTags — existing -tags=foo; we merge the new
// tag in comma-separated form without duplication.
func TestAppendTag_WithExistingTags(t *testing.T) {
	got := appendTag("-tags=foo", "devtools")
	assert.Equal(t, "GOFLAGS=-tags=foo,devtools", got)
}

// TestAppendTag_TagAlreadyPresent — idempotent when the target tag is
// already present in the existing -tags= fragment.
func TestAppendTag_TagAlreadyPresent(t *testing.T) {
	got := appendTag("-tags=devtools,foo", "devtools")
	assert.Equal(t, "GOFLAGS=-tags=devtools,foo", got)
}

// TestAppendTag_AcceptsFullPrefix — tolerant of a "GOFLAGS=" prefix on
// the input string so callers don't have to strip it.
func TestAppendTag_AcceptsFullPrefix(t *testing.T) {
	got := appendTag("GOFLAGS=-mod=mod", "devtools")
	assert.Equal(t, "GOFLAGS=-mod=mod -tags=devtools", got)
}

// TestSumCounterFamily — exact matches on a counter family name with
// and without labels. Returns 0 for unknown families and ignores
// similarly-prefixed families.
func TestSumCounterFamily(t *testing.T) {
	body := `# HELP http_requests_total Total HTTP requests.
# TYPE http_requests_total counter
http_requests_total{method="GET",status="200"} 42
http_requests_total{method="POST",status="201"} 7
http_requests_total_bucket{le="0.5"} 999
http_in_flight_requests 3
`
	assert.Equal(t, int64(49), sumCounterFamily(body, "http_requests_total"))
	assert.Equal(t, int64(3), sumCounterFamily(body, "http_in_flight_requests"))
	assert.Equal(t, int64(0), sumCounterFamily(body, "nonexistent_family"))
}

// TestApproxLatencyMS — mean computed from sum/count, converted from
// seconds to milliseconds.
func TestApproxLatencyMS(t *testing.T) {
	body := `# TYPE http_request_duration_seconds histogram
http_request_duration_seconds_sum 1.5
http_request_duration_seconds_count 3
`
	ms := approxLatencyMS(body, "http_request_duration_seconds")
	assert.InDelta(t, 500.0, ms, 0.01) // 1.5 / 3 = 0.5s = 500ms
}

// TestApproxLatencyMS_ZeroCount — avoids divide-by-zero when the
// histogram has no samples yet.
func TestApproxLatencyMS_ZeroCount(t *testing.T) {
	body := `http_request_duration_seconds_sum 0
http_request_duration_seconds_count 0
`
	assert.Equal(t, 0.0, approxLatencyMS(body, "http_request_duration_seconds"))
}

// TestScrapeMetrics_FullFlow — stand up a real HTTP server that serves
// a Prometheus text response and verify scrapeMetrics reduces it to the
// expected snapshot.
func TestScrapeMetrics_FullFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`http_requests_total 10
http_in_flight_requests 2
http_request_duration_seconds_sum 1.0
http_request_duration_seconds_count 10
`))
	}))
	defer srv.Close()

	got := scrapeMetrics(srv.URL)
	assert.True(t, got.MetricsOK)
	assert.Equal(t, int64(10), got.RequestsTotal)
	assert.Equal(t, int64(2), got.InFlight)
	if assert.NotNil(t, got.LatencyP50MS) {
		assert.InDelta(t, 100.0, *got.LatencyP50MS, 0.01) // 1.0/10 = 100ms
	}
}

// TestScrapeMetrics_Unreachable — when /metrics is down, scrapeMetrics
// returns a zero snapshot with MetricsOK=false.
func TestScrapeMetrics_Unreachable(t *testing.T) {
	got := scrapeMetrics("http://127.0.0.1:1") // guaranteed-unused port
	assert.False(t, got.MetricsOK)
	assert.Equal(t, int64(0), got.RequestsTotal)
}

// TestDevtoolsAvailable — /debug/health returns enabled vs stub; the
// helper flips the bool accordingly.
func TestDevtoolsAvailable(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		expected bool
	}{
		{"enabled", `{"devtools":"enabled"}`, true},
		{"stub", `{"devtools":"stub"}`, false},
		{"unknown", `{"devtools":"wat"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			assert.Equal(t, tc.expected, devtoolsAvailable(srv.URL))
		})
	}
}

// TestScrapeRequestLog — the endpoint returns a JSON array of
// RequestEntry objects that we decode into scrapedRequest.
func TestScrapeRequestLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		entries := []scrapedRequest{
			{Method: "GET", Path: "/users", Status: 200, DurationMS: 12},
			{Method: "POST", Path: "/users", Status: 201, DurationMS: 45},
		}
		_ = json.NewEncoder(w).Encode(entries)
	}))
	defer srv.Close()

	got := scrapeRequestLog(srv.URL)
	assert.Len(t, got, 2)
	assert.Equal(t, "GET", got[0].Method)
	assert.Equal(t, 201, got[1].Status)
}

// TestScrapeRequestLog_404 — when the devtools tag isn't set the
// endpoint 404s; the scraper should return nil without panicking.
func TestScrapeRequestLog_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer srv.Close()
	assert.Nil(t, scrapeRequestLog(srv.URL))
}

// ── Goroutine dump parser ─────────────────────────────────────────────

// TestParseGoroutineDump_GroupsByTop — exercises the happy path: two
// goroutines parked in the same top function get grouped; a third
// goroutine in a different function lives in its own group.
func TestParseGoroutineDump_GroupsByTop(t *testing.T) {
	text := `goroutine 1 [running]:
main.run(0xdeadbeef)
	/app/main.go:42 +0x1

goroutine 2 [IO wait]:
net/http.(*conn).serve(0x123)
	/sdk/net/http/server.go:1 +0x10

goroutine 3 [IO wait]:
net/http.(*conn).serve(0x456)
	/sdk/net/http/server.go:1 +0x10
`
	snap := parseGoroutineDump(text)
	assert.Equal(t, 3, snap.Total)
	// The first (biggest) group should be net/http (count=2), not main.run (count=1).
	if assert.Len(t, snap.Groups, 2) {
		assert.Equal(t, "net/http.(*conn).serve", snap.Groups[0].Top)
		assert.Equal(t, 2, snap.Groups[0].Count)
		assert.Contains(t, snap.Groups[0].States, "IO wait")
		assert.Equal(t, "main.run", snap.Groups[1].Top)
		assert.Equal(t, 1, snap.Groups[1].Count)
	}
}

// TestParseGoroutineDump_Empty — empty input returns a zero snapshot.
func TestParseGoroutineDump_Empty(t *testing.T) {
	snap := parseGoroutineDump("")
	assert.Equal(t, 0, snap.Total)
	assert.Empty(t, snap.Groups)
}

// TestParseGoroutineDump_MalformedHeaderIsSkipped — a line that doesn't
// start with `goroutine ` is ignored. No crash, no false positives.
func TestParseGoroutineDump_MalformedHeaderIsSkipped(t *testing.T) {
	text := `not a goroutine
also junk
`
	snap := parseGoroutineDump(text)
	assert.Equal(t, 0, snap.Total)
}

// TestParseGoroutineDump_MissingState — a header without [state] still
// produces a group; State list stays empty.
func TestParseGoroutineDump_MissingState(t *testing.T) {
	text := `goroutine 42
foo.bar()
	/app/x.go:1 +0x2
`
	snap := parseGoroutineDump(text)
	assert.Equal(t, 1, snap.Total)
	if assert.Len(t, snap.Groups, 1) {
		assert.Equal(t, "foo.bar", snap.Groups[0].Top)
		assert.Empty(t, snap.Groups[0].States)
	}
}

// TestScrapeGoroutines_200 — integration-level path hitting a stub
// pprof server.
func TestScrapeGoroutines_200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/debug/pprof/goroutine", r.URL.Path)
		_, _ = w.Write([]byte("goroutine 1 [running]:\nmain.x()\n\n"))
	}))
	defer srv.Close()
	snap := scrapeGoroutines(srv.URL)
	assert.Equal(t, 1, snap.Total)
}

// TestScrapeGoroutines_404 — devtools tag off: scraper returns a zero
// snapshot rather than erroring out.
func TestScrapeGoroutines_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer srv.Close()
	assert.Zero(t, scrapeGoroutines(srv.URL).Total)
}

// ── N+1 detector ──────────────────────────────────────────────────────

// TestNormalizeSQL — quoted strings, numeric literals, and
// whitespace all collapse so two queries differing only in params
// produce the same template.
func TestNormalizeSQL(t *testing.T) {
	cases := map[string]string{
		"SELECT * FROM users WHERE id = 42":                     "SELECT * FROM users WHERE id = ?",
		"SELECT * FROM users WHERE id = 15":                     "SELECT * FROM users WHERE id = ?",
		"SELECT * FROM users WHERE email = 'alice@example.com'": "SELECT * FROM users WHERE email = ?",
		"SELECT\n  *\n  FROM users\n  WHERE id = 1":             "SELECT * FROM users WHERE id = ?",
		`SELECT * FROM users WHERE name = "Bob"`:                "SELECT * FROM users WHERE name = ?",
		"SELECT COUNT(*) FROM orders WHERE total > 100.50":      "SELECT COUNT(*) FROM orders WHERE total > ?",
	}
	for in, want := range cases {
		assert.Equal(t, want, normalizeSQL(in), "input: %q", in)
	}
}

// TestDetectNPlusOne_FlagsRepeatedTemplate — three or more queries
// sharing (trace_id, template) trigger a finding.
func TestDetectNPlusOne_FlagsRepeatedTemplate(t *testing.T) {
	queries := []scrapedQuery{
		{TraceID: "t1", SQL: "SELECT * FROM perms WHERE user_id = 1"},
		{TraceID: "t1", SQL: "SELECT * FROM perms WHERE user_id = 2"},
		{TraceID: "t1", SQL: "SELECT * FROM perms WHERE user_id = 3"},
		{TraceID: "t1", SQL: "SELECT * FROM users"},
	}
	findings := detectNPlusOne(queries)
	if assert.Len(t, findings, 1) {
		assert.Equal(t, "t1", findings[0].TraceID)
		assert.Equal(t, 3, findings[0].Count)
		assert.Equal(t, "SELECT * FROM perms WHERE user_id = ?", findings[0].Template)
	}
}

// TestDetectNPlusOne_RespectsThreshold — two repeats don't trip the
// detector. (The threshold is 3.)
func TestDetectNPlusOne_RespectsThreshold(t *testing.T) {
	queries := []scrapedQuery{
		{TraceID: "t1", SQL: "SELECT * FROM a WHERE id = 1"},
		{TraceID: "t1", SQL: "SELECT * FROM a WHERE id = 2"},
	}
	assert.Empty(t, detectNPlusOne(queries))
}

// TestDetectNPlusOne_IgnoresQueriesWithoutTraceID — queries captured
// before trace propagation (or from non-request contexts) can't be
// attributed to a request so they're excluded.
func TestDetectNPlusOne_IgnoresQueriesWithoutTraceID(t *testing.T) {
	queries := []scrapedQuery{
		{TraceID: "", SQL: "SELECT 1"},
		{TraceID: "", SQL: "SELECT 2"},
		{TraceID: "", SQL: "SELECT 3"},
	}
	assert.Empty(t, detectNPlusOne(queries))
}

// TestBuildHAR_RoundTripsCoreFields — produced HAR contains method,
// path, status, and response body. Shape roughly matches the HAR 1.2
// schema (has log.entries[].request/response).
func TestBuildHAR_RoundTripsCoreFields(t *testing.T) {
	reqs := []scrapedRequest{
		{
			Method:              "POST",
			Path:                "/api/v1/users",
			Status:              201,
			DurationMS:          12,
			Body:                `{"name":"Alice"}`,
			ResponseBody:        `{"id":"u1"}`,
			ResponseContentType: "application/json",
		},
	}
	har := buildHAR(reqs)
	assert.Equal(t, "1.2", har.Log.Version)
	if assert.Len(t, har.Log.Entries, 1) {
		e := har.Log.Entries[0]
		assert.Equal(t, "POST", e.Request.Method)
		assert.Equal(t, "/api/v1/users", e.Request.URL)
		if assert.NotNil(t, e.Request.PostData) {
			assert.Equal(t, `{"name":"Alice"}`, e.Request.PostData.Text)
		}
		assert.Equal(t, 201, e.Response.Status)
		assert.Equal(t, "application/json", e.Response.Content.MimeType)
		assert.Equal(t, `{"id":"u1"}`, e.Response.Content.Text)
		assert.Equal(t, int64(12), e.Time)
	}
}

// TestBuildHAR_EmptyRing — zero requests produces a valid-but-empty
// HAR doc rather than nil, so the download is still a parseable JSON.
func TestBuildHAR_EmptyRing(t *testing.T) {
	har := buildHAR(nil)
	assert.Equal(t, "1.2", har.Log.Version)
	assert.Empty(t, har.Log.Entries)
}

// TestDetectNPlusOne_SortsByCountDesc — the worst offender renders
// first so the dashboard's first row is the highest-priority fix.
func TestDetectNPlusOne_SortsByCountDesc(t *testing.T) {
	queries := []scrapedQuery{
		{TraceID: "t1", SQL: "A WHERE id = 1"},
		{TraceID: "t1", SQL: "A WHERE id = 2"},
		{TraceID: "t1", SQL: "A WHERE id = 3"},
		{TraceID: "t2", SQL: "B WHERE id = 1"},
		{TraceID: "t2", SQL: "B WHERE id = 2"},
		{TraceID: "t2", SQL: "B WHERE id = 3"},
		{TraceID: "t2", SQL: "B WHERE id = 4"},
	}
	findings := detectNPlusOne(queries)
	if assert.Len(t, findings, 2) {
		assert.Equal(t, 4, findings[0].Count) // t2/B first
		assert.Equal(t, 3, findings[1].Count)
	}
}
