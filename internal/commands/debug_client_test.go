package commands

import (
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

// TestGetJSON_DecodesBody — happy path: 200 + JSON body decodes into
// the caller's struct.
func TestGetJSON_DecodesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/debug/requests", r.URL.Path)
		_, _ = w.Write([]byte(`[{"method":"GET","path":"/x"}]`))
	}))
	defer srv.Close()

	var out []scrapedRequest
	require.NoError(t, getJSON(srv.URL, "/debug/requests", &out))
	require.Len(t, out, 1)
	assert.Equal(t, "GET", out[0].Method)
}

// TestGetJSON_404ReturnsTraceNotFound — the shared path maps 404 to
// DEBUG_TRACE_NOT_FOUND so callers like `debug trace <id>` get a
// specific error code without custom handling.
func TestGetJSON_404ReturnsTraceNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer srv.Close()
	err := getJSON(srv.URL, "/debug/traces/abc", &struct{}{})
	require.Error(t, err)
	b, _ := json.Marshal(err)
	assert.Contains(t, string(b), "DEBUG_TRACE_NOT_FOUND")
}

// TestGetJSON_Non2xxReturnsAppUnreachable — any non-2xx, non-404
// surfaces as DEBUG_APP_UNREACHABLE with the body attached.
func TestGetJSON_Non2xxReturnsAppUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	err := getJSON(srv.URL, "/debug/requests", &struct{}{})
	require.Error(t, err)
	b, _ := json.Marshal(err)
	assert.Contains(t, string(b), "DEBUG_APP_UNREACHABLE")
	assert.Contains(t, err.Error(), "boom")
}

// TestGetJSON_NetworkError — wrong port returns an error wrapping the
// original net error.
func TestGetJSON_NetworkError(t *testing.T) {
	err := getJSON("http://127.0.0.1:1", "/debug/requests", &struct{}{})
	require.Error(t, err)
}

// TestGetJSON_MalformedBody — 200 with invalid JSON returns an error
// (not a silent empty out value).
func TestGetJSON_MalformedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()
	err := getJSON(srv.URL, "/x", &struct{}{})
	require.Error(t, err)
}

// TestPostJSON_HappyPath — body is sent as JSON, response decoded.
func TestPostJSON_HappyPath(t *testing.T) {
	var received map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		_ = json.NewDecoder(r.Body).Decode(&received)
		_, _ = w.Write([]byte(`{"plan":"ok"}`))
	}))
	defer srv.Close()

	var out struct {
		Plan string `json:"plan"`
	}
	require.NoError(t, postJSON(srv.URL, "/debug/explain",
		map[string]string{"sql": "SELECT 1"}, &out))
	assert.Equal(t, "ok", out.Plan)
	assert.Equal(t, "SELECT 1", received["sql"])
}

// TestPostJSON_NilOut — callers that only care about success (nil
// out) get a nil return instead of a decode error.
func TestPostJSON_NilOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	require.NoError(t, postJSON(srv.URL, "/x", map[string]string{}, nil))
}

// TestPostJSON_Non2xxSurfacesExplainFailed — /debug/explain returning
// 4xx surfaces as DEBUG_EXPLAIN_FAILED so the CLI can show a clear
// remediation hint.
func TestPostJSON_Non2xxSurfacesExplainFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("only SELECT"))
	}))
	defer srv.Close()
	err := postJSON(srv.URL, "/debug/explain", map[string]string{}, nil)
	require.Error(t, err)
	b, _ := json.Marshal(err)
	assert.Contains(t, string(b), "DEBUG_EXPLAIN_FAILED")
}

// TestPostJSON_NetworkError — unreachable host surfaces as
// DEBUG_APP_UNREACHABLE.
func TestPostJSON_NetworkError(t *testing.T) {
	err := postJSON("http://127.0.0.1:1", "/x", map[string]string{}, nil)
	require.Error(t, err)
}

// TestAppendQuery — empty params skipped; non-empty ones url-encoded.
func TestAppendQuery(t *testing.T) {
	assert.Equal(t, "/x", appendQuery("/x", nil))
	assert.Equal(t, "/x", appendQuery("/x", map[string]string{"a": "", "b": ""}))
	got := appendQuery("/debug/logs", map[string]string{
		"trace_id": "abc",
		"level":    "",
	})
	assert.Equal(t, "/debug/logs?trace_id=abc", got)
	// Multi-param ordering is map-iteration-dependent but both keys
	// must appear — use substring check.
	multi := appendQuery("/x", map[string]string{"a": "1", "b": "2"})
	assert.Contains(t, multi, "a=1")
	assert.Contains(t, multi, "b=2")
}

