package commands

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// dashboardTemplateSource is the full HTML template served at /. Lives
// in a sibling .html file so editors treat it as HTML (syntax
// highlighting, linting) and so the Go source of this file stays free
// of inline markup. Parsed once lazily via html/template — auto-
// escaping protects any server-rendered string that lands in the DOM.
//
//go:embed dev_dashboard.html
var dashboardTemplateSource string

// dashboardTemplate is the parsed template. Resolved lazily on first
// request so a malformed template surfaces as a 500 at runtime rather
// than blowing up package init.
var (
	dashboardTemplate     *template.Template
	dashboardTemplateOnce sync.Once
	dashboardTemplateErr  error
)

// loadDashboardTemplate parses the embedded HTML template once and
// caches the result. Subsequent calls are lock-free reads of the
// package-level pointer.
func loadDashboardTemplate() (*template.Template, error) {
	dashboardTemplateOnce.Do(func() {
		dashboardTemplate, dashboardTemplateErr = template.
			New("dashboard").
			Parse(dashboardTemplateSource)
	})
	return dashboardTemplate, dashboardTemplateErr
}

// ─────────────────────────────────────────────────────────────────────
// Dev dashboard — Phase 6 of the gofasta dev enhancement.
//
// When --dashboard is set, gofasta dev runs a tiny HTTP server on a
// separate debug port (default 9090) that serves:
//
//   GET /                → HTML page with live sections for routes,
//                          services, and the app health check
//   GET /api/state       → JSON snapshot of the current dashboard state
//   GET /api/stream      → SSE stream of state updates every 5s
//
// The dashboard is intentionally lightweight:
//
//   - No external deps beyond the stdlib (net/http, encoding/json)
//   - No runtime coupling to the app itself; polls the app's own
//     /health and /metrics endpoints instead of embedding in it
//   - Dies cleanly when gofasta dev exits — uses context cancellation
//     on the http.Server so Ctrl+C tears it down with the rest of the
//     pipeline
// ─────────────────────────────────────────────────────────────────────

// dashboardState is the JSON payload served by /api/state. Embedded in
// the HTML page for first paint and refreshed via SSE every 5s.
type dashboardState struct {
	AppPort         int                `json:"app_port"`
	AppURL          string             `json:"app_url"`
	Health          string             `json:"health"` // "ok" | "unreachable" | "unhealthy"
	Services        []serviceState     `json:"services"`
	Routes          []dashboardRoute   `json:"routes"`
	SwaggerURL      string             `json:"swagger_url,omitempty"`
	GraphQLURL      string             `json:"graphql_url,omitempty"`
	MetricsURL      string             `json:"metrics_url,omitempty"`
	Metrics         metricsSnapshot    `json:"metrics"`
	DevtoolsEnabled bool               `json:"devtools_enabled"`
	PprofURL        string             `json:"pprof_url,omitempty"`
	AsynqmonURL     string             `json:"asynqmon_url,omitempty"`
	Goroutines      goroutineSnapshot  `json:"goroutines"`
	RecentRequests  []scrapedRequest   `json:"recent_requests,omitempty"`
	RecentQueries   []scrapedQuery     `json:"recent_queries,omitempty"`
	RecentTraces    []scrapedTrace     `json:"recent_traces,omitempty"`
	NPlusOne        []nPlusOneFinding  `json:"n_plus_one,omitempty"`
	Exceptions      []scrapedException `json:"exceptions,omitempty"`
	CacheOps        []scrapedCache     `json:"cache_ops,omitempty"`
	LastUpdatedMS   int64              `json:"last_updated_ms"`
}

// dashboardRoute is a single REST route scraped from the scaffold's
// docs/swagger.json. Carries the request body type and the primary
// 2xx response type so the dashboard can show developers *what shape
// the endpoint expects and returns* without bouncing them to the
// Swagger UI. Fields are optional — older swagger docs or handwritten
// operations without schemas still produce a valid (method, path)
// row.
type dashboardRoute struct {
	Method   string `json:"method"`
	Path     string `json:"path"`
	Summary  string `json:"summary,omitempty"`
	Request  string `json:"request,omitempty"`
	Response string `json:"response,omitempty"`
}

