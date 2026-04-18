package generate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/gofastadev/cli/internal/termcolor"
)

// Planned-action support: when dry-run mode is active, every generator
// and patcher records what it WOULD do on disk instead of actually
// writing. The CLI prints the collected plan at the end of the run,
// giving callers (and AI agents) a preview-before-commit workflow.
//
// The package exposes a tiny control surface — SetDryRun / GetDryRun /
// Plan — that scaffold and generator subcommands toggle based on the
// --dry-run flag. Internal code paths consult GetDryRun() and, when
// true, call recordCreate / recordPatch instead of os.WriteFile.

// PlannedAction is one recorded action. Kind is "create" or "patch";
// Path is the file path relative to the project root; Size is the
// content size in bytes; Diff is an optional short human-readable
// description of the change for patch actions.
type PlannedAction struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Size   int    `json:"size"`
	Detail string `json:"detail,omitempty"`
}

var (
	planMu     sync.Mutex
	planActive bool
	planned    []PlannedAction
)

// SetDryRun turns plan-only mode on or off. When on, every filesystem
// write in the generate package is recorded instead of executed; when
// off, the package operates normally.
func SetDryRun(enabled bool) {
	planMu.Lock()
	defer planMu.Unlock()
	planActive = enabled
	if enabled {
		planned = nil
	}
}

// GetDryRun reports whether dry-run mode is currently active.
func GetDryRun() bool {
	planMu.Lock()
	defer planMu.Unlock()
	return planActive
}

// Plan returns a copy of every recorded action, sorted by path for
// deterministic output. Does not clear the internal buffer — callers
// can call Plan multiple times (e.g., once for --json output, once
// for the human summary) and see the same content.
func Plan() []PlannedAction {
	planMu.Lock()
	defer planMu.Unlock()
	out := make([]PlannedAction, len(planned))
	copy(out, planned)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// recordCreate adds a "create" entry to the plan. Called from
// WriteTemplate's dry-run branch.
func recordCreate(path string, size int) {
	planMu.Lock()
	defer planMu.Unlock()
	planned = append(planned, PlannedAction{
		Kind: "create",
		Path: path,
		Size: size,
	})
}

// recordPatch adds a "patch" entry to the plan. Called from every
// Patch* function's dry-run branch.
func recordPatch(path, detail string, newSize int) {
	planMu.Lock()
	defer planMu.Unlock()
	planned = append(planned, PlannedAction{
		Kind:   "patch",
		Path:   path,
		Size:   newSize,
		Detail: detail,
	})
}

// writeOrRecordCreate is the single chokepoint for file creation. In
// normal mode it writes to disk; in dry-run mode it only records the
// planned action. Every caller should prefer this over os.WriteFile
// directly so dry-run mode stays consistent across the package.
func writeOrRecordCreate(path string, body []byte) error {
	if GetDryRun() {
		recordCreate(path, len(body))
		termcolor.PrintCreate(path + " (dry-run)")
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return err
	}
	termcolor.PrintCreate(path)
	return nil
}

// writeOrRecordPatch is the chokepoint for patched (already-existing)
// files. Detail is a short human-readable description of the change —
// agents see it in --json output.
func writeOrRecordPatch(path, detail string, body []byte) error {
	if GetDryRun() {
		recordPatch(path, detail, len(body))
		termcolor.PrintPatch(path+" (dry-run)", detail)
		return nil
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return err
	}
	termcolor.PrintPatch(path, detail)
	return nil
}

// PrintPlanText writes a human-friendly summary of the recorded plan
// to w. Used by dry-run subcommands for the default (non-JSON) output.
func PrintPlanText(w io.Writer) {
	actions := Plan()
	if len(actions) == 0 {
		_, _ = io.WriteString(w, "No changes would be made.\n")
		return
	}
	created, patched := 0, 0
	for _, a := range actions {
		switch a.Kind {
		case "create":
			created++
		case "patch":
			patched++
		}
	}
	header := fmt.Sprintf("Dry run — %d create, %d patch\n\n", created, patched)
	_, _ = io.WriteString(w, header)

	_, _ = io.WriteString(w, "Files to create:\n")
	for _, a := range actions {
		if a.Kind != "create" {
			continue
		}
		_, _ = fmt.Fprintf(w, "  + %s (%s)\n", a.Path, humanSize(a.Size))
	}

	// Only emit the "patch" block when at least one patch is planned.
	hasPatch := false
	for _, a := range actions {
		if a.Kind == "patch" {
			hasPatch = true
			break
		}
	}
	if hasPatch {
		_, _ = io.WriteString(w, "\nFiles to patch:\n")
		for _, a := range actions {
			if a.Kind != "patch" {
				continue
			}
			detail := a.Detail
			if detail == "" {
				detail = "in-place edit"
			}
			_, _ = fmt.Fprintf(w, "  ~ %s — %s\n", a.Path, detail)
		}
	}

	_, _ = io.WriteString(w, "\nNo files were written. Re-run without --dry-run to apply.\n")
}

// humanSize renders a byte count as "340 B" / "4.2 KB" for the plan
// summary. Kept tiny — the plan typically shows sub-10KB files.
func humanSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	default:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
}

// describePatch returns a stable short string describing which fragment
// a Patch* function is about to inject into a file. Used as the "detail"
// field on planned patch actions so agents see what would change without
// having to diff bytes.
func describePatch(fragments ...string) string {
	trimmed := make([]string, 0, len(fragments))
	for _, f := range fragments {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		// Collapse newlines so the detail stays on one line.
		f = strings.ReplaceAll(f, "\n", " ")
		if len(f) > 60 {
			f = f[:57] + "..."
		}
		trimmed = append(trimmed, f)
	}
	return strings.Join(trimmed, " + ")
}
