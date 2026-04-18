package ai

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"text/template"

	"github.com/gofastadev/cli/internal/clierr"
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
type InstallResult struct {
	Agent        string   `json:"agent"`
	Created      []string `json:"created"`
	Skipped      []string `json:"skipped"`
	WouldReplace []string `json:"would_replace"`
	Replaced     []string `json:"replaced"`
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
// which files were created/skipped/replaced so callers can render
// either a human table or a JSON payload.
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

// renderTemplate reads the embedded template and executes it with data.
// Uses text/template (not html/template) — we're producing config files
// and shell scripts, not HTML.
func renderTemplate(sourcePath string, data InstallData) ([]byte, error) {
	raw, err := ReadTemplate(sourcePath)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New(filepath.Base(sourcePath)).Parse(string(raw))
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
func (r *InstallResult) PrintText(w io.Writer) {
	if len(r.Created) > 0 {
		fprintf(w, "  created %d file(s):\n", len(r.Created))
		for _, f := range r.Created {
			fprintf(w, "    + %s\n", f)
		}
	}
	if len(r.Replaced) > 0 {
		fprintf(w, "  replaced %d file(s):\n", len(r.Replaced))
		for _, f := range r.Replaced {
			fprintf(w, "    ~ %s\n", f)
		}
	}
	if len(r.WouldReplace) > 0 {
		fprintf(w, "  would replace %d file(s) (dry run):\n", len(r.WouldReplace))
		for _, f := range r.WouldReplace {
			fprintf(w, "    ~ %s\n", f)
		}
	}
	if len(r.Skipped) > 0 {
		fprintf(w, "  skipped %d unchanged file(s)\n", len(r.Skipped))
	}
}