// schemaRef represents the slice of an OpenAPI/Swagger schema object
// the dashboard needs to extract a readable type name. Handles the
// three shapes swag commonly emits: a bare $ref, an array whose
// items carry the ref, and a primitive scalar (type=string etc).
type schemaRef struct {
	Ref   string     `json:"$ref,omitempty"`
	Type  string     `json:"type,omitempty"`
	Items *schemaRef `json:"items,omitempty"`
}

// operationSpec is the subset of an OpenAPI operation object the
// dashboard route extractor inspects. Supports OpenAPI 2.0 (swag
// default) via `parameters[].in=body` and OpenAPI 3.0 via
// `requestBody.content['application/json'].schema` so hand-authored
// specs work too.
type operationSpec struct {
	Summary     string                  `json:"summary"`
	Parameters  []parameterSpec         `json:"parameters"`
	Responses   map[string]responseSpec `json:"responses"`
	RequestBody *requestBodySpec        `json:"requestBody,omitempty"`
}

type parameterSpec struct {
	In     string     `json:"in"`
	Schema *schemaRef `json:"schema,omitempty"`
}

type responseSpec struct {
	Schema *schemaRef `json:"schema,omitempty"`
	// OpenAPI 3.0 fallback.
	Content map[string]struct {
		Schema *schemaRef `json:"schema"`
	} `json:"content,omitempty"`
}

type requestBodySpec struct {
	Content map[string]struct {
		Schema *schemaRef `json:"schema"`
	} `json:"content"`
}

// dashboardServer owns the HTTP server and the cached state. Reads
// and writes to the state are guarded by a single mutex; subscribers
// receive fresh state via an in-memory pub/sub.
type dashboardServer struct {
	port      int
	appPort   int
	appURL    string
	svc       *devServices
	mu        sync.RWMutex
	state     dashboardState
	httpSrv   *http.Server
	listeners sync.Map // map[chan dashboardState]struct{}
}

// startDashboard spins up the dashboard HTTP server in the background
// and starts the periodic refresher goroutine that polls service state
// and app health. Returns a shutdown func wired to the pipeline's
// teardown path.
func startDashboard(port, appPort int, svc *devServices, emitter devEmitter) func() {
	appURL := "http://localhost:" + fmt.Sprintf("%d", appPort)
	srv := &dashboardServer{
		port:    port,
		appPort: appPort,
		appURL:  appURL,
		svc:     svc,
		state: dashboardState{
			AppPort:       appPort,
			AppURL:        appURL,
			Health:        "unknown",
			MetricsURL:    appURL + "/metrics",
			LastUpdatedMS: time.Now().UnixMilli(),
		},
	}

	// Optional endpoints — surfaced in the state if the scaffold
	// publishes them. Detected once at startup to keep the refresher
	// cheap; filesystem markers shouldn't change during a dev session.
	if _, err := os.Stat("docs/swagger.json"); err == nil {
		srv.state.SwaggerURL = appURL + "/swagger/index.html"
	}
	if _, err := os.Stat("gqlgen.yml"); err == nil {
		srv.state.GraphQLURL = appURL + "/graphql"
	}
	srv.state.Routes = readRouteEntries()

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleIndex)
	mux.HandleFunc("/api/state", srv.handleState)
	mux.HandleFunc("/api/stream", srv.handleStream)
	mux.HandleFunc("/api/trace/", srv.handleTraceDetail)
	mux.HandleFunc("/api/logs", srv.handleLogs)
	mux.HandleFunc("/api/replay", srv.handleReplay)
	mux.HandleFunc("/api/explain", srv.handleExplain)
	mux.HandleFunc("/api/har", srv.handleHAR)

	srv.httpSrv = &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := srv.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			emitter.Warn(fmt.Sprintf("dashboard server exited: %v", err))
		}
	}()
	go srv.refresherLoop(ctx)

	emitter.Info(fmt.Sprintf("dashboard running on http://localhost:%d", port))

	return func() {
		cancel()
		// 1-second grace period for in-flight requests (including any
		// open SSE streams we'd like to close politely).
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
		defer cancelShutdown()
		_ = srv.httpSrv.Shutdown(shutdownCtx)
	}
}

