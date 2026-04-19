package commands

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────
// External scrapers — no code runs inside the scaffolded project. Each
// function below talks to the running app over HTTP and normalizes the
// response into a shape the dashboard can render. Failures are soft:
// an unreachable endpoint, a parse error, or a 404 all resolve to an
// empty result so the dashboard degrades gracefully.
// ─────────────────────────────────────────────────────────────────────

// scrapeClient is a short-timeout HTTP client reused across scrapers.
// Dashboards that have many panels open shouldn't be able to stall the
// refresher loop for more than a second or two.
var scrapeClient = &http.Client{Timeout: 2 * time.Second}

// metricsSnapshot captures the Prometheus counters the dashboard renders
// inline. Only the fields we can reliably extract from the default
// pkg/observability output are surfaced — more fields can be added here
// as the underlying metrics expand.
type metricsSnapshot struct {
	RequestsTotal int64    `json:"requests_total"`
	InFlight      int64    `json:"in_flight"`
	LatencyP50MS  *float64 `json:"latency_p50_ms,omitempty"`
	LatencyP95MS  *float64 `json:"latency_p95_ms,omitempty"`
	MetricsOK     bool     `json:"metrics_ok"`
}

// scrapeMetrics fetches the app's /metrics endpoint and parses the
// Prometheus text format for the specific counters the dashboard cares
// about. We don't pull in a Prometheus parser — the text format is
// simple enough and we only need a handful of metric families.
func scrapeMetrics(appURL string) metricsSnapshot {
	result := metricsSnapshot{}

	resp, err := scrapeClient.Get(appURL + "/metrics")
	if err != nil {
		return result
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result
	}

	result.MetricsOK = true
	// Read the body in one gulp — /metrics responses are tiny (< 32KB
	// for a typical gofasta project).
	buf := make([]byte, 64*1024)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	// Sum `http_requests_total{...}` across all label sets.
	result.RequestsTotal = sumCounterFamily(body, "http_requests_total")
	result.InFlight = sumCounterFamily(body, "http_in_flight_requests")

	// Approximate p50/p95 from the _sum and _count (mean) — better than
	// nothing until we parse histogram buckets.
	p50 := approxLatencyMS(body, "http_request_duration_seconds")
	if p50 > 0 {
		result.LatencyP50MS = &p50
	}

	return result
}

// sumCounterFamily returns the sum of every sample in body matching a
// counter or gauge family by name. Labels are ignored — we reduce to a
// single scalar per family for the dashboard display. Returns 0 when
// the family isn't found or when parsing fails.
func sumCounterFamily(body, name string) int64 {
	var total int64
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Accept `name VALUE` or `name{labels...} VALUE`.
		if !strings.HasPrefix(line, name) {
			continue
		}
		rest := strings.TrimPrefix(line, name)
		if rest != "" && rest[0] != ' ' && rest[0] != '{' {
			// `http_requests_total_something` — different family.
			continue
		}
		// Last space-separated token is the value.
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil {
			continue
		}
		total += int64(v)
	}
	return total
}

// approxLatencyMS returns the mean of a histogram family — _sum / _count
// — converted from seconds to milliseconds. It's a mean, not a p50; the
// dashboard labels this as "avg" to avoid misleading the user.
func approxLatencyMS(body, name string) float64 {
	var sum, count float64
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(line, name+"_sum "), strings.HasPrefix(line, name+"_sum{"):
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if v, err := strconv.ParseFloat(fields[len(fields)-1], 64); err == nil {
					sum += v
				}
			}
		case strings.HasPrefix(line, name+"_count "), strings.HasPrefix(line, name+"_count{"):
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if v, err := strconv.ParseFloat(fields[len(fields)-1], 64); err == nil {
					count += v
				}
			}
		}
	}
	if count == 0 {
		return 0
	}
	return (sum / count) * 1000.0
}

