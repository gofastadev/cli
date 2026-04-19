package commands

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDashboardTemplate_Parses — the embedded dev_dashboard.html template
// must parse successfully. Caught by this test rather than at runtime
// the first time the dashboard flag is used.
func TestDashboardTemplate_Parses(t *testing.T) {
	tmpl, err := loadDashboardTemplate()
	require.NoError(t, err)
	require.NotNil(t, tmpl)
}

// TestDashboardHandleIndex_RendersServerSideState — the index handler
// server-renders the current state so first paint shows real data (no
// "loading" flash). This asserts the rendered HTML contains the
// injected values.
func TestDashboardHandleIndex_RendersServerSideState(t *testing.T) {
	srv := &dashboardServer{
		state: dashboardState{
			AppPort:    8080,
			AppURL:     "http://localhost:8080",
			Health:     "ok",
			SwaggerURL: "http://localhost:8080/swagger/index.html",
			Routes: []dashboardRoute{
				{Method: "GET", Path: "/users"},
				{Method: "POST", Path: "/users"},
			},
			LastUpdatedMS: 1700000000000,
		},
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.handleIndex(rr, req)

	body := rr.Body.String()
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "text/html; charset=utf-8", rr.Header().Get("Content-Type"))

	// State is embedded, not "loading".
	assert.Contains(t, body, "http://localhost:8080")
	assert.Contains(t, body, "8080")
	// Health pill is rendered with the "ok" variant class.
	assert.Contains(t, body, `class="pill ok"`)
	// Routes table is populated with server-rendered entries. Asserting
	// on the presence of the routes <table> (inside <div id="routes">)
	// is unambiguous — the client-side JS fallback string in <script>
	// contains the "empty" placeholder too, so we can't exclude it, but
	// we can positively confirm the server rendered the real table.
	assert.Contains(t, body, "/users")
	assert.Contains(t, body, ">GET<")
	assert.Contains(t, body, ">POST<")
	// The #routes div should contain a <table>, not an <div class="empty">.
	routesBlock := extractBetween(body, `<div id="routes">`, `</div>`)
	assert.Contains(t, routesBlock, "<table>")
	assert.NotContains(t, routesBlock, `class="empty"`)
}

// extractBetween returns the substring of s between the first occurrence
// of start and the next occurrence of end after start. Used by the
// dashboard tests to isolate a rendered section (e.g. the #routes div)
// so assertions don't accidentally match content in <script> blocks.
func extractBetween(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	j := strings.Index(s, end)
	if j < 0 {
		return s
	}
	return s[:j]
}

// TestDashboardHandleIndex_EmptyState — with no services and no routes,
// the template falls back to the "empty" placeholders instead of
// rendering blank tables.
func TestDashboardHandleIndex_EmptyState(t *testing.T) {
	srv := &dashboardServer{
		state: dashboardState{
			AppPort: 8080,
			AppURL:  "http://localhost:8080",
			Health:  "unreachable",
		},
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.handleIndex(rr, req)

	body := rr.Body.String()
	assert.Equal(t, http.StatusOK, rr.Code)
	// Extract the two content divs so we're looking at the rendered
	// empty states, not the identical JS fallback strings inside the
	// <script> block.
	servicesBlock := extractBetween(body, `<div id="services">`, `</div>`)
	routesBlock := extractBetween(body, `<div id="routes">`, `</div>`)
	assert.Contains(t, servicesBlock, `class="empty"`)
	assert.Contains(t, routesBlock, `class="empty"`)
	// Unreachable health uses the error pill variant.
	assert.Contains(t, body, `class="pill err"`)
}

// TestDashboardHandleIndex_EscapesHostileInput — html/template must
// escape attacker-controlled values that end up in the rendered DOM.
// A compose service named `<script>alert(1)</script>` should render
// inert, not as an actual tag.
func TestDashboardHandleIndex_EscapesHostileInput(t *testing.T) {
	srv := &dashboardServer{
		state: dashboardState{
			AppPort: 8080,
			AppURL:  "http://localhost:8080",
			Health:  "ok",
			Services: []serviceState{
				{Name: `<script>alert(1)</script>`, State: "running", Health: "healthy"},
			},
			Routes: []dashboardRoute{
				{Method: "GET", Path: `/"><img src=x onerror=alert(1)>`},
			},
		},
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.handleIndex(rr, req)

	body := rr.Body.String()
	// No raw <script> tag should have made it through — the hostile
	// service name must appear only as escaped entities.
	assert.NotContains(t, body, "<script>alert(1)</script>")
	// No raw <img> tag either — auto-escape turns the angle brackets
	// into &lt; / &gt;.
	assert.False(t, strings.Contains(body, `<img src=x onerror=alert(1)>`),
		"hostile route path escaped into the DOM as a real tag")
	// Positive confirmation that the escaped form IS present (proves
	// the value wasn't silently dropped, only neutered).
	assert.Contains(t, body, "&lt;script&gt;")
	assert.Contains(t, body, "&lt;img src=x onerror=alert(1)&gt;")
}

// TestDashboardHandleState_ReturnsJSON — /api/state must return the
// snapshot with the expected Content-Type so browsers don't sniff.
func TestDashboardHandleState_ReturnsJSON(t *testing.T) {
	srv := &dashboardServer{
		state: dashboardState{AppPort: 8080, Health: "ok"},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	srv.handleState(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.Contains(t, rr.Body.String(), `"app_port":8080`)
	assert.Contains(t, rr.Body.String(), `"health":"ok"`)
}
