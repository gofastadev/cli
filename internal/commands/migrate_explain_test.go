package commands

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
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

func TestParseMigrationName(t *testing.T) {
	cases := map[string][2]string{
		"000007_add_archive_to_orders.up.sql": {"000007", "add_archive_to_orders"},
		"000001_create_users.up.sql":          {"000001", "create_users"},
		"odd_filename_without_number.up.sql":  {"odd", "filename_without_number"},
		"no_underscores.up.sql":               {"no", "underscores"},
		"trailing.up.sql":                     {"", "trailing"},
	}
	for in, want := range cases {
		v, n := parseMigrationName(in)
		if v != want[0] || n != want[1] {
			t.Errorf("parseMigrationName(%q) = (%q, %q), want (%q, %q)", in, v, n, want[0], want[1])
		}
	}
}

func TestRunMigrateExplain_MissingDirReturnsClierr(t *testing.T) {
	chdirTest(t, t.TempDir())

	err := runMigrateExplain()
	if err == nil {
		t.Fatal("expected error when db/migrations is missing")
	}
	var ce *clierr.Error
	if !errors.As(err, &ce) || ce.Code != string(clierr.CodeMigrationMissing) {
		t.Errorf("got %v (code %q), want CodeMigrationMissing", err, codeOf(err))
	}
}

func TestRunMigrateExplain_DetectsRiskyMigration(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)

	if err := os.MkdirAll(filepath.Join(tmp, "db", "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	// One safe + one risky migration.
	mustWrite(t, filepath.Join(tmp, "db", "migrations", "000001_create_users.up.sql"),
		"CREATE TABLE users (id int PRIMARY KEY);")
	mustWrite(t, filepath.Join(tmp, "db", "migrations", "000002_drop_users.up.sql"),
		"DROP TABLE users;")

	// Reset strict flag in case a previous test left it on.
	migrateExplainFlags.strict = false
	if err := runMigrateExplain(); err != nil {
		t.Fatalf("runMigrateExplain returned unexpected error: %v", err)
	}
}

func TestRunMigrateExplain_StrictExitsNonzeroOnHigh(t *testing.T) {
	tmp := t.TempDir()
	chdirTest(t, tmp)

	if err := os.MkdirAll(filepath.Join(tmp, "db", "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(tmp, "db", "migrations", "000001_drop_users.up.sql"),
		"DROP TABLE users;") // RuleDropTable fires as high

	migrateExplainFlags.strict = true
	t.Cleanup(func() { migrateExplainFlags.strict = false })

	err := runMigrateExplain()
	if err == nil {
		t.Fatal("expected --strict to surface error when high-severity warning fires")
	}
	if codeOf(err) != string(clierr.CodeMigrationLintFailed) {
		t.Errorf("got code %q, want CodeMigrationLintFailed", codeOf(err))
	}
}

func TestRiskRankOrdering(t *testing.T) {
	// Encodes the contract: data-loss > rewrite > fill > lock > app-incompat > safe.
	order := []sqllint.Risk{
		sqllint.RiskSafe,
		sqllint.RiskAppIncompat,
		sqllint.RiskLockTable,
		sqllint.RiskLockAndFill,
		sqllint.RiskLockAndRewrite,
		sqllint.RiskDataLoss,
	}
	for i := 1; i < len(order); i++ {
		if riskRank(order[i]) <= riskRank(order[i-1]) {
			t.Errorf("riskRank(%q)=%d must be > riskRank(%q)=%d",
				order[i], riskRank(order[i]), order[i-1], riskRank(order[i-1]))
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