// scrapeRequestLog hits /debug/requests. Returns nil when the app is
// running without the `devtools` build tag (debug endpoints return 404).
func scrapeRequestLog(appURL string) []scrapedRequest {
	resp, err := scrapeClient.Get(appURL + "/debug/requests")
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var entries []scrapedRequest
	_ = json.NewDecoder(resp.Body).Decode(&entries)
	return entries
}

// scrapeSQLLog hits /debug/sql.
func scrapeSQLLog(appURL string) []scrapedQuery {
	resp, err := scrapeClient.Get(appURL + "/debug/sql")
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var entries []scrapedQuery
	_ = json.NewDecoder(resp.Body).Decode(&entries)
	return entries
}

// scrapedRequest mirrors the scaffold's devtools.RequestEntry shape.
// Duplicated here rather than imported so the CLI doesn't depend on the
// scaffold (which isn't even a Go package from the CLI's perspective).
type scrapedRequest struct {
	Time       time.Time `json:"time"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Status     int       `json:"status"`
	DurationMS int64     `json:"duration_ms"`
	RemoteAddr string    `json:"remote_addr,omitempty"`
	TraceID    string    `json:"trace_id,omitempty"`
	Body       string    `json:"body,omitempty"`
}

// scrapedQuery mirrors devtools.QueryEntry from the scaffold.
type scrapedQuery struct {
	Time       time.Time `json:"time"`
	SQL        string    `json:"sql"`
	Rows       int64     `json:"rows"`
	DurationMS int64     `json:"duration_ms"`
	Error      string    `json:"error,omitempty"`
}

// scrapedTrace mirrors devtools.TraceEntry. Spans are omitted from
// summary list responses and populated only when the dashboard fetches
// a single trace by ID.
type scrapedTrace struct {
	TraceID    string        `json:"trace_id"`
	RootName   string        `json:"root_name"`
	Time       time.Time     `json:"time"`
	DurationMS int64         `json:"duration_ms"`
	Status     string        `json:"status"`
	SpanCount  int           `json:"span_count"`
	Spans      []scrapedSpan `json:"spans,omitempty"`
}

// scrapedSpan mirrors devtools.TraceSpan.
type scrapedSpan struct {
	SpanID     string            `json:"span_id"`
	ParentID   string            `json:"parent_id,omitempty"`
	Name       string            `json:"name"`
	Kind       string            `json:"kind,omitempty"`
	OffsetMS   int64             `json:"offset_ms"`
	DurationMS int64             `json:"duration_ms"`
	Status     string            `json:"status,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Events     []scrapedEvent    `json:"events,omitempty"`
	Stack      []string          `json:"stack,omitempty"`
}

// scrapedEvent mirrors devtools.TraceEvent.
type scrapedEvent struct {
	Name       string            `json:"name"`
	OffsetMS   int64             `json:"offset_ms"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// scrapeTraces fetches summary list of recent traces. Spans are
// stripped server-side so this stays cheap to poll (5s cadence).
func scrapeTraces(appURL string) []scrapedTrace {
	resp, err := scrapeClient.Get(appURL + "/debug/traces")
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var entries []scrapedTrace
	_ = json.NewDecoder(resp.Body).Decode(&entries)
	return entries
}

// scrapeTraceDetail fetches one full trace including every span and
// stack. Returns (nil, false) when the trace is missing or the app
// isn't reachable.
func scrapeTraceDetail(appURL, id string) (*scrapedTrace, bool) {
	resp, err := scrapeClient.Get(appURL + "/debug/traces/" + id)
	if err != nil {
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var entry scrapedTrace
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, false
	}
	return &entry, true
}

// devtoolsAvailable reports whether the running app was built with the
// `devtools` tag. /debug/health returns {"devtools":"enabled"} in that
// case and {"devtools":"stub"} otherwise.
func devtoolsAvailable(appURL string) bool {
	resp, err := scrapeClient.Get(appURL + "/debug/health")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var payload struct {
		Devtools string `json:"devtools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false
	}
	return payload.Devtools == "enabled"
}
