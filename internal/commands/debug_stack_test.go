package commands

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/require"
)

func resetDebugStackFlags() {
	debugStackTrace = ""
	debugStackLastError = false
	debugStackContext = 3
}

func TestRunDebugStack_RequiresOneMode(t *testing.T) {
	resetDebugStackFlags()
	err := runDebugStack(false)
	require.Error(t, err)
	require.Equal(t, string(clierr.CodeDebugBadFilter), codeOf(err))
}

func TestRunDebugStack_MutuallyExclusiveModes(t *testing.T) {
	resetDebugStackFlags()
	debugStackTrace = "x"
	debugStackLastError = true
	t.Cleanup(resetDebugStackFlags)
	err := runDebugStack(false)
	require.Error(t, err)
	require.Equal(t, string(clierr.CodeDebugBadFilter), codeOf(err))
}

func TestRunDebugStack_LastError_NoExceptions(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedException{})
		},
	})
	withDebugAppURL(t, url)
	resetDebugStackFlags()
	debugStackLastError = true
	t.Cleanup(resetDebugStackFlags)

	err := runDebugStack(false)
	require.Error(t, err)
	require.Equal(t, string(clierr.CodeDebugTraceNotFound), codeOf(err))
}

func TestRunDebugStack_LastError_ResolvesFrames(t *testing.T) {
	// Create a real file we can frame against, then plumb that frame
	// through a fake /debug/errors response. Resolve should produce a
	// source window for it.
	tmp := t.TempDir()
	src := filepath.Join(tmp, "sample.go")
	require.NoError(t, os.WriteFile(src, []byte("package x\nvar a = 1\nvar b = 2\nvar c = 3\n"), 0o644))
	frame := src + ":3 sample.boom"

	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedException{
				{Recovered: "ka-boom", Stack: []string{frame}},
			})
		},
	})
	withDebugAppURL(t, url)
	resetDebugStackFlags()
	debugStackLastError = true
	debugStackContext = 1
	t.Cleanup(resetDebugStackFlags)

	require.NoError(t, runDebugStack(false))
}

func TestRunDebugStack_Trace_NoSpansWithStacks(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/traces/abc": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, scrapedTrace{
				TraceID:    "abc",
				RootName:   "GET /",
				DurationMS: 10,
				Spans: []scrapedSpan{
					{SpanID: "s1", Name: "no-stack"},
				},
			})
		},
	})
	withDebugAppURL(t, url)
	resetDebugStackFlags()
	debugStackTrace = "abc"
	t.Cleanup(resetDebugStackFlags)

	// Empty stacks should still succeed (no error) and emit the
	// informational text path. Use this to cover the empty-groups branch.
	require.NoError(t, runDebugStack(false))
}

func TestRunDebugStack_Trace_NotFound(t *testing.T) {
	url := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/traces/missing": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, scrapedTrace{}) // TraceID == ""
		},
	})
	withDebugAppURL(t, url)
	resetDebugStackFlags()
	debugStackTrace = "missing"
	t.Cleanup(resetDebugStackFlags)

	err := runDebugStack(false)
	require.Error(t, err)
	require.Equal(t, string(clierr.CodeDebugTraceNotFound), codeOf(err))
}

func TestReadFramesFromReader_FiltersNonFrameLines(t *testing.T) {
	in := strings.NewReader(`
		--- some test runner banner ---
		/a/b.go:10 pkg.Func
		not a frame
		/c/d.go:20 pkg.Other

		`)
	frames, err := readFramesFromReader(in)
	require.NoError(t, err)
	require.Equal(t, 2, len(frames))
}

func TestReadFramesFromReader_NoFrameShapedLinesErrors(t *testing.T) {
	in := strings.NewReader("not a frame\nalso not a frame\n")
	_, err := readFramesFromReader(in)
	require.Error(t, err)
	require.Equal(t, string(clierr.CodeDebugStackParseFailed), codeOf(err))
}

// Sanity check: the helper from migrate_explain_test.go imports clierr,
// so we share `codeOf` from that file. Confirm the type's identity hasn't
// drifted under us.
func TestCodeOf_ReturnsCodeForClierr(t *testing.T) {
	e := clierr.New(clierr.CodeDebugBadFilter, "x")
	require.Equal(t, string(clierr.CodeDebugBadFilter), codeOf(e))
}

func TestCodeOf_ReturnsEmptyForPlainError(t *testing.T) {
	require.Equal(t, "", codeOf(errors.New("plain")))
}