// refresherLoop updates the dashboard state every 5s. Sends a
// notification to every SSE subscriber on each refresh so browsers
// don't have to poll the snapshot endpoint.
func (s *dashboardServer) refresherLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	// Run one refresh immediately so the first SSE tick isn't delayed.
	s.refresh()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refresh()
		}
	}
}

// refresh rebuilds the dashboard state snapshot — polls app health,
// queries compose service states, stamps the update time, and
// broadcasts to subscribers.
func (s *dashboardServer) refresh() {
	health := probeHealth(s.appURL + "/health")
	states, asynqmonURL := s.resolveServices()
	metrics := scrapeMetrics(s.appURL)
	devtoolsOn := devtoolsAvailable(s.appURL)
	dt := s.scrapeDevtools(devtoolsOn)

	s.mu.Lock()
	s.state.Health = health
	s.state.Services = states
	s.state.Metrics = metrics
	s.state.DevtoolsEnabled = devtoolsOn
	if devtoolsOn {
		s.state.PprofURL = s.appURL + "/debug/pprof/"
	} else {
		s.state.PprofURL = ""
	}
	s.state.Goroutines = dt.goroutines
	s.state.AsynqmonURL = asynqmonURL
	s.state.NPlusOne = detectNPlusOne(dt.queries)
	s.state.Exceptions = dt.exceptions
	s.state.CacheOps = dt.cacheOps
	s.state.RecentRequests = dt.requests
	s.state.RecentQueries = dt.queries
	s.state.RecentTraces = dt.traces
	s.state.LastUpdatedMS = time.Now().UnixMilli()
	snapshot := s.state
	s.mu.Unlock()

	s.listeners.Range(func(key, _ any) bool {
		ch := key.(chan dashboardState)
		select {
		case ch <- snapshot:
		default:
			// subscriber is slow — drop the update rather than block
			// the refresher goroutine
		}
		return true
	})
}

// devtoolsScrape bundles everything we pull from the app's
// /debug/* endpoints in one pass. Assembled by scrapeDevtools and
// applied to state under the server's lock.
type devtoolsScrape struct {
	requests   []scrapedRequest
	queries    []scrapedQuery
	traces     []scrapedTrace
	exceptions []scrapedException
	cacheOps   []scrapedCache
	goroutines goroutineSnapshot
}

// scrapeDevtools fans out to each /debug/* endpoint when the target
// app exposes them. Each call fails soft so a single-surface outage
// never blanks the whole dashboard. Returns an empty struct when the
// app was built without the devtools tag.
func (s *dashboardServer) scrapeDevtools(devtoolsOn bool) devtoolsScrape {
	if !devtoolsOn {
		return devtoolsScrape{}
	}
	return devtoolsScrape{
		requests:   scrapeRequestLog(s.appURL),
		queries:    scrapeSQLLog(s.appURL),
		traces:     scrapeTraces(s.appURL),
		exceptions: scrapeExceptions(s.appURL),
		cacheOps:   scrapeCacheOps(s.appURL),
		goroutines: scrapeGoroutines(s.appURL),
	}
}

// resolveServices reconciles compose state with our selected service
// list. Returns the filtered service states plus a non-empty
// asynqmonURL when a `queue`-named service is running.
func (s *dashboardServer) resolveServices() (states []serviceState, asynqmonURL string) {
	if s.svc == nil || len(s.svc.selected) == 0 {
		return nil, ""
	}
	live, err := queryServiceStates()
	if err != nil {
		return nil, ""
	}
	for _, st := range live {
		for _, sel := range s.svc.selected {
			if st.Name == sel {
				states = append(states, st)
			}
		}
		if u := asynqmonURLFor(st); u != "" {
			asynqmonURL = u
		}
	}
	return states, asynqmonURL
}

// asynqmonURLFor returns the dashboard URL for a healthy `queue`-named
// compose service, or "" if the service isn't an asynqmon match. The
// scaffold's compose.yaml names this service `queue` and exposes it
// on ${ASYNQMON_HOST_PORT:-8081}.
func asynqmonURLFor(st serviceState) string {
	if !strings.Contains(strings.ToLower(st.Name), "queue") {
		return ""
	}
	if !strings.EqualFold(st.Health, "healthy") && !strings.EqualFold(st.State, "running") {
		return ""
	}
	port := os.Getenv("ASYNQMON_HOST_PORT")
	if port == "" {
		port = "8081"
	}
	return "http://localhost:" + port
}

