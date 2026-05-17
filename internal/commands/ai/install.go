package ai

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"text/template"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/termcolor"
)

// InstallData is the template payload — every .tmpl file under an
// agent's template directory gets this struct as its context. Keep
// field names stable so existing templates don't break when new fields
// are added.
type InstallData struct {
	ProjectName      string
	ProjectNameLower string
	ProjectNameUpper string
	ModulePath       string
	CLIVersion       string
}

// InstallResult summarizes one `gofasta ai <agent>` invocation. Files
// are categorized so the output table shows "created 3 new, skipped 2
// unchanged, would overwrite 1" instead of just a total count.
//
// Renamed/WouldRename track the doc-file rename (e.g. AGENTS.md → CLAUDE.md)
// performed for agents that don't read AGENTS.md natively. Each entry
// is the human-readable form "<src> → <dst>".
type InstallResult struct {
	Agent        string   `json:"agent"`
	Created      []string `json:"created"`
	Skipped      []string `json:"skipped"`
	WouldReplace []string `json:"would_replace"`
	Replaced     []string `json:"replaced"`
	Renamed      []string `json:"renamed,omitempty"`
	WouldRename  []string `json:"would_rename,omitempty"`

	// renameFrom / renameTo are internal bookkeeping for the manifest —
	// not serialized. Set when a rename actually happened (or would have
	// in dry-run). Empty when the agent reads AGENTS.md natively.
	renameFrom string `json:"-"`
	renameTo   string `json:"-"`
}

// InstallOptions tunes the behavior of Install. In --dry-run mode no
// files are written and WouldReplace holds the would-be-overwritten
// files. In --force mode existing files are overwritten without prompts.
type InstallOptions struct {
	DryRun bool
	Force  bool
}

// Install renders every template for agent into the project rooted at
// projectRoot, honoring opts. The returned *InstallResult describes
// which files were created/skipped/replaced/renamed so callers can
// render either a human table or a JSON payload.
//
// For agents that don't read AGENTS.md natively (Agent.DocFilename is
// non-empty), Install first renames AGENTS.md → DocFilename. The rename
// is non-destructive: if both files exist it errors; if only DocFilename
// exists it's treated as already-renamed (idempotent re-install).
//
// Idempotency rule: a file that already exists on disk with byte-for-byte
// identical contents is recorded as Skipped (no-op). A file that exists
// with different contents is recorded as WouldReplace (dry-run) or
// Replaced (when --force). Without --force, an existing-and-different
// file halts the install with a clierr.
func Install(agent *Agent, projectRoot string, data InstallData, opts InstallOptions) (*InstallResult, error) {
	files, err := TemplateFiles(agent)
	if err != nil {
		return nil, clierr.Wrap(clierr.CodeAIInstallFailed, err,
			"could not enumerate templates for agent "+agent.Key)
	}

	result := &InstallResult{Agent: agent.Key}

	if err := renameDocFile(agent, projectRoot, opts, result); err != nil {
		return nil, err
	}

	for _, tf := range files {
		rendered, err := renderTemplate(tf.SourcePath, data)
		if err != nil {
			return nil, clierr.Wrapf(clierr.CodeAIInstallFailed, err,
				"render %s", tf.SourcePath)
		}
		destAbs := filepath.Join(projectRoot, tf.DestPath)

		existing, err := os.ReadFile(destAbs)
		switch {
		case err == nil && bytes.Equal(existing, rendered):
			// File exists with identical content — no-op.
			result.Skipped = append(result.Skipped, tf.DestPath)
			continue
		case err == nil && !opts.Force:
			// File exists with different content and we're not forcing.
			// In dry-run, record it; otherwise halt so the user decides.
			result.WouldReplace = append(result.WouldReplace, tf.DestPath)
			if !opts.DryRun {
				return result, clierr.Newf(clierr.CodeAIInstallFailed,
					"%s already exists and differs from the template; pass --force to overwrite or edit the file to resolve",
					tf.DestPath)
			}
			continue
		case err == nil && opts.Force:
			// Force-replace.
			result.Replaced = append(result.Replaced, tf.DestPath)
		case os.IsNotExist(err):
			result.Created = append(result.Created, tf.DestPath)
		default:
			return nil, clierr.Wrapf(clierr.CodeAIInstallFailed, err,
				"stat %s", destAbs)
		}

		if opts.DryRun {
			continue
		}
		if err := writeFile(destAbs, rendered); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// osRename is a package-level seam for os.Rename so tests can force a
// rename failure (e.g. simulate cross-device or permission errors).
var osRename = os.Rename

// adaptDocFn / restoreDocFn are package-level seams for the docadapt
// content transforms so tests can inject a failure into the
// install / uninstall flows that wrap them. Production callers see
// AdaptDocFileContent / RestoreDocFileContent's normal behavior.
var (
	adaptDocFn   = AdaptDocFileContent
	restoreDocFn = RestoreDocFileContent
)

// renameDocFile handles the AGENTS.md → agent.DocFilename rename for
// agents that don't read AGENTS.md natively. It records the outcome on
// the result so the caller (and the manifest) can reverse it later.
//
// Rules:
//   - agent.DocFilename empty → no-op (agent reads AGENTS.md natively).
//   - both AGENTS.md and DocFilename present → error (ambiguous; user
//     must resolve which one wins before re-running).
//   - only AGENTS.md present → rename. Records renameFrom/renameTo.
//   - only DocFilename present → treat as already-renamed; record the
//     rename in the manifest so uninstall can reverse it.
//   - neither present → no-op (project was generated without AGENTS.md).
func renameDocFile(agent *Agent, projectRoot string, opts InstallOptions, result *InstallResult) error {
	if agent.DocFilename == "" {
		return nil
	}
	srcAbs := filepath.Join(projectRoot, "AGENTS.md")
	dstAbs := filepath.Join(projectRoot, agent.DocFilename)

	srcExists := fileExists(srcAbs)
	dstExists := fileExists(dstAbs)

	switch {
	case srcExists && dstExists:
		return clierr.Newf(clierr.CodeAIInstallFailed,
			"both AGENTS.md and %s exist at the project root; remove one (or merge their content) before installing %s",
			agent.DocFilename, agent.Key)
	case srcExists && !dstExists:
		entry := "AGENTS.md → " + agent.DocFilename
		result.renameFrom = "AGENTS.md"
		result.renameTo = agent.DocFilename
		if opts.DryRun {
			result.WouldRename = append(result.WouldRename, entry)
			return nil
		}
		if err := osRename(srcAbs, dstAbs); err != nil {
			return clierr.Wrapf(clierr.CodeAIInstallFailed, err,
				"rename AGENTS.md → %s", agent.DocFilename)
		}
		// Adapt the renamed file's title + intro paragraph to the
		// installed agent. A plain os.Rename leaves the H1 still
		// titled "# AGENTS.md — Guidance for AI coding agents", which
		// is incongruent with the new filename and the agent the user
		// chose. AdaptDocFileContent is exact-string-match-based, so
		// hand-edited files round-trip cleanly through uninstall.
		// Routed through adaptDocFn so tests can inject a failure.
		if err := adaptDocFn(dstAbs, agent); err != nil {
			return err
		}
		result.Renamed = append(result.Renamed, entry)
	case !srcExists && dstExists:
		// Already-renamed (likely an idempotent re-install). Record so
		// the manifest still knows how to reverse it on uninstall.
		result.renameFrom = "AGENTS.md"
		result.renameTo = agent.DocFilename
	}
	return nil
}

// fileExists reports whether path refers to an existing regular file
// (not a directory). Errors other than IsNotExist are treated as
// "exists" so the caller's downstream Stat/ReadFile surfaces the real
// problem with better context.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return !os.IsNotExist(err)
	}
	return !info.IsDir()
}

