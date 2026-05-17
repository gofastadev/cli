package ai

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

// uninstallCmd is `gofasta ai uninstall <agent>`. Removes everything an
// install added (per the manifest's CreatedFiles + RenamedFrom/To)
// while preserving files the user has modified since install.
var uninstallCmd = &cobra.Command{
	Use:   "uninstall <agent>",
	Short: "Remove an installed AI agent's configuration from this project",
	Long: `Remove the files an earlier ` + "`gofasta ai <agent>`" + ` installed.

Reverses the doc-file rename (e.g. CLAUDE.md → AGENTS.md) and removes
every file recorded in the manifest. Files you've modified since install
are preserved and reported — never silently overwritten.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUninstall(args[0], uninstallDryRun)
	},
}

var uninstallDryRun bool

func init() {
	uninstallCmd.Flags().BoolVar(&uninstallDryRun, "dry-run", false,
		"Preview what would be removed without touching disk")
	Cmd.AddCommand(uninstallCmd)
}

// UninstallResult mirrors InstallResult so cliout.Print routes output
// the same way (text by default, JSON under --json).
type UninstallResult struct {
	Agent     string   `json:"agent"`
	Removed   []string `json:"removed"`
	Preserved []string `json:"preserved"`           // locally-modified, kept on disk
	Renamed   []string `json:"renamed,omitempty"`   // e.g. ["CLAUDE.md → AGENTS.md"]
	NotFound  []string `json:"not_found,omitempty"` // recorded in manifest but already gone
}

// PrintText renders an UninstallResult as a human-friendly summary,
// using the canonical termcolor vocabulary.
func (r *UninstallResult) PrintText(w io.Writer) {
	for _, f := range r.Renamed {
		fprintln(w, "  "+termcolor.Success("renamed: %s", f))
	}
	for _, f := range r.Removed {
		fprintln(w, "  "+termcolor.Success("removed: %s", f))
	}
	for _, f := range r.Preserved {
		fprintln(w, "  "+termcolor.Warn("preserved (locally modified): %s", f))
	}
	for _, f := range r.NotFound {
		fprintln(w, "  "+termcolor.Info("already gone: %s", f))
	}
}

// runUninstall is the entry point for `gofasta ai uninstall <agent>`.
// Looks up the manifest entry, removes the files, reverses the rename,
// and saves an updated manifest.
func runUninstall(key string, dryRun bool) error {
	agent := AgentByKey(key)
	if agent == nil {
		return clierr.Newf(clierr.CodeUnknownAgent,
			"unknown agent %q — run `gofasta ai list` to see supported agents", key)
	}

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	m, err := LoadManifest(root)
	if err != nil {
		return err
	}

	rec, ok := m.Installed[key]
	if !ok {
		return clierr.Newf(clierr.CodeAIInstallFailed,
			"%s is not currently installed in this project (run `gofasta ai status` to see installed agents)",
			agent.Name)
	}

	data, err := buildInstallData(root)
	if err != nil {
		return err
	}

	result, err := Uninstall(agent, root, rec, data, UninstallOptions{DryRun: dryRun})
	if err != nil {
		return err
	}

	if !dryRun {
		m.RecordUninstall(key)
		if err := m.Save(root); err != nil {
			return err
		}
	}

	cliout.Print(result, func(w io.Writer) {
		if dryRun {
			fprintln(w, termcolor.Step("Dry run: %s would be uninstalled from %s", agent.Name, root))
		} else {
			fprintln(w, termcolor.Success("%s uninstalled from %s", agent.Name, root))
		}
		result.PrintText(w)
	})
	return nil
}

// UninstallOptions tunes Uninstall behavior. DryRun reports what would
// happen without modifying disk.
type UninstallOptions struct {
	DryRun bool
}

// Uninstall removes the files recorded in rec from projectRoot. It
// preserves any file whose contents differ from what the install would
// re-render today (i.e. the user edited it). Reverses the doc-file
// rename if rec.RenamedTo exists at the project root.
func Uninstall(agent *Agent, projectRoot string, rec InstallRecord, data InstallData, opts UninstallOptions) (*UninstallResult, error) {
	result := &UninstallResult{Agent: agent.Key}
	expected := expectedRenderings(agent, data)

	if err := removeRecordedFiles(projectRoot, rec.CreatedFiles, expected, opts, result); err != nil {
		return nil, err
	}
	if err := reverseDocRename(agent, projectRoot, rec, opts, result); err != nil {
		return nil, err
	}
	return result, nil
}

// expectedRenderings returns a map of dest-path → rendered content for
// every template the agent ships. Used by Uninstall to detect files the
// user has edited since install. Errors are swallowed: if templates
// have moved between install and uninstall, we fall back to "no expected
// content" which means uninstall will remove files unconditionally —
// the manifest's CreatedFiles list is still the source of truth for
// ownership.
func expectedRenderings(agent *Agent, data InstallData) map[string][]byte {
	out := map[string][]byte{}
	files, err := TemplateFiles(agent)
	if err != nil {
		return out
	}
	for _, tf := range files {
		if rendered, rerr := renderTemplate(tf.SourcePath, data); rerr == nil {
			out[tf.DestPath] = rendered
		}
	}
	return out
}

// removeRecordedFiles walks the manifest's CreatedFiles list, removing
// each file (or recording it as preserved/not-found) per the rules
// described on Uninstall. Mutates result in place.
func removeRecordedFiles(projectRoot string, created []string, expected map[string][]byte, opts UninstallOptions, result *UninstallResult) error {
	paths := append([]string(nil), created...)
	sort.Strings(paths)

	for _, rel := range paths {
		if err := removeOneFile(projectRoot, rel, expected, opts, result); err != nil {
			return err
		}
	}
	return nil
}

// removeOneFile handles a single entry in the CreatedFiles list. Splits
// out from removeRecordedFiles so the per-file branching doesn't
// inflate the parent's cyclomatic complexity.
func removeOneFile(projectRoot, rel string, expected map[string][]byte, opts UninstallOptions, result *UninstallResult) error {
	abs := filepath.Join(projectRoot, rel)
	current, err := os.ReadFile(abs)
	if os.IsNotExist(err) {
		result.NotFound = append(result.NotFound, rel)
		return nil
	}
	if err != nil {
		return clierr.Wrapf(clierr.CodeAIInstallFailed, err,
			"read %s for uninstall", rel)
	}
	if exp, ok := expected[rel]; ok && !bytes.Equal(current, exp) {
		result.Preserved = append(result.Preserved, rel)
		return nil
	}
	if opts.DryRun {
		result.Removed = append(result.Removed, rel)
		return nil
	}
	if err := os.Remove(abs); err != nil {
		return clierr.Wrapf(clierr.CodeAIInstallFailed, err,
			"remove %s", rel)
	}
	result.Removed = append(result.Removed, rel)
	removeEmptyParents(projectRoot, filepath.Dir(abs))
	return nil
}

// reverseDocRename undoes the AGENTS.md → DocFilename rename performed
// at install time, if any. No-op when the agent reads AGENTS.md
// natively (no rename was recorded) or when the destination has been
// removed/replaced since install.
func reverseDocRename(agent *Agent, projectRoot string, rec InstallRecord, opts UninstallOptions, result *UninstallResult) error {
	if rec.RenamedTo == "" || rec.RenamedFrom == "" {
		return nil
	}
	dstAbs := filepath.Join(projectRoot, rec.RenamedTo)
	srcAbs := filepath.Join(projectRoot, rec.RenamedFrom)
	if !fileExists(dstAbs) || fileExists(srcAbs) {
		return nil
	}
	entry := rec.RenamedTo + " → " + rec.RenamedFrom
	if opts.DryRun {
		result.Renamed = append(result.Renamed, entry)
		return nil
	}
	// Restore the original title + intro paragraph BEFORE the rename,
	// so the file that lands at AGENTS.md is shaped like the original
	// scaffold output. Exact-string match keeps user edits to other
	// sections intact; if the user has hand-edited the title/intro
	// themselves, the no-op branch leaves the file alone.
	// Routed through restoreDocFn so tests can inject a failure.
	if err := restoreDocFn(dstAbs, agent); err != nil {
		return err
	}
	if err := osRename(dstAbs, srcAbs); err != nil {
		return clierr.Wrapf(clierr.CodeAIInstallFailed, err,
			"rename %s → %s", rec.RenamedTo, rec.RenamedFrom)
	}
	result.Renamed = append(result.Renamed, entry)
	return nil
}

// removeEmptyParents walks up from dir toward projectRoot, removing
// each directory that's empty. Best-effort — failures are silent
// because a non-empty parent is the normal case (the user added their
// own files alongside ours).
func removeEmptyParents(projectRoot, dir string) {
	rootAbs, _ := filepath.Abs(projectRoot)
	for {
		curAbs, _ := filepath.Abs(dir)
		if curAbs == rootAbs || curAbs == filepath.Dir(curAbs) {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