// probeHealth does a 2-second-timeout GET against the app's /health
// endpoint. Doesn't care about the body — any 2xx counts as healthy.
func probeHealth(healthURL string) string {
	client := &http.Client{Timeout: 2 * time.Second}
	//nolint:noctx // Short-lived single-purpose client; no context threading needed.
	resp, err := client.Get(healthURL)
	if err != nil {
		return "unreachable"
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "ok"
	}
	return "unhealthy"
}

// readRouteEntries opens `docs/swagger.json` and extracts route
// metadata: method, path, optional operation summary, request body
// type, and primary 2xx response type. The scaffold regenerates
// swagger.json on build so this is usually fresh. Missing file
// (GraphQL-only projects, pre-first-build) → nil → empty routes
// table in the dashboard; never blocks the pipeline.
func readRouteEntries() []dashboardRoute {
	path := filepath.Join("docs", "swagger.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Paths map[string]map[string]operationSpec `json:"paths"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	entries := make([]dashboardRoute, 0, len(doc.Paths))
	for routePath, methods := range doc.Paths {
		for method, op := range methods {
			entries = append(entries, dashboardRoute{
				Method:   strings.ToUpper(method),
				Path:     routePath,
				Summary:  op.Summary,
				Request:  extractRequestType(op),
				Response: extractResponseType(op.Responses),
			})
		}
	}
	return entries
}

// extractRequestType returns a readable name for the request body
// type, handling both OpenAPI 2.0 (parameters[in=body].schema) and
// OpenAPI 3.0 (requestBody.content[application/json].schema). Returns
// "" when the operation has no request body.
func extractRequestType(op operationSpec) string {
	// OpenAPI 2.0 path — swag's default output.
	for _, p := range op.Parameters {
		if p.In == "body" {
			return typeNameFromSchema(p.Schema)
		}
	}
	// OpenAPI 3.0 fallback — hand-written specs or future swag versions.
	if op.RequestBody != nil {
		if body, ok := op.RequestBody.Content["application/json"]; ok {
			return typeNameFromSchema(body.Schema)
		}
	}
	return ""
}

// extractResponseType picks the most meaningful response to display.
// Prefers the lowest-numbered 2xx (200, 201, 202 …); falls back to
// the lowest-numbered response if no 2xx exists. Lexicographic code
// ordering is fine here — three-digit status codes sort numerically.
func extractResponseType(responses map[string]responseSpec) string {
	if len(responses) == 0 {
		return ""
	}
	best := pickPrimaryResponseCode(responses)
	if best == "" {
		return ""
	}
	r := responses[best]
	// OpenAPI 2.0 puts the schema at the response root; 3.0 puts it in
	// content["application/json"].schema. Try both.
	if r.Schema != nil {
		return typeNameFromSchema(r.Schema)
	}
	if body, ok := r.Content["application/json"]; ok {
		return typeNameFromSchema(body.Schema)
	}
	return ""
}

// pickPrimaryResponseCode returns the lowest 2xx status code present in
// the responses map, or the lowest response code of any tier if no 2xx
// exists. Used to decide which response's schema to surface on the
// dashboard.
func pickPrimaryResponseCode(responses map[string]responseSpec) string {
	var best2xx, bestAny string
	for code := range responses {
		if code == "" {
			continue
		}
		if bestAny == "" || code < bestAny {
			bestAny = code
		}
		if len(code) == 3 && code[0] == '2' {
			if best2xx == "" || code < best2xx {
				best2xx = code
			}
		}
	}
	if best2xx != "" {
		return best2xx
	}
	return bestAny
}

// typeNameFromSchema turns a Swagger/OpenAPI schema object into a
// developer-readable Go-ish type name:
//
//	{$ref: "#/definitions/User"}          → "User"
//	{type: "array", items: {$ref: "..."}} → "[]User"
//	{type: "string"}                      → "string"
//
// Returns "" when the schema is nil or too opaque to describe in a
// single token (anyOf / oneOf / free-form objects etc).
func typeNameFromSchema(s *schemaRef) string {
	if s == nil {
		return ""
	}
	if s.Ref != "" {
		// "#/definitions/User" or "#/components/schemas/User" → "User"
		if i := strings.LastIndex(s.Ref, "/"); i >= 0 {
			return s.Ref[i+1:]
		}
		return s.Ref
	}
	if s.Type == "array" && s.Items != nil {
		if inner := typeNameFromSchema(s.Items); inner != "" {
			return "[]" + inner
		}
	}
	if s.Type != "" {
		return s.Type
	}
	return ""
}

// handleIndex renders the dashboard HTML with the current state as the
// template context. Server-side rendering means first paint shows live
// data immediately (no "loading" flash before the SSE stream connects).
// html/template auto-escapes every interpolated string, so untrusted
// values (route paths scraped from swagger, service names from compose)
// can never break out of their tags.
func (s *dashboardServer) handleIndex(w http.ResponseWriter, _ *http.Request) {
	tmpl, err := loadDashboardTemplate()
	if err != nil {
		http.Error(w, "dashboard template error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.mu.RLock()
	snapshot := s.state
	s.mu.RUnlock()

	// Execute into an in-memory buffer first so a render error doesn't
	// leave the response half-written with a partial page visible to
	// the client.
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, snapshot); err != nil {
		http.Error(w, "dashboard render error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

// handleState serves the current state snapshot as JSON. Cheap to call;
// use /api/stream for push updates.
func (s *dashboardServer) handleState(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	snapshot := s.state
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snapshot)
}

// handleStream is an SSE endpoint that pushes a fresh state snapshot
// to the connected client on every refresher tick (5s).
func (s *dashboardServer) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan dashboardState, 4)
	s.listeners.Store(ch, struct{}{})
	defer func() {
		s.listeners.Delete(ch)
		close(ch)
	}()

	// Prime the client with the current state so it doesn't have to wait
	// up to 5s for the first tick.
	s.mu.RLock()
	snapshot := s.state
	s.mu.RUnlock()
	writeSSE(w, flusher, snapshot)

	for {
		select {
		case <-r.Context().Done():
			return
		case st := <-ch:
			writeSSE(w, flusher, st)
		}
	}
}

// writeSSE marshals one state snapshot as an SSE data: frame.
func writeSSE(w http.ResponseWriter, flusher http.Flusher, st dashboardState) {
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	flusher.Flush()
}

// handleHAR serializes the current request ring as HAR 1.2 JSON so
// developers can hand the file to any HAR-aware viewer (Chrome
// DevTools, insomnia, postman). Keeping this client-side-triggered
// means the server doesn't persist the HAR anywhere — it's a download,
// not a report.
func (s *dashboardServer) handleHAR(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	reqs := s.state.RecentRequests
	s.mu.RUnlock()
	har := buildHAR(reqs)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="gofasta-dev.har"`)
	_ = json.NewEncoder(w).Encode(har)
}

// buildHAR converts scrapedRequest entries into the HAR 1.2 shape the
// ecosystem's tooling reads. We emit minimally — no cookies, no
// detailed headers beyond Content-Type, no timings breakdown —
// because the devtools ring doesn't capture any of that. Viewers
// gracefully degrade.
func buildHAR(reqs []scrapedRequest) harDoc {
	entries := make([]harEntry, 0, len(reqs))
	for _, r := range reqs {
		ctype := r.ResponseContentType
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		reqContentType := "application/json"
		entries = append(entries, harEntry{
			StartedDateTime: r.Time.UTC().Format(time.RFC3339Nano),
			Time:            r.DurationMS,
			Request: harRequest{
				Method:      r.Method,
				URL:         r.Path,
				HTTPVersion: "HTTP/1.1",
				Cookies:     []struct{}{},
				Headers:     []struct{}{},
				QueryString: []struct{}{},
				PostData: &harPostData{
					MimeType: reqContentType,
					Text:     r.Body,
				},
				HeadersSize: -1,
				BodySize:    int64(len(r.Body)),
			},
			Response: harResponse{
				Status:      r.Status,
				StatusText:  http.StatusText(r.Status),
				HTTPVersion: "HTTP/1.1",
				Cookies:     []struct{}{},
				Headers:     []struct{}{},
				Content: harContent{
					Size:     int64(len(r.ResponseBody)),
					MimeType: ctype,
					Text:     r.ResponseBody,
				},
				RedirectURL: "",
				HeadersSize: -1,
				BodySize:    int64(len(r.ResponseBody)),
			},
			Cache:   struct{}{},
			Timings: harTimings{Send: 0, Wait: r.DurationMS, Receive: 0},
		})
	}
	return harDoc{
		Log: harLog{
			Version: "1.2",
			Creator: harCreator{Name: "gofasta dev dashboard", Version: "1"},
			Entries: entries,
		},
	}
}

// ── HAR 1.2 shape — https://en.wikipedia.org/wiki/HAR_(file_format) ──

type harDoc struct {
	Log harLog `json:"log"`
}
type harLog struct {
	Version string     `json:"version"`
	Creator harCreator `json:"creator"`
	Entries []harEntry `json:"entries"`
}
type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
type harEntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            int64       `json:"time"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
	Cache           struct{}    `json:"cache"`
	Timings         harTimings  `json:"timings"`
}
type harRequest struct {
	Method      string       `json:"method"`
	URL         string       `json:"url"`
	HTTPVersion string       `json:"httpVersion"`
	Cookies     []struct{}   `json:"cookies"`
	Headers     []struct{}   `json:"headers"`
	QueryString []struct{}   `json:"queryString"`
	PostData    *harPostData `json:"postData,omitempty"`
	HeadersSize int64        `json:"headersSize"`
	BodySize    int64        `json:"bodySize"`
}
type harPostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}
type harResponse struct {
	Status      int        `json:"status"`
	StatusText  string     `json:"statusText"`
	HTTPVersion string     `json:"httpVersion"`
	Cookies     []struct{} `json:"cookies"`
	Headers     []struct{} `json:"headers"`
	Content     harContent `json:"content"`
	RedirectURL string     `json:"redirectURL"`
	HeadersSize int64      `json:"headersSize"`
	BodySize    int64      `json:"bodySize"`
}
type harContent struct {
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
}
type harTimings struct {
	Send    int64 `json:"send"`
	Wait    int64 `json:"wait"`
	Receive int64 `json:"receive"`
}

// handleExplain forwards the dashboard's EXPLAIN request to the app's
// /debug/explain endpoint. The scaffold's handler enforces the
// SELECT-only whitelist and runs the plan against GORM; we pass the
// response through verbatim so any failure surfaces in the modal.
func (s *dashboardServer) handleExplain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.appURL+"/debug/explain", strings.NewReader(string(body)))
	if err != nil {
		http.Error(w, "build upstream: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// handleLogs proxies to the app's /debug/logs, forwarding the
// trace_id and level query parameters. Keeps the dashboard same-origin
// (no CORS) and lets the CLI inject other filtering later without
// the browser learning about the app's port layout.
func (s *dashboardServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	entries := scrapeLogs(s.appURL, r.URL.Query().Get("trace_id"), r.URL.Query().Get("level"))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entries)
}

// handleTraceDetail proxies to the app's /debug/traces/{id} endpoint
// and returns the full TraceEntry (every span, stack, attribute,
// event). The dashboard calls this on demand when the developer
// expands a trace row — keeping trace bodies out of the SSE stream
// keeps polling cheap.
func (s *dashboardServer) handleTraceDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/trace/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	entry, ok := scrapeTraceDetail(s.appURL, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entry)
}

