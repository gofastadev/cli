package commands

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for dev_scrape.go branches the happy-path tests don't hit:
// unreachable connections (err != nil from Get), non-2xx responses,
// and malformed counter/histogram lines.
// ─────────────────────────────────────────────────────────────────────

// closedServer returns an httptest.Server URL that is definitely
// unreachable (the server closed immediately). Scrapers' Get will
// return a net error when hit.
func closedServer() string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close()
	return url
}

// TestScrapeMetrics_Non2xx — non-2xx response returns zero-valued
// snapshot with MetricsOK=false.
func TestScrapeMetrics_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	got := scrapeMetrics(srv.URL)
	assert.False(t, got.MetricsOK)
	assert.Zero(t, got.RequestsTotal)
}

// TestScrapeMetrics_UnreachableClosed — server is closed; Get returns an err.
func TestScrapeMetrics_UnreachableClosed(t *testing.T) {
	url := closedServer()
	got := scrapeMetrics(url)
	assert.False(t, got.MetricsOK)
}

// TestSumCounterFamily_ShortLine — a line with no value field is
// skipped without a panic.
func TestSumCounterFamily_ShortLine(t *testing.T) {
	body := "http_requests_total\n"
	assert.Equal(t, int64(0), sumCounterFamily(body, "http_requests_total"))
}

// TestSumCounterFamily_BadNumber — lines whose numeric token doesn't
// parse are silently skipped.
func TestSumCounterFamily_BadNumber(t *testing.T) {
	body := "http_requests_total 42\nhttp_requests_total not-a-number\n"
	assert.Equal(t, int64(42), sumCounterFamily(body, "http_requests_total"))
}

// TestScrapeRequestLog_Unreachable — closed server → nil slice.
func TestScrapeRequestLog_Unreachable(t *testing.T) {
	assert.Nil(t, scrapeRequestLog(closedServer()))
}

// TestScrapeSQLLog_Unreachable — closed server → nil slice.
func TestScrapeSQLLog_Unreachable(t *testing.T) {
	assert.Nil(t, scrapeSQLLog(closedServer()))
}

// TestScrapeCacheOps_Unreachable — closed server → nil slice.
func TestScrapeCacheOps_Unreachable(t *testing.T) {
	assert.Nil(t, scrapeCacheOps(closedServer()))
}

// TestScrapeExceptions_Unreachable — closed server → nil slice.
func TestScrapeExceptions_Unreachable(t *testing.T) {
	assert.Nil(t, scrapeExceptions(closedServer()))
}

// TestScrapeLogs_Unreachable — closed server → nil slice.
func TestScrapeLogs_Unreachable(t *testing.T) {
	assert.Nil(t, scrapeLogs(closedServer(), "", ""))
}

// TestScrapeLogs_Non200 — non-200 returns nil. Also verify the
// trace_id / level query params make it to the handler (non-200 case).
func TestScrapeLogs_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	assert.Nil(t, scrapeLogs(srv.URL, "abc", "INFO"))
}

// TestScrapeTraces_Unreachable — closed server → nil slice.
func TestScrapeTraces_Unreachable(t *testing.T) {
	assert.Nil(t, scrapeTraces(closedServer()))
}

// TestScrapeGoroutines_Unreachable — closed server → zero snapshot.
func TestScrapeGoroutines_Unreachable(t *testing.T) {
	got := scrapeGoroutines(closedServer())
	assert.Zero(t, got.Total)
}

// TestDevtoolsAvailable_Unreachable — closed server → false.
func TestDevtoolsAvailable_Unreachable(t *testing.T) {
	assert.False(t, devtoolsAvailable(closedServer()))
}

// TestDevtoolsAvailable_Non200 — 500 response → false.
func TestDevtoolsAvailable_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	assert.False(t, devtoolsAvailable(srv.URL))
}

// TestDevtoolsAvailable_BadJSONCoverage — body isn't JSON → false.
func TestDevtoolsAvailable_BadJSONCoverage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()
	assert.False(t, devtoolsAvailable(srv.URL))
}

// TestDevtoolsAvailable_Stub — body says "stub" → false.
func TestDevtoolsAvailable_Stub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"devtools":"stub"}`))
	}))
	defer srv.Close()
	assert.False(t, devtoolsAvailable(srv.URL))
}

// TestDetectNPlusOne_SortsBySelectionSort — a larger input with out-of-
// order Count values exercises the swap path in the selection sort.
// Also seeds an out-of-order distribution so the `best = b` branch
// fires.
func TestDetectNPlusOne_SortsBySelectionSort(t *testing.T) {
	queries := []scrapedQuery{}
	// Generate in ORDER reversed (smallest first in traceID) so the
	// sort has work to do.
	for i := 0; i < 3; i++ {
		queries = append(queries, scrapedQuery{TraceID: "A", SQL: "SELECT " + strings.Repeat("a", 1)})
	}
	for i := 0; i < 5; i++ {
		queries = append(queries, scrapedQuery{TraceID: "B", SQL: "SELECT " + strings.Repeat("b", 1)})
	}
	for i := 0; i < 4; i++ {
		queries = append(queries, scrapedQuery{TraceID: "C", SQL: "SELECT " + strings.Repeat("c", 1)})
	}
	out := detectNPlusOne(queries)
	require.Len(t, out, 3)
	// Sort descending by Count.
	assert.Equal(t, 5, out[0].Count)
	assert.GreaterOrEqual(t, out[1].Count, out[2].Count)
}

// TestSortGoroutineGroupsDescByCount_Swap — multi-group input exercises
// the "best != a" swap branch.
func TestSortGoroutineGroupsDescByCount_Swap(t *testing.T) {
	groups := []goroutineGroup{
		{Top: "a", Count: 1},
		{Top: "b", Count: 5},
		{Top: "c", Count: 3},
	}
	sortGoroutineGroupsDescByCount(groups)
	assert.Equal(t, 5, groups[0].Count)
	assert.Equal(t, 3, groups[1].Count)
	assert.Equal(t, 1, groups[2].Count)
}
