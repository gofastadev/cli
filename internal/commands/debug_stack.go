package commands

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/commands/stackresolve"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var (
	debugStackTrace     string
	debugStackLastError bool
	debugStackContext   int
)

// debugStackCmd is the "show me what those frames were doing" companion
// to debug last-error and debug trace --with-stacks. Captured stacks are
// stored as "file:line function" strings; this command reads each file
// and slices out a context window around the target line so the agent
// (or human) can see what code was running without grep + open-in-editor.
var debugStackCmd = &cobra.Command{
	Use:   "stack",
	Short: "Resolve captured stack frames to source-context windows",
	Long: `Three input modes:

  --trace=<id>      fetch /debug/traces/{id} and resolve every span's stack
  --last-error      fetch /debug/errors, take the newest, resolve its stack
  -                 read raw stack from stdin (one "file:line function" per line)

The frame format matches what the skeleton's devtools package captures —
runtime.Callers + runtime.CallersFrames produce strings like

  /Users/x/proj/app/services/order.service.go:42 irodata/app/services.(*orderService).Archive

For each frame the command reads the named file and shows ±N source
lines around the target line. Frames whose source file is missing
(deleted, vendored, GOROOT) are returned with external=true and no
source — never errors out a whole resolve run.

Examples:

  gofasta debug stack --last-error
  gofasta debug stack --trace=01HXYZ --context=5
  gofasta debug stack --trace=01HXYZ --json | jq '.frames[] | select(.external == false)'
  go test ./... 2>&1 | gofasta debug stack -`,
	RunE: func(_ *cobra.Command, args []string) error {
		// Detect "-" as positional arg = read from stdin.
		fromStdin := false
		for _, a := range args {
			if a == "-" {
				fromStdin = true
			}
		}
		return runDebugStack(fromStdin)
	},
}

func init() {
	debugStackCmd.Flags().StringVar(&debugStackTrace, "trace", "",
		"Trace ID — resolve every span's captured stack")
	debugStackCmd.Flags().BoolVar(&debugStackLastError, "last-error", false,
		"Resolve the most recent recovered panic's stack")
	debugStackCmd.Flags().IntVar(&debugStackContext, "context", 3,
		"Source-context window size (lines before/after the target)")
	debugCmd.AddCommand(debugStackCmd)
}

// debugStackResult is the JSON envelope. Source is the union of every
// resolved frame across all stacks fed in (a trace yields one stack per
// span, a panic yields one stack; the envelope flattens them and tags
// each group via Source).
type debugStackResult struct {
	Source string                       `json:"source"`
	Groups []debugStackGroup            `json:"groups,omitempty"`
	Frames []stackresolve.ResolvedFrame `json:"frames,omitempty"`
}

// debugStackGroup is one captured stack with a label identifying where it
// came from (a span name, "panic", or "stdin"). Used when more than one
// stack is being resolved in one invocation.
type debugStackGroup struct {
	Label  string                       `json:"label"`
	Frames []stackresolve.ResolvedFrame `json:"frames"`
}

func runDebugStack(fromStdin bool) error {
	// Mode validation: exactly one of --trace, --last-error, or stdin.
	modes := 0
	if debugStackTrace != "" {
		modes++
	}
	if debugStackLastError {
		modes++
	}
	if fromStdin {
		modes++
	}
	if modes == 0 {
		return clierr.New(clierr.CodeDebugBadFilter,
			"pick one input mode: --trace=<id>, --last-error, or `-` for stdin")
	}
	if modes > 1 {
		return clierr.New(clierr.CodeDebugBadFilter,
			"--trace, --last-error, and stdin are mutually exclusive")
	}

	if fromStdin {
		return runDebugStackStdin()
	}

	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}

	if debugStackLastError {
		return runDebugStackLastError(appURL)
	}
	return runDebugStackTrace(appURL, debugStackTrace)
}

func runDebugStackStdin() error {
	frames, err := readFramesFromReader(os.Stdin)
	if err != nil {
		return err
	}
	resolved, err := stackresolve.ResolveMany(frames, debugStackContext)
	if err != nil {
		return err
	}
	result := debugStackResult{Source: "stdin", Frames: resolved}
	cliout.Print(result, func(w io.Writer) {
		_, _ = fmt.Fprintln(w, "Source: stdin")
		renderResolvedFrames(w, resolved)
	})
	return nil
}

func runDebugStackLastError(appURL string) error {
	var exceptions []scrapedException
	if err := getJSON(appURL, "/debug/errors", &exceptions); err != nil {
		return err
	}
	if len(exceptions) == 0 {
		return clierr.New(clierr.CodeDebugTraceNotFound,
			"no captured exceptions — trigger one then re-run")
	}
	ex := exceptions[0]
	resolved, err := stackresolve.ResolveMany(ex.Stack, debugStackContext)
	if err != nil {
		return err
	}
	label := fmt.Sprintf("panic: %s", trimLine(ex.Recovered, 80))
	result := debugStackResult{
		Source: "last-error",
		Groups: []debugStackGroup{{Label: label, Frames: resolved}},
	}
	cliout.Print(result, func(w io.Writer) {
		_, _ = fmt.Fprintln(w, termcolor.CRed(label))
		if ex.TraceID != "" {
			_, _ = fmt.Fprintln(w, "trace:", ex.TraceID)
		}
		_, _ = fmt.Fprintln(w)
		renderResolvedFrames(w, resolved)
	})
	return nil
}

