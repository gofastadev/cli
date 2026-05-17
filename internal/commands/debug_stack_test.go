package commands

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/commands/stackresolve"
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

// TestRunDebugStack_Trace_WithSpansHavingStacks — span has captured
// frames; the rendering branch fires (otherwise covered by
// NoSpansWithStacks).
func TestRunDebugStack_Trace_WithSpansHavingStacks(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "sample.go")
	require.NoError(t, os.WriteFile(src, []byte("package x\nvar a = 1\nvar b = 2\nvar c = 3\n"), 0o644))
	frame := src + ":2 sample.fn"

	u := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/traces/abc": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, scrapedTrace{
				TraceID:    "abc",
				RootName:   "GET /",
				DurationMS: 10,
				Spans: []scrapedSpan{
					{SpanID: "s1", Name: "handler", DurationMS: 5, Stack: []string{frame}},
				},
			})
		},
	})
	withDebugAppURL(t, u)
	resetDebugStackFlags()
	debugStackTrace = "abc"
	debugStackContext = 1
	t.Cleanup(resetDebugStackFlags)

	require.NoError(t, runDebugStack(false))
}

// TestRunDebugStack_LastError_WithTraceID — exception carries a
// TraceID; the rendering branch that prints "trace:" fires.
func TestRunDebugStack_LastError_WithTraceID(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "sample.go")
	require.NoError(t, os.WriteFile(src, []byte("package x\nvar a = 1\nvar b = 2\n"), 0o644))
	frame := src + ":2 sample.boom"

	u := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedException{
				{Recovered: "ka-boom", TraceID: "01HXYZ", Stack: []string{frame}},
			})
		},
	})
	withDebugAppURL(t, u)
	resetDebugStackFlags()
	debugStackLastError = true
	debugStackContext = 0
	t.Cleanup(resetDebugStackFlags)

	require.NoError(t, runDebugStack(false))
}

// TestRunDebugStackStdin_ResolvesFrames — feed real frames via a pipe
// swapped into os.Stdin, then call runDebugStackStdin directly.
// Covers the entire function (previously 0% — no test ever called it).
func TestRunDebugStackStdin_ResolvesFrames(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "sample.go")
	require.NoError(t, os.WriteFile(src, []byte("package x\nvar a = 1\n"), 0o644))
	input := src + ":2 sample.fn\n"

	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, _ = w.WriteString(input)
	require.NoError(t, w.Close())

	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin; _ = r.Close() })

	resetDebugStackFlags()
	debugStackContext = 0
	t.Cleanup(resetDebugStackFlags)

	require.NoError(t, runDebugStackStdin())
}

// TestRunDebugStack_StdinDispatches — the top-level runDebugStack
// must route `fromStdin=true` to runDebugStackStdin (covers
// `if fromStdin { return runDebugStackStdin() }` at line 115).
func TestRunDebugStack_StdinDispatches(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "sample.go")
	require.NoError(t, os.WriteFile(src, []byte("package x\nvar a = 1\n"), 0o644))

	r, w, _ := os.Pipe()
	_, _ = w.WriteString(src + ":2 sample.fn\n")
	_ = w.Close()
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin })

	resetDebugStackFlags()
	t.Cleanup(resetDebugStackFlags)

	require.NoError(t, runDebugStack(true))
}

// TestTrimLine_Truncates — input longer than n returns prefix + "…".
func TestTrimLine_Truncates(t *testing.T) {
	got := trimLine("This is a fairly long sentence that needs trimming", 20)
	require.Len(t, []rune(got), 20)
	require.Equal(t, '…', []rune(got)[19])
}

// TestPadLine — left-pads the number with spaces to the requested
// width. Covers the entire 0%-coverage function.
func TestPadLine_Padding(t *testing.T) {
	require.Equal(t, "   5", padLine(5, 4))
	require.Equal(t, "  42", padLine(42, 4))
	require.Equal(t, "12345", padLine(12345, 4)) // already wider — no pad
}

// TestRenderResolvedFrames_AllBranches — exercise every branch of
// the frame-rendering loop: external frame, frame with Source nil,
// frame with Before/Current/After source-context windows.
func TestRenderResolvedFrames_AllBranches(t *testing.T) {
	var buf bytes.Buffer
	frames := []stackresolve.ResolvedFrame{
		// External frame (e.g. GOROOT) — "external — source not in working tree"
		{File: "/usr/local/go/src/net/http.go", Line: 100, Func: "net/http.HandleFunc", External: true},
		// Frame with no source (resolved external-ish — Source nil branch)
		{File: "foo.go", Line: 1, Func: "x.Y", External: false, Source: nil},
		// Frame with Before/Current/After
		{
			File: "bar.go", Line: 42, Func: "bar.Run", External: false,
			Source: &stackresolve.SourceWindow{
				Before:  []stackresolve.SourceLine{{Line: 41, Text: "if order.Status == \"archived\" {"}},
				Current: stackresolve.SourceLine{Line: 42, Text: "return ErrAlreadyArchived"},
				After:   []stackresolve.SourceLine{{Line: 43, Text: "}"}},
			},
		},
	}
	renderResolvedFrames(&buf, frames)
	out := buf.String()
	require.Contains(t, out, "external — source not in working tree")
	require.Contains(t, out, "bar.go:42")
	require.Contains(t, out, "ErrAlreadyArchived")
	require.Contains(t, out, "41")
	require.Contains(t, out, "43")
}

