package commands

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
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
	AppPort         int              `json:"app_port"`
	AppURL          string           `json:"app_url"`
	Health          string           `json:"health"` // "ok" | "unreachable" | "unhealthy"
	Services        []serviceState   `json:"services"`
	Routes          []dashboardRoute `json:"routes"`
	SwaggerURL      string           `json:"swagger_url,omitempty"`
	GraphQLURL      string           `json:"graphql_url,omitempty"`
	MetricsURL      string           `json:"metrics_url,omitempty"`
	Metrics         metricsSnapshot  `json:"metrics"`
	DevtoolsEnabled bool             `json:"devtools_enabled"`
	RecentRequests  []scrapedRequest `json:"recent_requests,omitempty"`
	RecentQueries   []scrapedQuery   `json:"recent_queries,omitempty"`
	LastUpdatedMS   int64            `json:"last_updated_ms"`
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

	var states []serviceState
	if s.svc != nil && len(s.svc.selected) > 0 {
		if live, err := queryServiceStates(); err == nil {
			for _, st := range live {
				for _, sel := range s.svc.selected {
					if st.Name == sel {
						states = append(states, st)
					}
				}
			}
		}
	}

	// External scrapes — each fails soft (empty result) so one missing
	// surface never blanks the whole dashboard.
	metrics := scrapeMetrics(s.appURL)
	devtoolsOn := devtoolsAvailable(s.appURL)
	var recentReqs []scrapedRequest
	var recentQueries []scrapedQuery
	if devtoolsOn {
		recentReqs = scrapeRequestLog(s.appURL)
		recentQueries = scrapeSQLLog(s.appURL)
	}

	s.mu.Lock()
	s.state.Health = health
	s.state.Services = states
	s.state.Metrics = metrics
	s.state.DevtoolsEnabled = devtoolsOn
	s.state.RecentRequests = recentReqs
	s.state.RecentQueries = recentQueries
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

// probeHealth does a 2-second-timeout GET against the app's /health
// endpoint. Doesn't care about the body — any 2xx counts as healthy.
func probeHealth(url string) string {
	client := &http.Client{Timeout: 2 * time.Second}
	//nolint:noctx // Short-lived single-purpose client; no context threading needed.
	resp, err := client.Get(url)
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