// TestBytesReader — reads correct bytes, reports EOF at end.
func TestBytesReader(t *testing.T) {
	r := bytesReader([]byte("hello"))
	buf, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(buf))

	// Second read returns EOF.
	r = bytesReader(nil)
	b := make([]byte, 4)
	n, err := r.Read(b)
	assert.Equal(t, 0, n)
	assert.Equal(t, io.EOF, err)
}

// TestBytesReader_MultiRead — subsequent reads advance pos correctly.
func TestBytesReader_MultiRead(t *testing.T) {
	r := bytesReader([]byte("abcdef"))
	buf := make([]byte, 3)
	n, err := r.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, "abc", string(buf[:n]))

	n, err = r.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, "def", string(buf[:n]))

	n, err = r.Read(buf)
	assert.Equal(t, 0, n)
	assert.Equal(t, io.EOF, err)
}

// TestResolveAppURL_DefaultPort — with no override and no config, we
// fall through configutil.GetPort which returns 8080.
func TestResolveAppURL_DefaultPort(t *testing.T) {
	saved := debugAppURL
	debugAppURL = ""
	t.Cleanup(func() { debugAppURL = saved })
	got := resolveAppURL()
	assert.Contains(t, got, "http://localhost:")
}

// TestRequireDevtools_Non2xx — /debug/health returns 500.
func TestRequireDevtools_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	require.Error(t, requireDevtools(srv.URL))
}

// TestRequireDevtools_MalformedJSON — 200 but body isn't JSON.
func TestRequireDevtools_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()
	require.Error(t, requireDevtools(srv.URL))
}

// TestPostJSON_BadResponse — server returns malformed JSON body;
// postJSON propagates the decode error.
func TestPostJSON_BadResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()
	var out map[string]interface{}
	require.Error(t, postJSON(srv.URL, "/x", map[string]int{"a": 1}, &out))
}

// TestPostJSON_MarshalError — an input that can't be JSON-marshaled
// (channel) triggers the first error branch.
func TestPostJSON_MarshalError(t *testing.T) {
	var out map[string]interface{}
	require.Error(t, postJSON("http://irrelevant", "/x", make(chan int), &out))
}

// TestPostJSON_NewRequestError — an appURL with invalid characters
// makes http.NewRequest fail.
func TestPostJSON_NewRequestError(t *testing.T) {
	var out map[string]interface{}
	// A control character in the URL trips NewRequest validation.
	require.Error(t, postJSON("\x7f://bad", "/x", map[string]int{}, &out))
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

// Compile-time assertion: bytesReader returns an io.Reader.
var _ io.Reader = bytesReader(nil)

// debugFixture stands up a test server that serves every /debug/*
// endpoint using the caller-supplied handler map. An entry for
// /debug/health is prepended so requireDevtools passes unless the
// caller overrides it.
func debugFixture(t *testing.T, handlers map[string]http.HandlerFunc) (url string) {
	t.Helper()
	mux := http.NewServeMux()
	// Default /debug/health → enabled unless the caller overrode it.
	if _, set := handlers["/debug/health"]; !set {
		mux.HandleFunc("/debug/health", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"devtools":"enabled"}`))
		})
	}
	for path, h := range handlers {
		mux.HandleFunc(path, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// debug500 stands up a fixture whose named debug endpoint returns 500.
// /debug/health still returns {"devtools":"enabled"} so requireDevtools
// passes and runDebug* reaches the getJSON call.
func debug500(t *testing.T, path string) string {
	return debugFixture(t, map[string]http.HandlerFunc{
		path: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	})
}

// writeJSON is a convenience so fixture handlers don't have to
// remember to set Content-Type.
func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// errWriter returns an error on every Write; used by debug tests to
// force encoder / io.Copy errors.
type errWriter struct{}

func (errWriter) Write(_ []byte) (int, error) { return 0, fmt.Errorf("write boom") }

// debugFixtureAll serves an "everything succeeds" upstream app so any
// runDebug* invocation that doesn't care about the filter arguments
// returns nil. Individual tests can narrow this down if needed.
func debugFixtureAll(t *testing.T) string {
	return debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/sql":        func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/traces":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/traces/t1":  func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"trace_id":"t1"}`)) },
		"/debug/errors":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/cache":      func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/logs":       func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) },
		"/debug/pprof/":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) },
		"/debug/pprof/heap": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("heap-bytes")) },
		"/debug/pprof/goroutine": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("goroutine 1 [running]:\nmain.x()\n"))
		},
		"/debug/explain": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"plan":"ok"}`)) },
	})
}