// TestReadFramesFromReader_ScannerError — scanner.Err() returns the
// io error injected by errReader. Covers the wrap-and-return branch.
func TestReadFramesFromReader_ScannerError(t *testing.T) {
	_, err := readFramesFromReader(scannerErrReader{})
	require.Error(t, err)
}

// scannerErrReader returns a non-EOF error on Read so bufio.Scanner's
// internal err propagates through readFramesFromReader's sc.Err().
type scannerErrReader struct{}

func (scannerErrReader) Read(_ []byte) (int, error) { return 0, errors.New("io boom") }

// TestDebugStackCmd_RunE_StdinDash — invokes the cobra command with
// "-" as the positional arg so the for-loop in RunE flips
// fromStdin=true. Covers the RunE wrapper.
func TestDebugStackCmd_RunE_StdinDash(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "sample.go")
	require.NoError(t, os.WriteFile(src, []byte("package x\nvar a = 1\n"), 0o644))

	r, w, _ := os.Pipe()
	_, _ = w.WriteString(src + ":2 sample.fn\n")
	_ = w.Close()
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin })

	resetDebugStackFlags()
	t.Cleanup(resetDebugStackFlags)
	require.NoError(t, debugStackCmd.RunE(debugStackCmd, []string{"-"}))
}

// TestRunDebugStack_DevtoolsProbeFails — requireDevtools fails when
// the app URL doesn't respond. Covers the `if err := requireDevtools
// ...; err != nil { return err }` branch.
func TestRunDebugStack_DevtoolsProbeFails(t *testing.T) {
	// Point at an unreachable URL.
	withDebugAppURL(t, "http://127.0.0.1:1")
	resetDebugStackFlags()
	debugStackLastError = true
	t.Cleanup(resetDebugStackFlags)
	err := runDebugStack(false)
	require.Error(t, err)
}

// TestRunDebugStackLastError_GetJSONFails — server returns malformed
// JSON; getJSON errors and the func surfaces it. Covers line 149-151.
func TestRunDebugStackLastError_GetJSONFails(t *testing.T) {
	u := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not-json"))
		},
	})
	withDebugAppURL(t, u)
	resetDebugStackFlags()
	debugStackLastError = true
	t.Cleanup(resetDebugStackFlags)
	err := runDebugStack(false)
	require.Error(t, err)
}

// TestRunDebugStackTrace_GetJSONFails — server returns malformed
// JSON for the trace; getJSON errors. Covers line 179-181.
func TestRunDebugStackTrace_GetJSONFails(t *testing.T) {
	u := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/traces/xyz": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not-json"))
		},
	})
	withDebugAppURL(t, u)
	resetDebugStackFlags()
	debugStackTrace = "xyz"
	t.Cleanup(resetDebugStackFlags)
	err := runDebugStack(false)
	require.Error(t, err)
}

// TestRunDebugStackStdin_ReadFramesError — empty stdin yields no
// frame-shaped lines; readFramesFromReader returns
// CodeDebugStackParseFailed and runDebugStackStdin surfaces it.
func TestRunDebugStackStdin_ReadFramesError(t *testing.T) {
	r, w, _ := os.Pipe()
	_ = w.Close()
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin })

	resetDebugStackFlags()
	t.Cleanup(resetDebugStackFlags)
	err := runDebugStackStdin()
	require.Error(t, err)
	require.Equal(t, string(clierr.CodeDebugStackParseFailed), codeOf(err))
}

// TestRunDebugStackStdin_ResolveManyError — feed a line that survives
// readFramesFromReader's filter (has a colon and a space) but fails
// stackresolve.ParseFrame's regex match. ResolveMany returns the parse
// error and runDebugStackStdin surfaces it.
func TestRunDebugStackStdin_ResolveManyError(t *testing.T) {
	r, w, _ := os.Pipe()
	// "abc: def" passes the readFramesFromReader filter but has no
	// digits after the colon, so stackresolve.ParseFrame's regex fails.
	_, _ = w.WriteString("abc: def\n")
	_ = w.Close()
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin })

	resetDebugStackFlags()
	t.Cleanup(resetDebugStackFlags)
	err := runDebugStackStdin()
	require.Error(t, err)
}

// TestRunDebugStack_LastError_ResolveManyError — exception's stack
// contains a malformed frame; ResolveMany fails inside
// runDebugStackLastError. Covers line 158-160.
func TestRunDebugStack_LastError_ResolveManyError(t *testing.T) {
	u := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/errors": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, []scrapedException{
				{Recovered: "ka-boom", Stack: []string{"abc: def"}},
			})
		},
	})
	withDebugAppURL(t, u)
	resetDebugStackFlags()
	debugStackLastError = true
	t.Cleanup(resetDebugStackFlags)
	err := runDebugStack(false)
	require.Error(t, err)
}

// TestRunDebugStack_Trace_ResolveManyError — span has a malformed
// frame; ResolveMany fails inside runDebugStackTrace's per-span loop.
// Covers line 192-194.
func TestRunDebugStack_Trace_ResolveManyError(t *testing.T) {
	u := debugFixture(t, map[string]http.HandlerFunc{
		"/debug/traces/abc": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, scrapedTrace{
				TraceID:    "abc",
				RootName:   "GET /",
				DurationMS: 10,
				Spans: []scrapedSpan{
					{SpanID: "s1", Name: "handler", DurationMS: 5, Stack: []string{"abc: def"}},
				},
			})
		},
	})
	withDebugAppURL(t, u)
	resetDebugStackFlags()
	debugStackTrace = "abc"
	t.Cleanup(resetDebugStackFlags)
	err := runDebugStack(false)
	require.Error(t, err)
}
