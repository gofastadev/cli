package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

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
	AppPort       int              `json:"app_port"`
	AppURL        string           `json:"app_url"`
	Health        string           `json:"health"` // "ok" | "unreachable" | "unhealthy"
	Services      []serviceState   `json:"services"`
	Routes        []dashboardRoute `json:"routes"`
	SwaggerURL    string           `json:"swagger_url,omitempty"`
	GraphQLURL    string           `json:"graphql_url,omitempty"`
	MetricsURL    string           `json:"metrics_url,omitempty"`
	LastUpdatedMS int64            `json:"last_updated_ms"`
}

// dashboardRoute is a single REST route scraped from the scaffold's
// `gofasta routes --json` output (if available). Kept minimal so the
// dashboard remains resilient to routing schema changes.
type dashboardRoute struct {
	Method string `json:"method"`
	Path   string `json:"path"`
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

	s.mu.Lock()
	s.state.Health = health
	s.state.Services = states
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

// readRouteEntries opens `docs/swagger.json` and extracts a simple
// (method, path) list. The scaffold regenerates swagger.json on build,
// so this is usually fresh. If the file is missing (GraphQL-only
// projects, or before the first build) returns nil and the dashboard
// renders an empty routes table rather than blocking.
func readRouteEntries() []dashboardRoute {
	path := filepath.Join("docs", "swagger.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	entries := make([]dashboardRoute, 0, len(doc.Paths))
	for routePath, methods := range doc.Paths {
		for method := range methods {
			entries = append(entries, dashboardRoute{
				Method: strings.ToUpper(method),
				Path:   routePath,
			})
		}
	}
	return entries
}

// handleIndex serves the static dashboard HTML. Small enough to
// template inline so there's no filesystem dependency.
func (s *dashboardServer) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
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

// dashboardHTML is the single-file HTML page served at /. Uses the SSE
// endpoint for live updates and the snapshot endpoint for first paint
// fallback. Zero external assets — the page is fully self-contained so
// the dashboard works offline.
const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Gofasta dev dashboard</title>
<style>
  :root {
    --bg: #0a1c24;
    --surface: #172b34;
    --primary: #4fd1e5;
    --fg: #d9eaee;
    --muted: #8a9ba0;
    --ok: #22c55e;
    --warn: #f59e0b;
    --err: #ef4444;
  }
  * { box-sizing: border-box; }
  body {
    background: var(--bg); color: var(--fg);
    font-family: system-ui, -apple-system, sans-serif;
    margin: 0; padding: 24px;
  }
  h1 { color: var(--primary); margin: 0 0 4px 0; font-size: 20px; }
  h2 { color: var(--fg); margin: 24px 0 12px 0; font-size: 14px; text-transform: uppercase; letter-spacing: 0.08em; }
  .sub { color: var(--muted); font-size: 13px; }
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 12px; }
  .card {
    background: var(--surface); border-radius: 8px;
    padding: 16px; border: 1px solid rgba(79,209,229,0.15);
  }
  .card .label { color: var(--muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.08em; }
  .card .value { font-size: 18px; margin-top: 4px; font-family: ui-monospace, Menlo, monospace; }
  a { color: var(--primary); text-decoration: none; }
  a:hover { text-decoration: underline; }
  table { width: 100%; border-collapse: collapse; background: var(--surface); border-radius: 8px; overflow: hidden; }
  th, td { text-align: left; padding: 8px 12px; border-bottom: 1px solid rgba(138,155,160,0.15); font-family: ui-monospace, Menlo, monospace; font-size: 13px; }
  th { color: var(--muted); font-weight: 500; font-size: 11px; text-transform: uppercase; letter-spacing: 0.08em; }
  .method { display: inline-block; padding: 2px 6px; border-radius: 4px; background: rgba(79,209,229,0.15); color: var(--primary); font-size: 11px; }
  .pill { display: inline-block; padding: 2px 8px; border-radius: 999px; font-size: 11px; font-family: ui-monospace, Menlo, monospace; }
  .pill.ok { background: rgba(34,197,94,0.15); color: var(--ok); }
  .pill.warn { background: rgba(245,158,11,0.15); color: var(--warn); }
  .pill.err { background: rgba(239,68,68,0.15); color: var(--err); }
  .empty { color: var(--muted); font-style: italic; padding: 16px; text-align: center; }
  .footer { color: var(--muted); font-size: 11px; margin-top: 24px; }
</style>
</head>
<body>
<h1>Gofasta dev dashboard</h1>
<div class="sub">Live debug view for the project running on <span id="app-url"></span></div>

<h2>App</h2>
<div class="grid">
  <div class="card"><div class="label">Health</div><div class="value" id="health"><span class="pill">loading</span></div></div>
  <div class="card"><div class="label">Port</div><div class="value" id="port">—</div></div>
  <div class="card"><div class="label">Swagger</div><div class="value" id="swagger">—</div></div>
  <div class="card"><div class="label">GraphQL</div><div class="value" id="graphql">—</div></div>
</div>

<h2>Services</h2>
<div id="services"><div class="empty">No compose services attached.</div></div>

<h2>Routes</h2>
<div id="routes"><div class="empty">No routes scraped yet (regenerate swagger to populate).</div></div>

<div class="footer">Updated <span id="updated">—</span> · refreshes every 5s</div>

<script>
function renderHealth(h) {
  const cls = h === 'ok' ? 'ok' : (h === 'unhealthy' ? 'warn' : 'err');
  return '<span class="pill ' + cls + '">' + h + '</span>';
}
function renderLink(url) {
  if (!url) return '—';
  return '<a href="' + url + '" target="_blank">' + url + '</a>';
}
function renderServices(services) {
  if (!services || services.length === 0) {
    return '<div class="empty">No compose services attached.</div>';
  }
  let rows = services.map(s => {
    const status = (s.Health || s.State || '').toLowerCase();
    const cls = status === 'healthy' || status === 'running' ? 'ok' : 'warn';
    return '<tr><td>' + s.Service + '</td><td><span class="pill ' + cls + '">' + (s.Health || s.State) + '</span></td></tr>';
  }).join('');
  return '<table><thead><tr><th>Service</th><th>State</th></tr></thead><tbody>' + rows + '</tbody></table>';
}
function renderRoutes(routes) {
  if (!routes || routes.length === 0) {
    return '<div class="empty">No routes scraped yet (regenerate swagger to populate).</div>';
  }
  let rows = routes.map(r =>
    '<tr><td><span class="method">' + r.method + '</span></td><td>' + r.path + '</td></tr>'
  ).join('');
  return '<table><thead><tr><th>Method</th><th>Path</th></tr></thead><tbody>' + rows + '</tbody></table>';
}
function apply(state) {
  document.getElementById('app-url').textContent = state.app_url;
  document.getElementById('health').innerHTML = renderHealth(state.health);
  document.getElementById('port').textContent = state.app_port;
  document.getElementById('swagger').innerHTML = renderLink(state.swagger_url);
  document.getElementById('graphql').innerHTML = renderLink(state.graphql_url);
  document.getElementById('services').innerHTML = renderServices(state.services);
  document.getElementById('routes').innerHTML = renderRoutes(state.routes);
  document.getElementById('updated').textContent = new Date(state.last_updated_ms).toLocaleTimeString();
}
fetch('/api/state').then(r => r.json()).then(apply);
const es = new EventSource('/api/stream');
es.onmessage = e => apply(JSON.parse(e.data));
</script>
</body>
</html>`
