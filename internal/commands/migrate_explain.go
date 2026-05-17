package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/gofastadev/cli/internal/commands/sqllint"
	"github.com/gofastadev/cli/internal/termcolor"
)

// migrateExplainFlags is set from cobra when --explain or --strict is passed
// on `gofasta migrate up`.
var migrateExplainFlags struct {
	enabled bool
	strict  bool
}

// MigrationFile is one db/migrations/*.up.sql entry with its lint report.
type MigrationFile struct {
	Version    string              `json:"version"`
	Name       string              `json:"name"`
	File       string              `json:"file"`
	Statements []sqllint.Statement `json:"statements"`
	MaxRisk    sqllint.Risk        `json:"max_risk"`
}

// MigrateExplainResult is the JSON payload emitted by `migrate up --explain`.
// Field names are stable contract.
type MigrateExplainResult struct {
	Driver       string          `json:"driver"`
	MigrationDir string          `json:"migration_dir"`
	Count        int             `json:"count"`
	Migrations   []MigrationFile `json:"migrations"`
	MaxRisk      sqllint.Risk    `json:"max_risk"`
	HighCount    int             `json:"high_count"`
	MediumCount  int             `json:"medium_count"`
	LowCount     int             `json:"low_count"`
}

// runMigrateExplain reads every .up.sql file under db/migrations/, runs
// sqllint over each, and emits a structured report. No database is opened —
// this is purely static analysis suitable for CI / pre-deploy review.
//
// Returns CodeMigrationFailed when --strict is set and any high-severity
// warning fires. Otherwise always exits 0; the goal is informational.
func runMigrateExplain() error {
	driver := configutil.ReadDBDriver()
	dir := filepath.Join("db", "migrations")

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return clierr.Newf(clierr.CodeMigrationMissing,
				"%s does not exist — generate a migration with `gofasta g migration <name>` first", dir)
		}
		return clierr.Wrap(clierr.CodeFileIO, err, "reading "+dir)
	}

	// Collect every .up.sql, sorted by filename so output is deterministic.
	type upFile struct {
		path    string
		name    string
		version string
		title   string
	}
	var ups []upFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		version, title := parseMigrationName(name)
		ups = append(ups, upFile{
			path:    filepath.Join(dir, name),
			name:    name,
			version: version,
			title:   title,
		})
	}
	sort.Slice(ups, func(i, j int) bool { return ups[i].name < ups[j].name })

	result := MigrateExplainResult{
		Driver:       driver,
		MigrationDir: dir,
		Count:        len(ups),
		MaxRisk:      sqllint.RiskSafe,
	}

	for _, up := range ups {
		raw, readErr := os.ReadFile(up.path)
		if readErr != nil {
			return clierr.Wrapf(clierr.CodeFileIO, readErr, "reading %s", up.path)
		}
		report, lintErr := sqllint.Lint(driver, string(raw))
		if lintErr != nil {
			return clierr.Wrapf(clierr.CodeMigrationLintFailed, lintErr,
				"linting %s", up.path)
		}
		mf := MigrationFile{
			Version:    up.version,
			Name:       up.title,
			File:       up.path,
			Statements: report.Statements,
			MaxRisk:    report.MaxRisk,
		}
		result.Migrations = append(result.Migrations, mf)
		result.HighCount += report.HighCount
		result.MediumCount += report.MediumCount
		result.LowCount += report.LowCount
		if riskRank(report.MaxRisk) > riskRank(result.MaxRisk) {
			result.MaxRisk = report.MaxRisk
		}
	}

	cliout.Print(result, func(w io.Writer) { printExplainText(w, result) })

	if migrateExplainFlags.strict && result.HighCount > 0 {
		return clierr.Newf(clierr.CodeMigrationLintFailed,
			"--strict: %d high-severity warning(s) — fix or downgrade before re-running",
			result.HighCount)
	}
	return nil
}

// parseMigrationName splits "000007_add_archive_to_orders.up.sql" into
// version ("000007") and title ("add_archive_to_orders"). Falls back to
// the full name minus the suffix when the prefix isn't a number.
func parseMigrationName(filename string) (version, title string) {
	stripped := strings.TrimSuffix(filename, ".up.sql")
	if idx := strings.Index(stripped, "_"); idx > 0 {
		return stripped[:idx], stripped[idx+1:]
	}
	return "", stripped
}

// riskRank mirrors sqllint.Risk.rank but is local — we don't expose the
// internal ordering, so callers compare via this helper.
func riskRank(r sqllint.Risk) int {
	switch r {
	case sqllint.RiskDataLoss:
		return 5
	case sqllint.RiskLockAndRewrite:
		return 4
	case sqllint.RiskLockAndFill:
		return 3
	case sqllint.RiskLockTable:
		return 2
	case sqllint.RiskAppIncompat:
		return 1
	default:
		return 0
	}
}

// printExplainText renders the human-friendly view of an explain report.
// JSON mode bypasses this entirely and emits the struct verbatim.
func printExplainText(w io.Writer, r MigrateExplainResult) {
	if r.Count == 0 {
		_, _ = fmt.Fprintln(w, "No migrations found in", r.MigrationDir)
		return
	}
	_, _ = fmt.Fprintf(w, "Driver: %s · %d migration file(s) under %s\n\n",
		r.Driver, r.Count, r.MigrationDir)

	for _, m := range r.Migrations {
		_, _ = fmt.Fprintf(w, "  %s  %s\n", termcolor.CBrand(m.Version), m.Name)
		_, _ = fmt.Fprintf(w, "    file: %s\n", m.File)
		if m.MaxRisk != sqllint.RiskSafe {
			_, _ = fmt.Fprintf(w, "    risk: %s\n", colorRisk(m.MaxRisk))
		}
		for _, st := range m.Statements {
			if len(st.Warnings) == 0 {
				continue
			}
			short := shorten(st.SQL, 80)
			_, _ = fmt.Fprintf(w, "    · %s\n", short)
			for _, warn := range st.Warnings {
				_, _ = fmt.Fprintf(w, "      [%s] %s — %s\n",
					colorSeverity(warn.Severity), warn.Rule, warn.Message)
			}
		}
		_, _ = fmt.Fprintln(w)
	}

	_, _ = fmt.Fprintf(w, "Summary: max-risk=%s · %d high · %d medium · %d low\n",
		colorRisk(r.MaxRisk), r.HighCount, r.MediumCount, r.LowCount)
	if migrateExplainFlags.strict && r.HighCount > 0 {
		_, _ = fmt.Fprintln(w, termcolor.CRed("--strict will exit non-zero due to high-severity warnings."))
	}
}

func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func colorRisk(r sqllint.Risk) string {
	switch r {
	case sqllint.RiskDataLoss, sqllint.RiskLockAndRewrite, sqllint.RiskLockAndFill:
		return termcolor.CRed(string(r))
	case sqllint.RiskLockTable, sqllint.RiskAppIncompat:
		return termcolor.CYellow(string(r))
	default:
		return termcolor.CGreen(string(r))
	}
}

func colorSeverity(s sqllint.Severity) string {
	switch s {
	case sqllint.SeverityHigh:
		return termcolor.CRed(string(s))
	case sqllint.SeverityMedium:
		return termcolor.CYellow(string(s))
	default:
		return termcolor.CBrand(string(s))
	}
}
