package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// debugHealthFixture spins up an httptest server that responds to
// every /debug/* endpoint so the health command sees a complete
// surface. The returned url is ready to pass as --app-url.
func debugHealthFixture(t *testing.T, devtools string) string {
	t.Helper()
	handler := http.NewServeMux()
	handler.HandleFunc("/debug/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"devtools":"` + devtools + `"}`))
	})
	// Other endpoints respond 200 so the liveness matrix reflects
	// reality under the devtools=enabled scenario.
	for _, path := range []string{
		"/debug/requests", "/debug/sql", "/debug/traces",
		"/debug/logs", "/debug/errors", "/debug/cache",
		"/debug/pprof/",
	} {
		handler.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("[]"))
		})
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestRunDebugHealth_Enabled — happy path: devtools tag set, every
// endpoint reachable. Report should show reachable=true, devtools=
// "enabled", every status code 200.
func TestRunDebugHealth_Enabled(t *testing.T) {
	url := debugHealthFixture(t, "enabled")
	debugAppURL = url
	t.Cleanup(func() { debugAppURL = "" })

	// Build the report in the same way runDebugHealth would. This
	// sidesteps stdout capture and gives us the structured payload
	// directly so we can assert on it.
	appURL := resolveAppURL()
	report := debugHealthReport{AppURL: appURL}
	probeEndpoint(appURL, "/debug/health", &report)
	report.Reachable = report.Endpoints[0].Status == 200
	if report.Reachable {
		report.Devtools = readDevtoolsState(appURL)
	}
	for _, p := range []string{
		"/debug/requests", "/debug/sql", "/debug/traces",
		"/debug/logs", "/debug/errors", "/debug/cache",
		"/debug/pprof/",
	} {
		probeEndpoint(appURL, p, &report)
	}

	assert.True(t, report.Reachable)
	assert.Equal(t, "enabled", report.Devtools)
	require.Len(t, report.Endpoints, 8)
	for _, e := range report.Endpoints {
		assert.Equal(t, 200, e.Status, "endpoint %s", e.Path)
	}
}

// TestRunDebugHealth_Stub — production build path: /debug/health
// reports "stub", so downstream commands would 404. Report must show
// devtools="stub" so the agent branches cleanly.
func TestRunDebugHealth_Stub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/debug/health" {
			_, _ = w.Write([]byte(`{"devtools":"stub"}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	debugAppURL = srv.URL
	t.Cleanup(func() { debugAppURL = "" })

	appURL := resolveAppURL()
	report := debugHealthReport{AppURL: appURL}
	probeEndpoint(appURL, "/debug/health", &report)
	report.Reachable = report.Endpoints[0].Status == 200
	if report.Reachable {
		report.Devtools = readDevtoolsState(appURL)
	}

	assert.True(t, report.Reachable)
	assert.Equal(t, "stub", report.Devtools)
}

// TestRunDebugHealth_Unreachable — /debug/health times out / refuses
// connection. We expect reachable=false and devtools="unreachable".
func TestRunDebugHealth_Unreachable(t *testing.T) {
	// A closed server yields immediate connection refused (no sleep
	// needed). The test stays fast.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	closedURL := srv.URL
	srv.Close()

	debugAppURL = closedURL
	t.Cleanup(func() { debugAppURL = "" })

	appURL := resolveAppURL()
	report := debugHealthReport{AppURL: appURL}
	probeEndpoint(appURL, "/debug/health", &report)

	assert.Equal(t, 0, report.Endpoints[0].Status)
	assert.NotEmpty(t, report.Endpoints[0].Error)
}

// TestResolveAppURL_FromFlag — --app-url overrides config.yaml.
func TestResolveAppURL_FromFlag(t *testing.T) {
	debugAppURL = "http://10.0.0.1:9090"
	t.Cleanup(func() { debugAppURL = "" })
	assert.Equal(t, "http://10.0.0.1:9090", resolveAppURL())
}

// TestRequireDevtools_Enabled — probe succeeds → nil.
func TestRequireDevtools_Enabled(t *testing.T) {
	url := debugHealthFixture(t, "enabled")
	assert.NoError(t, requireDevtools(url))
}

// TestRequireDevtools_StubReturnsCode — probe replies stub → error
// with DEBUG_DEVTOOLS_OFF so agents branch on the code.
func TestRequireDevtools_StubReturnsCode(t *testing.T) {
	url := debugHealthFixture(t, "stub")
	err := requireDevtools(url)
	require.Error(t, err)
	b, _ := json.Marshal(err)
	assert.Contains(t, string(b), "DEBUG_DEVTOOLS_OFF")
}

// TestRequireDevtools_Unreachable — wrong URL → DEBUG_APP_UNREACHABLE.
func TestRequireDevtools_Unreachable(t *testing.T) {
	err := requireDevtools("http://127.0.0.1:1") // guaranteed-unused port
	require.Error(t, err)
	b, _ := json.Marshal(err)
	assert.True(t,
		strings.Contains(string(b), "DEBUG_APP_UNREACHABLE"),
		"expected DEBUG_APP_UNREACHABLE, got %s", string(b),
	)
}

// TestContainsSubstring_EdgeCases — needle longer than haystack,
// empty haystack, exact match, substring match.
func TestContainsSubstring_EdgeCases(t *testing.T) {
	assert.False(t, containsSubstring([]byte("short"), "longer-needle"))
	assert.False(t, containsSubstring([]byte(""), "x"))
	assert.True(t, containsSubstring([]byte("abc-xyz"), "xyz"))
	assert.True(t, containsSubstring([]byte("abc"), "abc"))
}
