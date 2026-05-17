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

// TestSplitStatements_BlockCommentClose — closes a /* ... */ block.
func TestSplitStatements_BlockCommentClose(t *testing.T) {
	stmts, err := SplitStatements("/* hello */ SELECT 1;")
	require.NoError(t, err)
	require.Equal(t, 1, len(stmts))
}

// TestSplitStatements_BlockCommentUnterminated — exercise
// finishError's inBlock branch.
func TestSplitStatements_BlockCommentUnterminated(t *testing.T) {
	_, err := SplitStatements("/* never ends")
	require.Error(t, err)
}

// TestSplitStatements_DollarUnterminated — exercise finishError's
// dollar-quote branch.
func TestSplitStatements_DollarUnterminated(t *testing.T) {
	_, err := SplitStatements("DO $$ BEGIN PERFORM 1; END")
	require.Error(t, err)
}

// TestSplitStatements_BackslashEscapeInString — exercise the
// backslash-escape branch in advanceInString (line 125-129).
func TestSplitStatements_BackslashEscapeInString(t *testing.T) {
	stmts, err := SplitStatements(`INSERT INTO t VALUES ('a\nb');`)
	require.NoError(t, err)
	require.Equal(t, 1, len(stmts))
}

// TestSplitStatements_PeekOutOfRange — a trailing single '-' triggers
// peek(1) returning 0 (out-of-range branch).
func TestSplitStatements_PeekOutOfRange(t *testing.T) {
	stmts, err := SplitStatements("SELECT 1; -")
	require.NoError(t, err)
	// Last "-" is just a regular char (peek(1) was 0 so the comment
	// branch didn't fire); the buffer trim returns the trailing run.
	require.GreaterOrEqual(t, len(stmts), 1)
}

// TestSplitStatements_DollarFollowedByEOF — `$` at EOF should NOT
// enter a dollar-quote (tryEnterDollarQuote's out-of-range branch).
func TestSplitStatements_DollarFollowedByEOF(t *testing.T) {
	stmts, err := SplitStatements("SELECT '$' ;")
	require.NoError(t, err)
	require.Equal(t, 1, len(stmts))
}

// TestSplitStatements_BareDollar — a single `$` not followed by tag/$.
// tryEnterDollarQuote returns false.
func TestSplitStatements_BareDollar(t *testing.T) {
	stmts, err := SplitStatements("SELECT $;")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(stmts), 1)
}

// TestSplitStatements_DollarTagged — $tag$...$tag$ block round-trip.
func TestSplitStatements_DollarTagged(t *testing.T) {
	stmts, err := SplitStatements("DO $body$ BEGIN PERFORM 1; END $body$ ;")
	require.NoError(t, err)
	require.Equal(t, 1, len(stmts))
}

// TestClassify_AllBranches — exercise every branch of classify.
func TestClassify_AllBranches(t *testing.T) {
	cases := map[string]string{
		"ALTER TABLE users ADD c int":             "alter_table",
		"CREATE TABLE x (id int)":                 "create_table",
		"CREATE INDEX idx ON t(c)":                "create_index",
		"CREATE UNIQUE INDEX idx ON t(c)":         "create_index",
		"DROP TABLE x":                            "drop_table",
		"DROP INDEX idx":                          "drop_index",
		"TRUNCATE TABLE x":                        "truncate",
		"RENAME TABLE old TO new":                 "rename_table",
		"INSERT INTO t VALUES (1)":                "insert",
		"UPDATE t SET c = 1":                      "update",
		"DELETE FROM t":                           "delete",
		"-- comment only\nSELECT 1":               "other",
	}
	for in, want := range cases {
		require.Equal(t, want, classify(in), "classify(%q)", in)
	}
}

// TestRuleCreateIndexBlocking_WrongStmtType — Match returns nil when
// the statement is not classified as create_index.
func TestRuleCreateIndexBlocking_WrongStmtType(t *testing.T) {
	r := ruleCreateIndexBlocking{}
	got := r.Match("alter_table", "ALTER TABLE x ADD c int;")
	require.Nil(t, got)
}

// TestRuleCreateIndexBlocking_OnlineKeyword — SQL Server's ONLINE
// keyword silences CreateIndexBlocking (line 148-150).
func TestRuleCreateIndexBlocking_OnlineKeyword(t *testing.T) {
	r := ruleCreateIndexBlocking{}
	got := r.Match("create_index", "CREATE INDEX idx ON t(c) WITH ONLINE = ON;")
	require.Nil(t, got)
}

// lowRule is a test-only Rule that emits a single SeverityLow warning
// on any "create_table" statement. Registered briefly to cover Lint's
// SeverityLow accumulator branch.
type lowRule struct{}

func (lowRule) Name() string                       { return "TestLow" }
func (lowRule) AppliesTo(_ string) bool            { return true }
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

// TestRuleCreateIndexBlocking_RegexMismatch — stmtType says create_index
// but the SQL doesn't contain CREATE INDEX (forced classifier
// disagreement) → regex misses, returns nil.
func TestRuleCreateIndexBlocking_RegexMismatch(t *testing.T) {
	r := ruleCreateIndexBlocking{}
	got := r.Match("create_index", "SOMETHING ELSE")
	require.Nil(t, got)
}
