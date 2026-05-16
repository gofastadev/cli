// Package sqllint performs static analysis of SQL migration files to flag
// patterns that are dangerous in production: data-loss, table-level locks,
// long-running rewrites, and application-incompatibility (renames).
//
// The analysis is purely static — sqllint never opens a database connection.
// That keeps the command usable offline, in CI, before deploy, and against
// any driver. Driver-specific rules (e.g. Postgres CREATE INDEX needs
// CONCURRENTLY) are gated on the configured driver.
//
// Splitting strategy: SQL is parsed statement-by-statement using a stateful
// tokenizer that respects string literals ('...' / "..."), line comments
// (--), block comments (/* ... */), and Postgres dollar-quote blocks
// ($$...$$ and tagged $tag$...$tag$). Statements end at the first
// unescaped semicolon outside any of those contexts.
//
// Rules are evaluated as simple keyword + regex matchers on each statement.
// We intentionally avoid pulling in a full SQL parser — the rule set we
// care about is small and the false-positive rate of a strict tokenizer is
// negligible for migration files.
package sqllint

import (
	"strings"
)

// Risk classifies the worst-case impact of a statement at production scale.
// Severity is the ordering used by max_risk aggregation.
type Risk string

const (
	RiskSafe           Risk = "safe"
	RiskAppIncompat    Risk = "app-incompatibility"
	RiskLockTable      Risk = "lock-table"
	RiskLockAndFill    Risk = "lock-and-fill"
	RiskLockAndRewrite Risk = "lock-and-rewrite"
	RiskDataLoss       Risk = "data-loss"
)

// rank assigns a numeric ordering so we can compute max_risk across a
// migration file. Higher is worse.
func (r Risk) rank() int {
	switch r {
	case RiskDataLoss:
		return 5
	case RiskLockAndRewrite:
		return 4
	case RiskLockAndFill:
		return 3
	case RiskLockTable:
		return 2
	case RiskAppIncompat:
		return 1
	default:
		return 0
	}
}

// Severity is the per-warning level. Used by --strict to gate CI exit codes.
type Severity string

const (
	SeverityHigh   Severity = "high"
	SeverityMedium Severity = "medium"
	SeverityLow    Severity = "low"
)

// Statement is one SQL statement extracted from a migration file.
type Statement struct {
	// SQL is the original statement text, trimmed of leading/trailing whitespace.
	SQL string `json:"sql"`
	// Type is a coarse classification (alter_table, create_index, drop_table, etc.).
	Type string `json:"type"`
	// Risk is the worst-case impact across all warnings for this statement.
	Risk Risk `json:"risk"`
	// Warnings is every rule hit on this statement.
	Warnings []Warning `json:"warnings,omitempty"`
}

// Warning is a single rule hit on a single statement.
type Warning struct {
	Rule     string   `json:"rule"`
	Message  string   `json:"message"`
	Severity Severity `json:"severity"`
	Risk     Risk     `json:"risk"`
}

// Report is the result of linting a whole migration file.
type Report struct {
	Driver      string      `json:"driver"`
	Statements  []Statement `json:"statements"`
	MaxRisk     Risk        `json:"max_risk"`
	HighCount   int         `json:"high_count"`
	MediumCount int         `json:"medium_count"`
	LowCount    int         `json:"low_count"`
}

// Lint analyzes a single migration file's SQL text for the named driver.
// driver should be one of "postgres", "mysql", "sqlite", "sqlserver",
// "clickhouse" (the gofasta-supported drivers); unknown drivers fall back
// to the cross-driver rule set.
func Lint(driver, sql string) (Report, error) {
	stmts, err := SplitStatements(sql)
	if err != nil {
		return Report{}, err
	}

	r := Report{Driver: driver}
	r.MaxRisk = RiskSafe

	for _, raw := range stmts {
		s := analyze(driver, raw)
		for _, w := range s.Warnings {
			switch w.Severity {
			case SeverityHigh:
				r.HighCount++
			case SeverityMedium:
				r.MediumCount++
			case SeverityLow:
				r.LowCount++
			}
		}
		if s.Risk.rank() > r.MaxRisk.rank() {
			r.MaxRisk = s.Risk
		}
		r.Statements = append(r.Statements, s)
	}

	return r, nil
}

// analyze runs every applicable rule against one statement and returns the
// resulting Statement with computed risk + warnings attached.
func analyze(driver, raw string) Statement {
	trimmed := strings.TrimSpace(raw)
	s := Statement{
		SQL:  trimmed,
		Type: classify(trimmed),
		Risk: RiskSafe,
	}
	for _, rule := range allRules {
		if !rule.AppliesTo(driver) {
			continue
		}
		warns := rule.Match(s.Type, trimmed)
		for _, w := range warns {
			if w.Risk.rank() > s.Risk.rank() {
				s.Risk = w.Risk
			}
			s.Warnings = append(s.Warnings, w)
		}
	}
	return s
}

// classify returns a coarse statement type from the first keyword.
// Used by rules to short-circuit work and shown in the report for context.
func classify(stmt string) string {
	upper := strings.ToUpper(strings.TrimLeft(stmt, " \t\n\r"))
	switch {
	case strings.HasPrefix(upper, "ALTER TABLE"):
		return "alter_table"
	case strings.HasPrefix(upper, "CREATE TABLE"):
		return "create_table"
	case strings.HasPrefix(upper, "CREATE INDEX"), strings.HasPrefix(upper, "CREATE UNIQUE INDEX"):
		return "create_index"
	case strings.HasPrefix(upper, "DROP TABLE"):
		return "drop_table"
	case strings.HasPrefix(upper, "DROP INDEX"):
		return "drop_index"
	case strings.HasPrefix(upper, "TRUNCATE"):
		return "truncate"
	case strings.HasPrefix(upper, "RENAME TABLE"):
		return "rename_table"
	case strings.HasPrefix(upper, "INSERT"):
		return "insert"
	case strings.HasPrefix(upper, "UPDATE"):
		return "update"
	case strings.HasPrefix(upper, "DELETE"):
		return "delete"
	default:
		return "other"
	}
}
