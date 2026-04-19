package commands

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SQL normalization regexps for N+1 detection. Compiled once at init
// so detection stays cheap in the refresher loop.
var (
	// Single- and double-quoted string literals. Uses non-greedy
	// matching so a malformed SQL with unbalanced quotes doesn't eat
	// the rest of the input.
	reSQLStringLit = regexp.MustCompile(`'[^']*'|"[^"]*"`)
	// Whole-word numeric literals so `id` and `15` differ but
	// `WHERE x = 42` normalizes to `WHERE x = ?`.
	reSQLNumberLit = regexp.MustCompile(`\b\d+(\.\d+)?\b`)
	// Runs of whitespace (including newlines) collapse to a single space.
	reSQLWhitespace = regexp.MustCompile(`\s+`)
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
	Time                time.Time `json:"time"`
	Method              string    `json:"method"`
	Path                string    `json:"path"`
	Status              int       `json:"status"`
	DurationMS          int64     `json:"duration_ms"`
	RemoteAddr          string    `json:"remote_addr,omitempty"`
	TraceID             string    `json:"trace_id,omitempty"`
	Body                string    `json:"body,omitempty"`
	ResponseBody        string    `json:"response_body,omitempty"`
	ResponseContentType string    `json:"response_content_type,omitempty"`
}