func runDebugStackTrace(appURL, traceID string) error {
	var tr scrapedTrace
	if err := getJSON(appURL, "/debug/traces/"+url.PathEscape(traceID), &tr); err != nil {
		return err
	}
	if tr.TraceID == "" {
		return clierr.Newf(clierr.CodeDebugTraceNotFound,
			"trace %q not found in capture ring", traceID)
	}
	result := debugStackResult{Source: "trace " + tr.TraceID}
	for _, sp := range tr.Spans {
		if len(sp.Stack) == 0 {
			continue
		}
		resolved, err := stackresolve.ResolveMany(sp.Stack, debugStackContext)
		if err != nil {
			return err
		}
		result.Groups = append(result.Groups, debugStackGroup{
			Label:  fmt.Sprintf("span: %s (%dms)", sp.Name, sp.DurationMS),
			Frames: resolved,
		})
	}
	if len(result.Groups) == 0 {
		// JSON callers still want a deterministic envelope even when no
		// stacks were attached — emit the empty result and inform the
		// human caller.
		cliout.Print(result, func(w io.Writer) {
			_, _ = fmt.Fprintln(w, "Trace had no spans with captured stacks. "+
				"Rebuild under `gofasta dev` (devtools tag) to enable stack capture.")
		})
		return nil
	}
	cliout.Print(result, func(w io.Writer) {
		_, _ = fmt.Fprintf(w, "Trace: %s — %s (%dms)\n\n", tr.TraceID, tr.RootName, tr.DurationMS)
		for _, g := range result.Groups {
			_, _ = fmt.Fprintln(w, termcolor.CBrand(g.Label))
			renderResolvedFrames(w, g.Frames)
			_, _ = fmt.Fprintln(w)
		}
	})
	return nil
}

// ----- io helpers --------------------------------------------------------

// readFramesFromReader pulls one frame per line from r, skipping blank
// lines. We don't enforce the "file:line function" shape here — that's
// stackresolve's job — but we do filter out obviously-non-frame text
// (e.g. test-runner banner lines) by requiring a colon and a space.
func readFramesFromReader(r io.Reader) ([]string, error) {
	var frames []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if !strings.Contains(line, ":") || !strings.Contains(line, " ") {
			continue
		}
		frames = append(frames, line)
	}
	if err := sc.Err(); err != nil {
		return nil, clierr.Wrap(clierr.CodeFileIO, err, "reading stdin")
	}
	if len(frames) == 0 {
		return nil, clierr.New(clierr.CodeDebugStackParseFailed,
			"stdin contained no frame-shaped lines (expected `file:line function`)")
	}
	return frames, nil
}

func trimLine(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// renderResolvedFrames prints one frame per stanza:
//
//	#0 app/services/order.service.go:42 — irodata/app/services.(*orderService).Archive
//	     41 |  if order.Status == "archived" {
//	  →  42 |    return ErrAlreadyArchived
//	     43 |  }
func renderResolvedFrames(w io.Writer, frames []stackresolve.ResolvedFrame) {
	for i, f := range frames {
		_, _ = fmt.Fprintf(w, "%s %s:%d — %s\n",
			termcolor.CDim(fmt.Sprintf("#%d", i)),
			f.File, f.Line, f.Func)
		if f.External {
			_, _ = fmt.Fprintln(w, "  "+termcolor.CDim("(external — source not in working tree)"))
			continue
		}
		if f.Source == nil {
			continue
		}
		for _, l := range f.Source.Before {
			_, _ = fmt.Fprintf(w, "    %s | %s\n", padLine(l.Line, 4), l.Text)
		}
		_, _ = fmt.Fprintf(w, "  %s %s | %s\n",
			termcolor.CRed("→"), padLine(f.Source.Current.Line, 4),
			termcolor.CBold(f.Source.Current.Text))
		for _, l := range f.Source.After {
			_, _ = fmt.Fprintf(w, "    %s | %s\n", padLine(l.Line, 4), l.Text)
		}
		_, _ = fmt.Fprintln(w)
	}
}

// padLine is parameterized on width even though every caller passes 4;
// the test suite exercises other widths and keeping the param leaves room
// for renderers that want different gutter widths.
//
//nolint:unparam // width is intentionally configurable; see comment above.
func padLine(n, width int) string {
	s := fmt.Sprintf("%d", n)
	for len(s) < width {
		s = " " + s
	}
	return s
}
