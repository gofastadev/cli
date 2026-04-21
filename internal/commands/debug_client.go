package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/commands/configutil"
)

// debugDefaultTimeout bounds most /debug/* queries. Profiles and the
// execution trace override this — see callers that pass a custom
// http.Client.
const debugDefaultTimeout = 5 * time.Second

// debugClient is the shared low-timeout HTTP client reused by every
// tier-1 debug command. Long-running endpoints (profiles, traces,
// streams) construct their own clients.
var debugClient = &http.Client{Timeout: debugDefaultTimeout}

// resolveAppURL returns the base URL for the target app. Precedence:
//
//  1. --app-url flag if set
//  2. config.yaml's server.port
//  3. PORT env var
//  4. 8080 (final fallback)
//
// The function never errors — even if config.yaml is missing, the
// fallback keeps a bare `gofasta debug` invocation from blowing up
// before it's reached its diagnostic surface. Unreachable apps are
// caught by requireDevtools with a clear DEBUG_APP_UNREACHABLE code.
func resolveAppURL() string {
	if debugAppURL != "" {
		return debugAppURL
	}
	port := configutil.GetPort()
	return "http://localhost:" + port
}

// requireDevtools probes /debug/health and returns:
//
//   - nil                           if devtools is enabled
//   - DEBUG_APP_UNREACHABLE         if the probe couldn't connect / got 5xx
//   - DEBUG_DEVTOOLS_OFF            if the app replied with {"devtools":"stub"}
//
// Every tier-1 command calls this first so agents get a single
// predictable error code instead of an endpoint-specific 404.
func requireDevtools(appURL string) error {
	resp, err := debugClient.Get(appURL + "/debug/health")
	if err != nil {
		return clierr.Wrap(clierr.CodeDebugAppUnreachable, err,
			fmt.Sprintf("could not reach app at %s", appURL))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return clierr.Newf(clierr.CodeDebugAppUnreachable,
			"app responded %d at %s/debug/health", resp.StatusCode, appURL)
	}
	var payload struct {
		Devtools string `json:"devtools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return clierr.Wrap(clierr.CodeDebugAppUnreachable, err,
			"could not parse /debug/health response")
	}
	if payload.Devtools != "enabled" {
		return clierr.New(clierr.CodeDebugDevtoolsOff,
			"app is running without the devtools build tag")
	}
	return nil
}

// getJSON issues a GET against path (relative to the app URL) and
// decodes the response body into out. Returns a wrapped clierr on
// failure so callers can propagate without wrapping further.
func getJSON(appURL, path string, out interface{}) error {
	resp, err := debugClient.Get(appURL + path)
	if err != nil {
		return clierr.Wrap(clierr.CodeDebugAppUnreachable, err,
			fmt.Sprintf("GET %s failed", path))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return clierr.Newf(clierr.CodeDebugTraceNotFound,
			"endpoint %s returned 404 — resource not in ring, or not supported", path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return clierr.Newf(clierr.CodeDebugAppUnreachable,
			"GET %s responded %d: %s", path, resp.StatusCode, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return clierr.Wrap(clierr.CodeDebugAppUnreachable, err,
			fmt.Sprintf("could not decode %s response", path))
	}
	return nil
}

// postJSON issues a POST with a JSON body and decodes the response.
// Used by commands that call /debug/explain.
func postJSON(appURL, path string, in, out interface{}) error {
	body, err := json.Marshal(in)
	if err != nil {
		return clierr.Wrap(clierr.CodeDebugBadFilter, err,
			"could not encode POST body")
	}
	req, err := http.NewRequest(http.MethodPost, appURL+path, bytesReader(body))
	if err != nil {
		return clierr.Wrap(clierr.CodeDebugAppUnreachable, err,
			"could not construct POST request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := debugClient.Do(req)
	if err != nil {
		return clierr.Wrap(clierr.CodeDebugAppUnreachable, err,
			fmt.Sprintf("POST %s failed", path))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return clierr.Newf(clierr.CodeDebugExplainFailed,
			"POST %s responded %d: %s", path, resp.StatusCode, string(b))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return clierr.Wrap(clierr.CodeDebugAppUnreachable, err,
			fmt.Sprintf("could not decode %s response", path))
	}
	return nil
}

// appendQuery builds a URL path with optional query parameters. Skips
// empty values so callers can pass through unset flags freely.
func appendQuery(base string, params map[string]string) string {
	qs := url.Values{}
	for k, v := range params {
		if v == "" {
			continue
		}
		qs.Set(k, v)
	}
	if enc := qs.Encode(); enc != "" {
		return base + "?" + enc
	}
	return base
}

// bytesReader avoids importing bytes in callers that only need a
// []byte → io.Reader conversion.
func bytesReader(b []byte) io.Reader {
	return &byteSliceReader{buf: b}
}

type byteSliceReader struct {
	buf []byte
	pos int
}

func (r *byteSliceReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.buf) {
		return 0, io.EOF
	}
	n := copy(p, r.buf[r.pos:])
	r.pos += n
	return n, nil
}
