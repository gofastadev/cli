package sqllint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLint_PropagatesSplitError — unterminated string literal makes
// SplitStatements error; Lint forwards it.
func TestLint_PropagatesSplitError(t *testing.T) {
	_, err := Lint("postgres", "SELECT 'oops")
	require.Error(t, err)
}

// TestLint_MediumAndLowCounts — verify the SeverityMedium and
// SeverityLow accumulators by composing a migration that triggers
// rules of those severities.
func TestLint_MediumAndLowCounts(t *testing.T) {
	// CreateIndexBlocking is SeverityMedium; bare CREATE INDEX without
	// CONCURRENTLY/ONLINE.
	sql := "CREATE INDEX idx_users_email ON users(email);"
	r, err := Lint("postgres", sql)
	require.NoError(t, err)
	require.Greater(t, r.MediumCount, 0)
}

// TestClassify_AllBranches — exercise every branch of classify.
func TestClassify_AllBranches(t *testing.T) {
	cases := map[string]string{
		"ALTER TABLE users ADD c int":     "alter_table",
		"CREATE TABLE x (id int)":         "create_table",
		"CREATE INDEX idx ON t(c)":        "create_index",
		"CREATE UNIQUE INDEX idx ON t(c)": "create_index",
		"DROP TABLE x":                    "drop_table",
		"DROP INDEX idx":                  "drop_index",
		"TRUNCATE TABLE x":                "truncate",
		"RENAME TABLE old TO new":         "rename_table",
		"INSERT INTO t VALUES (1)":        "insert",
		"UPDATE t SET c = 1":              "update",
		"DELETE FROM t":                   "delete",
		"-- comment only\nSELECT 1":       "other",
	}
	for in, want := range cases {
		require.Equal(t, want, classify(in), "classify(%q)", in)
	}
}

// lowRule is a test-only Rule that emits a single SeverityLow warning
// on any "create_table" statement. Registered briefly to cover Lint's
// SeverityLow accumulator branch.
type lowRule struct{}

func (lowRule) Name() string { return "TestLow" }

func (lowRule) AppliesTo(_ string) bool { return true }

func (lowRule) Match(stmtType, _ string) []Warning {
	if stmtType != "create_table" {
		return nil
	}
	return []Warning{{Rule: "TestLow", Message: "stub", Severity: SeverityLow, Risk: RiskSafe}}
}

// TestLint_LowSeverityAccumulated — temporarily register a low-severity
// rule and confirm Lint increments LowCount (line 123-124).
func TestLint_LowSeverityAccumulated(t *testing.T) {
	saved := allRules
	allRules = append([]Rule{lowRule{}}, saved...)
	t.Cleanup(func() { allRules = saved })

	r, err := Lint("postgres", "CREATE TABLE u (id int PRIMARY KEY);")
	require.NoError(t, err)
	require.Equal(t, 1, r.LowCount)
}

func TestLint_AggregatesMaxRisk(t *testing.T) {
	sql := `
		CREATE INDEX idx_a ON t (a);
		ALTER TABLE t DROP COLUMN b;
	`
	r, err := Lint("postgres", sql)
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if r.MaxRisk != RiskDataLoss {
		t.Errorf("MaxRisk = %q, want %q (DROP COLUMN should dominate CREATE INDEX)", r.MaxRisk, RiskDataLoss)
	}
	if r.HighCount < 1 {
		t.Errorf("HighCount = %d, want >= 1", r.HighCount)
	}
}