// scrapedQuery mirrors devtools.QueryEntry from the scaffold.
type scrapedQuery struct {
	Time       time.Time `json:"time"`
	SQL        string    `json:"sql"`
	Rows       int64     `json:"rows"`
	DurationMS int64     `json:"duration_ms"`
	Error      string    `json:"error,omitempty"`
	TraceID    string    `json:"trace_id,omitempty"`
	Vars       []string  `json:"vars,omitempty"`
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

// scrapedLog mirrors devtools.LogEntry — one slog record.
type scrapedLog struct {
	Time    time.Time         `json:"time"`
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Attrs   map[string]string `json:"attrs,omitempty"`
	TraceID string            `json:"trace_id,omitempty"`
}

// scrapedCache mirrors devtools.CacheEntry — one cache op.
type scrapedCache struct {
	Time       time.Time `json:"time"`
	Op         string    `json:"op"`
	Key        string    `json:"key,omitempty"`
	Hit        bool      `json:"hit,omitempty"`
	DurationMS int64     `json:"duration_ms"`
	Error      string    `json:"error,omitempty"`
	TraceID    string    `json:"trace_id,omitempty"`
}

// scrapeCacheOps fetches /debug/cache. Empty ring → nil.
func scrapeCacheOps(appURL string) []scrapedCache {
	resp, err := scrapeClient.Get(appURL + "/debug/cache")
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var entries []scrapedCache
	_ = json.NewDecoder(resp.Body).Decode(&entries)
	return entries
}

// scrapedException mirrors devtools.ExceptionEntry.
type scrapedException struct {
	Time      time.Time `json:"time"`
	Path      string    `json:"path,omitempty"`
	Method    string    `json:"method,omitempty"`
	Status    int       `json:"status,omitempty"`
	Recovered string    `json:"recovered"`
	Stack     []string  `json:"stack,omitempty"`
	TraceID   string    `json:"trace_id,omitempty"`
}

// scrapeExceptions fetches the recent-exceptions ring.
func scrapeExceptions(appURL string) []scrapedException {
	resp, err := scrapeClient.Get(appURL + "/debug/errors")
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var entries []scrapedException
	_ = json.NewDecoder(resp.Body).Decode(&entries)
	return entries
}

// scrapeLogs fetches the devtools log ring, optionally filtered by
// trace ID and/or minimum level. Empty filters mean "no filter on
// that dimension" — the app's /debug/logs handler applies the same
// semantics.
func scrapeLogs(appURL, traceID, level string) []scrapedLog {
	u := appURL + "/debug/logs"
	qs := url.Values{}
	if traceID != "" {
		qs.Set("trace_id", traceID)
	}
	if level != "" {
		qs.Set("level", level)
	}
	if enc := qs.Encode(); enc != "" {
		u += "?" + enc
	}
	resp, err := scrapeClient.Get(u)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var entries []scrapedLog
	_ = json.NewDecoder(resp.Body).Decode(&entries)
	return entries
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

// ── N+1 detection ─────────────────────────────────────────────────────
//
// n+1 is the classic "I loaded 50 users, then ran one SELECT per user
// to fetch their permissions" problem. The detector groups queries by
// trace ID and normalized SQL template (literal values replaced with
// placeholders) and flags any (trace, template) pair with ≥
// nPlusOneThreshold hits. The threshold defaults to 3: two repeated
// queries are probably intentional (a lookup + a count), three is
// usually a smell.

const nPlusOneThreshold = 3

// nPlusOneFinding is one detected N+1 pattern. TraceID points back to
// the offending request; Template is the normalized SQL (e.g.
// "SELECT * FROM users WHERE id = ?"); Count is how many times it
// fired inside the trace.
type nPlusOneFinding struct {
	TraceID  string `json:"trace_id"`
	Template string `json:"template"`
	Count    int    `json:"count"`
}

// detectNPlusOne walks the query ring and returns any (trace,
// template) pair with ≥ nPlusOneThreshold hits. Pure function; no I/O,
// so unit-testable without a running app.
func detectNPlusOne(queries []scrapedQuery) []nPlusOneFinding {
	// trace_id → template → count.
	buckets := make(map[string]map[string]int)
	for _, q := range queries {
		if q.TraceID == "" || q.SQL == "" {
			continue
		}
		tpl := normalizeSQL(q.SQL)
		inner, ok := buckets[q.TraceID]
		if !ok {
			inner = make(map[string]int)
			buckets[q.TraceID] = inner
		}
		inner[tpl]++
	}
	var out []nPlusOneFinding
	for tid, perTpl := range buckets {
		for tpl, count := range perTpl {
			if count >= nPlusOneThreshold {
				out = append(out, nPlusOneFinding{
					TraceID:  tid,
					Template: tpl,
					Count:    count,
				})
			}
		}
	}
	// Sort by count desc so the worst offenders render first.
	for a := 0; a < len(out); a++ {
		best := a
		for b := a + 1; b < len(out); b++ {
			if out[b].Count > out[best].Count {
				best = b
			}
		}
		if best != a {
			out[a], out[best] = out[best], out[a]
		}
	}
	return out
}

// normalizeSQL collapses string / number literals and whitespace so
// two queries that differ only in their parameters produce the same
// template. This is intentionally simple — it catches the 90% case
// (same table, same WHERE columns, varying values) without a full SQL
// parser. False positives (two differently-shaped queries that
// happen to normalize to the same string) are rare and harmless: at
// worst the dashboard misattributes a finding.
func normalizeSQL(sql string) string {
	s := sql
	// Replace quoted strings with a sentinel. Handle both single and
	// double quotes. Non-greedy match keeps us from swallowing an
	// entire SQL statement on a malformed literal.
	s = reSQLStringLit.ReplaceAllString(s, "?")
	// Replace integer / float literals with the same sentinel so
	// numeric-only queries group with their string-literal siblings.
	s = reSQLNumberLit.ReplaceAllString(s, "?")
	// Collapse runs of whitespace so reformatted queries match.
	s = reSQLWhitespace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// goroutineGroup is one bucket of goroutines sharing the same
// top-of-stack function. The dashboard renders total count per group
// so a developer can spot (e.g.) "18 goroutines parked in net/http
// waiting for accept" at a glance, then expand for the full stacks.
type goroutineGroup struct {
	Top    string   `json:"top"`
	Count  int      `json:"count"`
	States []string `json:"states,omitempty"`
}

// goroutineSnapshot is a shallow summary of the app's goroutine
// population. Total is the absolute count; Groups is a sorted (desc
// by count) slice of aggregates. Zero-valued when /debug/pprof is
// unavailable so the dashboard quietly renders "0 goroutines".
type goroutineSnapshot struct {
	Total  int              `json:"total"`
	Groups []goroutineGroup `json:"groups,omitempty"`
}

// scrapeGoroutines fetches /debug/pprof/goroutine?debug=2, parses the
// text dump, and aggregates by top-of-stack function name. This
// reuses the pprof endpoint rather than adding a second goroutine
// dump surface.
func scrapeGoroutines(appURL string) goroutineSnapshot {
	var snap goroutineSnapshot
	resp, err := scrapeClient.Get(appURL + "/debug/pprof/goroutine?debug=2")
	if err != nil {
		return snap
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return snap
	}
	buf := make([]byte, 1<<20) // 1 MiB ceiling on a dev-time dump
	n, _ := resp.Body.Read(buf)
	return parseGoroutineDump(string(buf[:n]))
}

// parseGoroutineDump walks the debug=2 text format. Each goroutine
// block starts with `goroutine N [state]:` and the first function
// line after that header is the top-of-stack. We aggregate by that
// function and also collect the distinct state strings we saw for
// each bucket.
func parseGoroutineDump(text string) goroutineSnapshot {
	var snap goroutineSnapshot
	if text == "" {
		return snap
	}
	lines := strings.Split(text, "\n")
	groups := make(map[string]*goroutineGroup)
	for i := 0; i < len(lines); {
		line := lines[i]
		if !strings.HasPrefix(line, "goroutine ") {
			i++
			continue
		}
		snap.Total++
		recordGoroutineEntry(groups, goroutineStateOf(line), firstTopOfStack(lines, i+1))
		i = advancePastGoroutineBlock(lines, i+1)
	}
	snap.Groups = make([]goroutineGroup, 0, len(groups))
	for _, g := range groups {
		snap.Groups = append(snap.Groups, *g)
	}
	sortGoroutineGroupsDescByCount(snap.Groups)
	return snap
}

// goroutineStateOf returns whatever's between [ and ] on a goroutine
// header line. Empty string if the header is malformed.
func goroutineStateOf(header string) string {
	lb := strings.Index(header, "[")
	if lb < 0 {
		return ""
	}
	rb := strings.Index(header[lb:], "]")
	if rb <= 0 {
		return ""
	}
	return header[lb+1 : lb+rb]
}

// firstTopOfStack returns the first non-blank line starting at `from`,
// with the argument list stripped. Method receivers look like
// `pkg.(*Type).method(args)` — the first `(` belongs to the type, so
// we strip from the LAST `(` instead to preserve the method name.
func firstTopOfStack(lines []string, from int) string {
	top := ""
	for j := from; j < len(lines); j++ {
		cand := strings.TrimSpace(lines[j])
		if cand == "" {
			continue
		}
		top = cand
		break
	}
	if paren := strings.LastIndex(top, "("); paren > 0 {
		top = top[:paren]
	}
	if top == "" {
		return "<unknown>"
	}
	return top
}

// advancePastGoroutineBlock walks forward until the next `goroutine `
// header (or EOF), returning the index to resume parsing from.
func advancePastGoroutineBlock(lines []string, from int) int {
	for from < len(lines) && !strings.HasPrefix(lines[from], "goroutine ") {
		from++
	}
	return from
}

// recordGoroutineEntry increments the count for (top) and unions the
// state into the group's distinct-states list.
func recordGoroutineEntry(groups map[string]*goroutineGroup, state, top string) {
	g, ok := groups[top]
	if !ok {
		g = &goroutineGroup{Top: top}
		groups[top] = g
	}
	g.Count++
	if state == "" {
		return
	}
	for _, s := range g.States {
		if s == state {
			return
		}
	}
	g.States = append(g.States, state)
}

// sortGoroutineGroupsDescByCount orders in-place by Count descending so
// the dashboard's first row is the biggest bucket. Uses a selection
// sort to avoid pulling in the sort package for a tiny slice.
func sortGoroutineGroupsDescByCount(groups []goroutineGroup) {
	for a := 0; a < len(groups); a++ {
		best := a
		for b := a + 1; b < len(groups); b++ {
			if groups[b].Count > groups[best].Count {
				best = b
			}
		}
		if best != a {
			groups[a], groups[best] = groups[best], groups[a]
		}
	}
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
