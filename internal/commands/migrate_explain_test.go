package commands

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/commands/sqllint"
)

// helper: switch cwd into a temp dir for the duration of one test, ensure
// the original cwd is restored regardless of the test outcome.
func chdirTest(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
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
		if !(riskRank(order[i]) > riskRank(order[i-1])) {
			t.Errorf("riskRank(%q)=%d must be > riskRank(%q)=%d",
				order[i], riskRank(order[i]), order[i-1], riskRank(order[i-1]))
		}
	}
}

// ----- helpers -----------------------------------------------------------

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func codeOf(err error) string {
	var ce *clierr.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}
