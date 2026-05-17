// Package stackresolve parses gofasta-captured stack frames and resolves
// each to a source-context window.
//
// Frame format: the skeleton's app/devtools/devtools_enabled.go.tmpl emits
// frames as "<absolute-file>:<line> <full-function-name>", produced by
// runtime.Callers + runtime.CallersFrames. Example:
//
//	/Users/descholar/proj/app/services/order.service.go:42 irodata/app/services.(*orderService).Archive
//
// Resolve() reads the file at the given line and slices a context window
// around it. Frames whose file is missing (deleted, vendored to a path
// outside cwd, in GOROOT) are returned with Source == nil and External
// set to true rather than erroring.
package stackresolve

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
)

// SourceLine is one numbered line of source captured around a frame.
type SourceLine struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

// SourceWindow is the ±N lines surrounding a stack frame's target line.
type SourceWindow struct {
	Before  []SourceLine `json:"before,omitempty"`
	Current SourceLine   `json:"current"`
	After   []SourceLine `json:"after,omitempty"`
}

// ResolvedFrame is the full resolved form of one captured stack entry.
// Source is nil when the file could not be read (deleted, vendored, or
// outside the current working tree); External flags those cases so
// consumers can render them differently.
type ResolvedFrame struct {
	Raw      string        `json:"raw"`
	File     string        `json:"file"`
	Line     int           `json:"line"`
	Func     string        `json:"func"`
	External bool          `json:"external,omitempty"`
	Source   *SourceWindow `json:"source,omitempty"`
}

// frameRe matches "<path>:<line> <func>". The path is greedy so that the
// last :NUMBER pair wins (covers paths containing colons; on POSIX file
// systems paths never contain colons, but we don't enforce that).
var frameRe = regexp.MustCompile(`^(.+):(\d+) (.+)$`)

// ErrInvalidFrame is returned by ParseFrame when the input does not match
// the expected "<file>:<line> <func>" shape.
var ErrInvalidFrame = errors.New("stack frame does not match \"<file>:<line> <func>\" format")

// ParseFrame splits a single captured frame string into its three parts.
// Returns a clierr-wrapped error with CodeDebugStackParseFailed on malformed
// input so callers can surface a useful hint to the user.
func ParseFrame(raw string) (file string, line int, fn string, err error) {
	raw = strings.TrimSpace(raw)
	m := frameRe.FindStringSubmatch(raw)
	if m == nil {
		return "", 0, "", clierr.Wrapf(clierr.CodeDebugStackParseFailed, ErrInvalidFrame,
			"frame %q", raw)
	}
	n, convErr := strconv.Atoi(m[2])
	if convErr != nil {
		return "", 0, "", clierr.Wrapf(clierr.CodeDebugStackParseFailed, convErr,
			"frame %q: line number not parseable", raw)
	}
	return m[1], n, m[3], nil
}

// Resolve parses raw, reads the referenced file, and returns the frame
// with a SourceWindow of ±contextLines around the target line. If the
// file is missing or unreadable, returns the frame with External=true and
// Source=nil — never errors for "file not in repo" cases (those are
// expected for GOROOT / vendored / deleted source).
//
// Parse errors (malformed frame format) DO return an error.
func Resolve(raw string, contextLines int) (ResolvedFrame, error) {
	file, line, fn, err := ParseFrame(raw)
	if err != nil {
		return ResolvedFrame{Raw: raw}, err
	}
	rf := ResolvedFrame{
		Raw:  raw,
		File: file,
		Line: line,
		Func: fn,
	}

	// Best-effort source read. Failure → External, not an error.
	win, readErr := readSourceWindow(file, line, contextLines)
	if readErr != nil {
		rf.External = true
		return rf, nil
	}
	rf.Source = &win

	// If the file lives outside the current working tree, mark external
	// so consumers know it's framework / dependency / stdlib code.
	if !underCwd(file) {
		rf.External = true
	} else {
		// Convert absolute paths to cwd-relative for clean display.
		if rel := relToCwd(file); rel != "" {
			rf.File = rel
		}
	}
	return rf, nil
}

// ResolveMany is a convenience wrapper for resolving a slice of captured
// frames. Stops at the first malformed frame and returns the resolved
// prefix plus the error. Empty raws are skipped.
func ResolveMany(raws []string, contextLines int) ([]ResolvedFrame, error) {
	out := make([]ResolvedFrame, 0, len(raws))
	for _, raw := range raws {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		rf, err := Resolve(raw, contextLines)
		if err != nil {
			return out, err
		}
		out = append(out, rf)
	}
	return out, nil
}

// ----- internals ---------------------------------------------------------

// readSourceWindow opens path, scans to line N, and returns the
// surrounding window. Returns an error only if the file cannot be read or
// the line is out of range.
func readSourceWindow(path string, line, ctx int) (SourceWindow, error) {
	if ctx < 0 {
		ctx = 0
	}
	f, err := os.Open(path)
	if err != nil {
		return SourceWindow{}, err
	}
	defer func() { _ = f.Close() }()

	target := line
	start := line - ctx
	end := line + ctx

	var (
		win SourceWindow
		sc  = bufio.NewScanner(f)
		cur = 0
	)
	// Allow long lines up to 1 MiB before truncation.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		cur++
		if cur < start {
			continue
		}
		if cur > end {
			break
		}
		sl := SourceLine{Line: cur, Text: sc.Text()}
		switch {
		case cur < target:
			win.Before = append(win.Before, sl)
		case cur == target:
			win.Current = sl
		case cur > target:
			win.After = append(win.After, sl)
		}
	}
	if err := sc.Err(); err != nil {
		return SourceWindow{}, err
	}
	if win.Current.Line == 0 {
		return SourceWindow{}, os.ErrNotExist
	}
	return win, nil
}

// Package-level seams over os.Getwd / filepath.Abs / filepath.Rel so
// tests can inject failures into the defensive branches of underCwd
// and relToCwd. Production code uses the stdlib versions unchanged.
var (
	getwdFn       = os.Getwd
	filepathAbsFn = filepath.Abs
	filepathRelFn = filepath.Rel
)

// underCwd returns true if path is a descendant of the current working dir.
// Used to flag "external" frames (GOROOT, vendored, deps).
func underCwd(path string) bool {
	if !filepath.IsAbs(path) {
		return true
	}
	cwd, err := getwdFn()
	if err != nil {
		return false
	}
	abs, err := filepathAbsFn(path)
	if err != nil {
		return false
	}
	rel, err := filepathRelFn(cwd, abs)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// relToCwd returns path made relative to cwd, or empty if it cannot be
// made relative cleanly.
func relToCwd(path string) string {
	if !filepath.IsAbs(path) {
		return path
	}
	cwd, err := getwdFn()
	if err != nil {
		return ""
	}
	abs, err := filepathAbsFn(path)
	if err != nil {
		return ""
	}
	rel, err := filepathRelFn(cwd, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	return rel
}
