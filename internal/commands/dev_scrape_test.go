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
