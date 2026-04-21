package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Replay-URL hardening tests.
//
// handleReplay forwards the user-supplied (method, path, body) to the
// resolved app URL. The URL assembly has to be paranoid because
// `path` comes from JSON in an untrusted request body — a naive
// `appURL + path` concatenation opens SSRF to any host the attacker
// names (via userinfo injection or a protocol-relative reference).
//
// CodeQL flagged this in April 2026 as "Uncontrolled data used in
// network request"; these tests lock in the fix against regression.
// ─────────────────────────────────────────────────────────────────────

// TestBuildReplayURL_ValidPathPasses — happy path: a leading-slash
// relative path produces the expected fully-qualified URL with the
// app's scheme + host preserved.
func TestBuildReplayURL_ValidPathPasses(t *testing.T) {
	cases := map[string]string{
		"/api/v1/users":        "http://localhost:8080/api/v1/users",
		"/api/v1/users?q=x":    "http://localhost:8080/api/v1/users?q=x",
		"/health":              "http://localhost:8080/health",
		"/api/v1/orders/42/ok": "http://localhost:8080/api/v1/orders/42/ok",
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			got, err := buildReplayURL("http://localhost:8080", path)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

// TestBuildReplayURL_RejectsUserinfoInjection — the core SSRF vector
// CodeQL flagged. "@evil.com/x" should not become the request's
// host. If this test fails the fix has regressed.
func TestBuildReplayURL_RejectsUserinfoInjection(t *testing.T) {
	_, err := buildReplayURL("http://localhost:8080", "@evil.com/x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path must")
}

// TestBuildReplayURL_RejectsFullURL — explicit scheme+host should be
// rejected outright. Would otherwise redirect the entire request.
func TestBuildReplayURL_RejectsFullURL(t *testing.T) {
	for _, in := range []string{
		"http://evil.com/x",
		"https://evil.com/x",
		"ftp://evil.com/x",
		"//evil.com/x",
	} {
		t.Run(in, func(t *testing.T) {
			_, err := buildReplayURL("http://localhost:8080", in)
			require.Error(t, err, "should reject %q", in)
		})
	}
}

// TestBuildReplayURL_RejectsMissingLeadingSlash — relative paths
// without a leading slash could resolve ambiguously depending on the
// base URL's path. Require absolute paths so behavior is predictable.
func TestBuildReplayURL_RejectsMissingLeadingSlash(t *testing.T) {
	_, err := buildReplayURL("http://localhost:8080", "api/v1/users")
	require.Error(t, err)
}

// TestBuildReplayURL_PreservesAppHostAcrossInjection — even a
// carefully-crafted path that parses as a relative URL must NOT
// escape the app host. Opaque URLs (mailto:foo, data:...) get
// rejected.
func TestBuildReplayURL_RejectsOpaqueScheme(t *testing.T) {
	for _, in := range []string{
		"mailto:attacker@evil.com",
		"data:text/plain,foo",
	} {
		t.Run(in, func(t *testing.T) {
			_, err := buildReplayURL("http://localhost:8080", in)
			require.Error(t, err)
		})
	}
}

// TestBuildReplayURL_RejectsInvalidURL — malformed input falls into
// a generic error rather than panicking.
func TestBuildReplayURL_RejectsInvalidURL(t *testing.T) {
	_, err := buildReplayURL("http://localhost:8080", "%gh")
	require.Error(t, err)
}

// TestBuildReplayURL_BadAppURL — if the app URL itself is malformed
// (shouldn't happen; config validates it), the function rejects
// rather than blindly proceeding. Covers the defensive internal-error
// branch.
func TestBuildReplayURL_BadAppURL(t *testing.T) {
	_, err := buildReplayURL("not-a-url", "/x")
	require.Error(t, err)
}

// TestValidateReplayMethod_AllowList — only the standard HTTP
// methods pass; unusual verbs (CONNECT, TRACE, custom strings)
// rejected.
func TestValidateReplayMethod_AllowList(t *testing.T) {
	ok := []string{"GET", "get", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
	for _, m := range ok {
		t.Run("ok/"+m, func(t *testing.T) {
			got, err := validateReplayMethod(m)
			require.NoError(t, err)
			assert.Equal(t, strings.ToUpper(m), got)
		})
	}
	for _, m := range []string{"CONNECT", "TRACE", "PROPFIND", "", "GARBAGE"} {
		t.Run("reject/"+m, func(t *testing.T) {
			_, err := validateReplayMethod(m)
			require.Error(t, err)
		})
	}
}

// TestHandleReplay_BlocksSSRFViaPath — end-to-end: the dashboard's
// /api/replay handler rejects a userinfo-injection attempt before
// ever issuing an upstream HTTP call. We stand up a sentinel server
// bound to a throwaway port and confirm no request ever lands on it.
func TestHandleReplay_BlocksSSRFViaPath(t *testing.T) {
	// Sentinel — any request here is the SSRF working. Fail loudly.
	sentinel := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("sentinel got unexpected request: %s %s (host=%s)",
			r.Method, r.URL.Path, r.Host)
	}))
	defer sentinel.Close()

	// Build a dashboardServer whose appURL points at a fixed
	// localhost — the test server itself would work but we want the
	// sentinel separately so any leak is unambiguous.
	srv := &dashboardServer{appURL: "http://127.0.0.1:0"}

	// Evil path that — without the fix — would turn into
	// http://127.0.0.1:0@evil.com/x, i.e. host=evil.com.
	// We try pointing the `@` prefix at the sentinel's host so if
	// SSRF works the sentinel sees the request.
	evilPath := "@" + sentinel.Listener.Addr().String() + "/x"
	body, _ := json.Marshal(replayRequest{
		Method: "GET",
		Path:   evilPath,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/replay", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv.handleReplay(rec, req)

	// Expect a 400 Bad Request from our validator, not a 502 from
	// an attempted-but-failed proxy. Either way, the sentinel's
	// t.Errorf in its handler would fire if the request leaked.
	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"body: %s", rec.Body.String())
}

// TestHandleReplay_BlocksSSRFViaScheme — same shape, but the attack
// tries to inject an explicit scheme+host.
func TestHandleReplay_BlocksSSRFViaScheme(t *testing.T) {
	sentinel := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("sentinel got unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer sentinel.Close()

	srv := &dashboardServer{appURL: "http://127.0.0.1:0"}
	body, _ := json.Marshal(replayRequest{
		Method: "GET",
		Path:   sentinel.URL + "/x",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/replay", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleReplay_BlocksBadMethod — replay rejects CONNECT/TRACE
// before touching the network.
func TestHandleReplay_BlocksBadMethod(t *testing.T) {
	srv := &dashboardServer{appURL: "http://127.0.0.1:0"}
	body, _ := json.Marshal(replayRequest{
		Method: "CONNECT",
		Path:   "/x",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/replay", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleReplay_NewRequestFails — inject a failing newReplayRequest
// seam → handler returns 400.
func TestHandleReplay_NewRequestFails(t *testing.T) {
	orig := newReplayRequest
	newReplayRequest = func(context.Context, string, string, io.Reader) (*http.Request, error) {
		return nil, fmt.Errorf("build failed")
	}
	t.Cleanup(func() { newReplayRequest = orig })
	srv := &dashboardServer{appURL: "http://irrelevant"}
	req := httptest.NewRequest(http.MethodPost, "/api/replay",
		strings.NewReader(`{"method":"GET","path":"/x"}`))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleReplay_WrongMethod — GET /api/replay → 405.
func TestHandleReplay_WrongMethod(t *testing.T) {
	srv := &dashboardServer{appURL: "http://irrelevant"}
	req := httptest.NewRequest(http.MethodGet, "/api/replay", nil)
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// TestHandleReplay_WithBody — body != "" exercises the body = reader
// and Content-Type setter branches.
func TestHandleReplay_WithBody(t *testing.T) {
	srv := &dashboardServer{appURL: "http://127.0.0.1:1"}
	req := httptest.NewRequest(http.MethodPost, "/api/replay",
		strings.NewReader(`{"method":"POST","path":"/x","body":"data"}`))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	// Upstream unreachable → 502.
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestHandleReplay_BadAppURL — makes http.NewRequestWithContext fail
// indirectly via buildReplayURL rejecting the malformed app URL.
func TestHandleReplay_BadAppURL(t *testing.T) {
	srv := &dashboardServer{appURL: "\x7f://bad"}
	req := httptest.NewRequest(http.MethodPost, "/api/replay",
		strings.NewReader(`{"method":"GET","path":"/x"}`))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	// buildReplayURL will fail on the malformed app URL.
	assert.GreaterOrEqual(t, rec.Code, 400)
}

// TestHandleReplay_NewRequestError — handleReplay's
// http.NewRequestWithContext error branch is unreachable after the
// validators; documented here.
func TestHandleReplay_NewRequestError(t *testing.T) {
	srv := &dashboardServer{appURL: "http://localhost:1234"}
	_ = srv
	t.Skip("handleReplay NewRequestWithContext error unreachable after validators")
}

// TestHandleReplay_AcceptsValidReplay — end-to-end happy path:
// dashboard → /api/replay → upstream app → response bubbled back as
// JSON. The upstream is our own stub; we just confirm the response
// shape.
func TestHandleReplay_AcceptsValidReplay(t *testing.T) {
	var upstreamHits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		assert.Equal(t, "/api/v1/health", r.URL.Path)
		assert.Equal(t, "1", r.Header.Get("X-Gofasta-Replay"))
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	srv := &dashboardServer{appURL: upstream.URL}
	body, _ := json.Marshal(replayRequest{
		Method: "get",
		Path:   "/api/v1/health",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/replay", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Equal(t, 1, upstreamHits)
	var out replayResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, 200, out.Status)
	assert.Equal(t, `{"ok":true}`, out.Body)
}