// templateParse is a package-level seam for template.New().Parse so
// tests can force a parse error on an otherwise-valid source. Every
// shipped template parses; without a seam the error branch would be
// unreachable.
var templateParse = func(sourcePath string, raw []byte) (*template.Template, error) {
	return template.New(filepath.Base(sourcePath)).Parse(string(raw))
}

// renderTemplate reads the embedded template and executes it with data.
// Uses text/template (not html/template) — we're producing config files
// and shell scripts, not HTML.
func renderTemplate(sourcePath string, data InstallData) ([]byte, error) {
	raw, err := ReadTemplate(sourcePath)
	if err != nil {
		return nil, err
	}
	tmpl, err := templateParse(sourcePath, raw)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeFile creates destAbs and any parent directories, then writes body.
// Shell scripts (pre-commit.sh) need to be executable — detect by suffix
// and set mode accordingly.
func writeFile(destAbs string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(destAbs), 0o755); err != nil {
		return clierr.Wrap(clierr.CodeAIInstallFailed, err,
			"could not create parent directory")
	}
	mode := os.FileMode(0o644)
	if filepathHasSuffix(destAbs, ".sh") {
		mode = 0o755
	}
	if err := os.WriteFile(destAbs, body, mode); err != nil {
		return clierr.Wrapf(clierr.CodeAIInstallFailed, err,
			"write %s", destAbs)
	}
	return nil
}

// PrintText renders an InstallResult as a human-friendly summary. Used
// only when cliout.JSON() is false — JSON mode emits the struct directly.
//
// Uses the canonical termcolor vocabulary so output is consistent with
// the rest of the CLI: green ✓ for created/renamed, yellow ~ for
// replaced/dry-run, dim - for unchanged.
func (r *InstallResult) PrintText(w io.Writer) {
	for _, f := range r.Renamed {
		fprintln(w, "  "+termcolor.Success("renamed: %s", f))
	}
	for _, f := range r.WouldRename {
		fprintln(w, "  "+termcolor.Warn("would rename (dry-run): %s", f))
	}
	for _, f := range r.Created {
		fprintln(w, "  "+termcolor.Success("created: %s", f))
	}
	for _, f := range r.Replaced {
		fprintln(w, "  "+termcolor.Warn("replaced: %s", f))
	}
	for _, f := range r.WouldReplace {
		fprintln(w, "  "+termcolor.Warn("would replace (dry-run): %s", f))
	}
	if len(r.Skipped) > 0 {
		fprintln(w, "  "+termcolor.Info("%d file(s) unchanged (skipped)", len(r.Skipped)))
	}
}
