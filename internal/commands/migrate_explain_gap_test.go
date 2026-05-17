package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/commands/sqllint"
	"github.com/stretchr/testify/require"
)

// TestRunMigrateExplain_ReadDirNonNotExistError — make db/migrations a
// regular file so os.ReadDir returns a non-NotExist error, surfacing as
// CodeFileIO.
func TestRunMigrateExplain_ReadDirNonNotExistError(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "db"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "db", "migrations"), []byte("not a dir"), 0o644))
	require.Error(t, runMigrateExplain())
}

// TestRunMigrateExplain_SkipsDirAndNonUpSql — a subdirectory and a
// non-.up.sql file both must be skipped.
func TestRunMigrateExplain_SkipsDirAndNonUpSql(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	migDir := filepath.Join(tmp, "db", "migrations")
	require.NoError(t, os.MkdirAll(migDir, 0o755))
	// Subdirectory — must be skipped.
	require.NoError(t, os.MkdirAll(filepath.Join(migDir, "subdir"), 0o755))
	// Non-up.sql — must be skipped.
	require.NoError(t, os.WriteFile(filepath.Join(migDir, "README.md"), []byte("# notes"), 0o644))
	// Valid up.sql so the run completes.
	require.NoError(t, os.WriteFile(filepath.Join(migDir, "000001_create.up.sql"),
		[]byte("CREATE TABLE u (id int PRIMARY KEY);"), 0o644))

	migrateExplainFlags.strict = false
	require.NoError(t, runMigrateExplain())
}

// TestRunMigrateExplain_ReadFileError — chmod 0o000 a .up.sql so
// os.ReadFile returns EACCES inside the loop, surfacing as CodeFileIO.
func TestRunMigrateExplain_ReadFileError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	tmp := t.TempDir()
	chdirTest(t, tmp)
	migDir := filepath.Join(tmp, "db", "migrations")
	require.NoError(t, os.MkdirAll(migDir, 0o755))
	upPath := filepath.Join(migDir, "000001_x.up.sql")
	require.NoError(t, os.WriteFile(upPath, []byte("SELECT 1;"), 0o644))
	require.NoError(t, os.Chmod(upPath, 0o000))
	t.Cleanup(func() { _ = os.Chmod(upPath, 0o644) })

	require.Error(t, runMigrateExplain())
}

// TestRunMigrateExplain_LintError — an unterminated string literal
// makes sqllint.SplitStatements (and thus Lint) fail; the error path
// returns CodeMigrationLintFailed.
func TestRunMigrateExplain_LintError(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)
	migDir := filepath.Join(tmp, "db", "migrations")
	require.NoError(t, os.MkdirAll(migDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(migDir, "000001_x.up.sql"),
		[]byte("SELECT 'unterminated"), 0o644))

	migrateExplainFlags.strict = false
	require.Error(t, runMigrateExplain())
}

// TestPrintExplainText_Empty — count-0 branch.
func TestPrintExplainText_Empty(t *testing.T) {
	var buf bytes.Buffer
	printExplainText(&buf, MigrateExplainResult{MigrationDir: "db/migrations"})
	require.Contains(t, buf.String(), "No migrations found in")
}

// TestShorten_Truncates — s longer than n triggers the truncation branch.
func TestShorten_Truncates(t *testing.T) {
	got := shorten(strings.Repeat("a b ", 50), 20)
	require.Len(t, []rune(got), 20)
	require.Equal(t, '…', []rune(got)[19])
}

// TestColorRisk_AllBranches — exercise every branch of the colorRisk
// switch.
func TestColorRisk_AllBranches(t *testing.T) {
	for _, r := range []sqllint.Risk{
		sqllint.RiskDataLoss,
		sqllint.RiskLockAndRewrite,
		sqllint.RiskLockAndFill,
		sqllint.RiskLockTable,
		sqllint.RiskAppIncompat,
		sqllint.RiskSafe,
	} {
		got := colorRisk(r)
		require.NotEmpty(t, got)
	}
}

// TestColorSeverity_AllBranches — exercise every branch of the
// colorSeverity switch.
func TestColorSeverity_AllBranches(t *testing.T) {
	for _, s := range []sqllint.Severity{
		sqllint.SeverityHigh,
		sqllint.SeverityMedium,
		sqllint.SeverityLow,
	} {
		got := colorSeverity(s)
		require.NotEmpty(t, got)
	}
}