// handleReplay re-fires a captured request against the app. The
// dashboard POSTs the original method + path + body here (scraped
// from the /debug/requests ring) rather than the dashboard opening a
// direct connection to the app, because browsers won't let us
// round-trip custom methods from the same-origin SSE client without
// CORS preflight headaches.
//
// Mutation methods (POST/PUT/PATCH/DELETE) round-trip the body
// verbatim so the app sees the exact same payload it saw before. The
// dashboard UI prompts the developer before replaying those.
//
// Security note: `req.Path` is attacker-controlled data. Naively
// concatenating it with s.appURL opens an SSRF window — e.g.
// `"@evil.com/x"` turns `http://localhost:8080` into a URL whose
// `localhost:8080` becomes userinfo and `evil.com` becomes the host.
// We parse the request path as a URL reference and explicitly pin
// the scheme+host+user to the resolved app URL before issuing the
// upstream request, so the user-supplied value can only influence
// the path + query portion.
func (s *dashboardServer) handleReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req replayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Method == "" || req.Path == "" {
		http.Error(w, "method and path are required", http.StatusBadRequest)
		return
	}
	method, err := validateReplayMethod(req.Method)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	target, err := buildReplayURL(s.appURL, req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var body io.Reader
	if req.Body != "" {
		body = strings.NewReader(req.Body)
	}
	upstream, err := http.NewRequestWithContext(r.Context(), method, target, body)
	if err != nil {
		http.Error(w, "build upstream: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Body != "" {
		upstream.Header.Set("Content-Type", "application/json")
	}
	upstream.Header.Set("X-Gofasta-Replay", "1")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(upstream)
	if err != nil {
		http.Error(w, "upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxReplayResponse))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(replayResult{
		Status:  resp.StatusCode,
		Body:    string(respBody),
		Headers: flattenHeaders(resp.Header),
	})
}

// replayAllowedMethods is the closed set of HTTP methods that can be
// replayed. Anything else (TRACE, CONNECT, custom verbs) is rejected
// so an attacker can't probe weird behaviors in the app via the
// replay endpoint.
var replayAllowedMethods = map[string]struct{}{
	http.MethodGet:     {},
	http.MethodPost:    {},
	http.MethodPut:     {},
	http.MethodPatch:   {},
	http.MethodDelete:  {},
	http.MethodHead:    {},
	http.MethodOptions: {},
}

// validateReplayMethod returns the canonical upper-case method name
// if it's in the allowlist; otherwise returns an error suitable for
// an HTTP 400 response.
func validateReplayMethod(method string) (string, error) {
	m := strings.ToUpper(strings.TrimSpace(method))
	if _, ok := replayAllowedMethods[m]; !ok {
		return "", fmt.Errorf("method %q is not allowed for replay", method)
	}
	return m, nil
}

// buildReplayURL safely combines the resolved app URL with the
// attacker-controlled path. It rejects any reference that carries a
// scheme, host, or userinfo, then explicitly pins the scheme, host,
// and user to the app URL's values — so the user's supplied input can
// influence only the path + query. Returns the fully-assembled URL
// string, ready for http.NewRequestWithContext.
func buildReplayURL(appURL, rawPath string) (string, error) {
	base, err := url.Parse(appURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("internal: resolved app URL %q is malformed", appURL)
	}
	ref, err := url.Parse(rawPath)
	if err != nil {
		return "", fmt.Errorf("path is not a valid URL reference")
	}
	if ref.Scheme != "" || ref.Host != "" || ref.User != nil || ref.Opaque != "" {
		// Reject anything that could redirect the request to a
		// different host. Explicit error message — the dashboard UI
		// surfaces it to the developer.
		return "", fmt.Errorf("path must be relative (no scheme, host, or userinfo)")
	}
	// Require a leading slash. Rejects `//evil.com/x` (network-path
	// reference, which some URL parsers treat as scheme-relative) and
	// any path that would resolve relative to an unknown base.
	if !strings.HasPrefix(ref.Path, "/") {
		return "", fmt.Errorf("path must start with /")
	}
	// Reassemble: base's scheme+host+user, ref's path+query. Copy
	// the base rather than mutating so concurrent handlers don't
	// race on s.appURL derivatives.
	out := *base
	out.Path = ref.Path
	out.RawQuery = ref.RawQuery
	out.Fragment = ""
	return out.String(), nil
}

// maxReplayResponse caps the response body the dashboard shows after
// a replay. Bodies past this size are truncated so an accidental
// replay against a large-list endpoint doesn't stuff the dashboard
// tab with MB of JSON.
const maxReplayResponse = 256 * 1024

type replayRequest struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   string `json:"body,omitempty"`
}

type replayResult struct {
	Status  int               `json:"status"`
	Body    string            `json:"body"`
	Headers map[string]string `json:"headers,omitempty"`
}

// flattenHeaders picks the first value of each response header — the
// dashboard displays only a flat key/value list, multi-value headers
// (Set-Cookie) aren't useful in a replay context.
func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}
