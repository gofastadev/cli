package commands

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/require"
)

func resetDebugReplayFlags() {
	debugReplayMethod = ""
	debugReplayPath = ""
	debugReplayBody = ""
	debugReplayHeaders = nil
	debugReplayStripAuth = false
}

func TestParseHeaderFlags_HappyPath(t *testing.T) {
	got, err := parseHeaderFlags([]string{"X-Foo:bar", "Authorization:Bearer x"})
	require.NoError(t, err)
	require.Equal(t, []string{"bar"}, got["X-Foo"])
	require.Equal(t, []string{"Bearer x"}, got["Authorization"])
}

func TestParseHeaderFlags_RepeatedKeyAppends(t *testing.T) {
	got, err := parseHeaderFlags([]string{"X:a", "X:b"})
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, got["X"])
}

func TestParseHeaderFlags_NilInputReturnsNil(t *testing.T) {
	got, err := parseHeaderFlags(nil)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestParseHeaderFlags_MalformedReturnsCode(t *testing.T) {
	_, err := parseHeaderFlags([]string{"no-colon"})
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeDebugBadFilter), ce.Code)
}

func TestReadBodyFlag_EmptyReturnsEmpty(t *testing.T) {
	got, err := readBodyFlag("")
	require.NoError(t, err)
	require.Equal(t, "", got)
}

func TestReadBodyFlag_InlineTextPassesThrough(t *testing.T) {
	got, err := readBodyFlag(`{"k":"v"}`)
	require.NoError(t, err)
	require.Equal(t, `{"k":"v"}`, got)
}

func TestReadBodyFlag_FileForm(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "payload.json")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

	got, err := readBodyFlag("@" + path)
	require.NoError(t, err)
	require.Equal(t, "hello", got)
}

func TestReadBodyFlag_MissingFileReturnsClierr(t *testing.T) {
	_, err := readBodyFlag("@/absolutely/nowhere/missing.json")
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeFileIO), ce.Code)
}

func TestReadBodyFlag_StdinForm(t *testing.T) {
	// Swap os.Stdin for a pipe with known content, then read back.
	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin })

	go func() {
		_, _ = io.WriteString(w, "stdin payload")
		_ = w.Close()
	}()

	got, err := readBodyFlag("-")
	require.NoError(t, err)
	require.Equal(t, "stdin payload", got)
}

func TestRunDebugReplay_DevtoolsUnreachable(t *testing.T) {
	resetDebugReplayFlags()
	withDebugAppURL(t, "http://127.0.0.1:1")
	err := runDebugReplay("req_1")
	require.Error(t, err) // requireDevtools should fail on unreachable
}

func TestRunDebugReplay_HappyPath(t *testing.T) {
	// Spin up a fake app exposing /debug/health, /debug/requests/{id},
	// and /debug/replay. Run the replay end-to-end.
	original := debugRequestEntry{
		ID:     "req_1",
		Method: "GET",
		Path:   "/orders",
	}
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests/req_1": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, original)
		},
		"/debug/replay": func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "POST", r.Method)
			var payload debugReplayPayload
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			require.Equal(t, "req_1", payload.RequestID)
			writeJSON(w, debugReplayResult{
				NewRequestID: "req_2",
				Status:       204,
				DurationMS:   5,
			})
		},
	})
	withDebugAppURL(t, url)
	resetDebugReplayFlags()
	require.NoError(t, runDebugReplay("req_1"))
}

func TestRunDebugReplay_WithOverridesAndStripAuth(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests/req_1": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, debugRequestEntry{ID: "req_1", Method: "GET", Path: "/x"})
		},
		"/debug/replay": func(w http.ResponseWriter, r *http.Request) {
			var payload debugReplayPayload
			_ = json.NewDecoder(r.Body).Decode(&payload)
			require.Equal(t, "POST", payload.Overrides.Method)
			require.Equal(t, "/y", payload.Overrides.Path)
			require.True(t, payload.Overrides.StripAuth)
			require.Equal(t, []string{"v"}, payload.Overrides.Headers["X-Test"])
			writeJSON(w, debugReplayResult{NewRequestID: "new", Status: 200})
		},
	})
	withDebugAppURL(t, url)
	resetDebugReplayFlags()
	debugReplayMethod = "POST"
	debugReplayPath = "/y"
	debugReplayHeaders = []string{"X-Test:v"}
	debugReplayStripAuth = true
	t.Cleanup(resetDebugReplayFlags)

	require.NoError(t, runDebugReplay("req_1"))
}

func TestRunDebugReplay_BadHeaderFlagFails(t *testing.T) {
	url := debugFixtureAll(t)
	withDebugAppURL(t, url)
	resetDebugReplayFlags()
	debugReplayHeaders = []string{"no-colon"}
	t.Cleanup(resetDebugReplayFlags)

	err := runDebugReplay("req_1")
	require.Error(t, err)
}

func TestRunDebugReplay_MissingRequestPropagates(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/requests/req_404": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]string{
				"code":    "DEBUG_REPLAY_NOT_FOUND",
				"message": "missing",
			})
		},
	})
	withDebugAppURL(t, url)
	resetDebugReplayFlags()
	err := runDebugReplay("req_404")
	require.Error(t, err)
}

func TestPrintReplayText_RenderableShapes(t *testing.T) {
	// Exercise the rendering path for coverage.
	var b strings.Builder
	printReplayText(&b,
		debugRequestEntry{Method: "GET", Path: "/orders", Status: 200, DurationMS: 10},
		debugReplayPayload{
			RequestID: "req_1",
			Overrides: debugReplayOverridePld{
				Method:    "POST",
				Path:      "/x",
				Headers:   map[string][]string{"K": {"v"}},
				Body:      "{\"x\":1}",
				StripAuth: true,
			},
		},
		debugReplayResult{
			NewRequestID: "req_2",
			Status:       204,
			DurationMS:   12,
			ResponseBody: strings.Repeat("a", 500), // exercises truncation
		},
	)
	out := b.String()
	require.Contains(t, out, "Original:")
	require.Contains(t, out, "Replay:")
	require.Contains(t, out, "strip-auth")
	require.Contains(t, out, "method")
}
