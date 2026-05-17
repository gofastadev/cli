package sqllint

import (
	"regexp"
	"strings"
)

// Rule is the contract every static-analysis rule implements. Rules are
// pure: given a statement type + raw text, return zero or more warnings.
// Statelessness keeps testing trivial and lets us evaluate rules in any
// order.
type Rule interface {
	// Name is the stable identifier reported in JSON output. Never rename
	// once shipped — agents and CI may match on it.
	Name() string
	// AppliesTo returns true if this rule should run against the given
	// driver. Cross-driver rules return true for everything.
	AppliesTo(driver string) bool
	// Match runs the rule against one statement. Empty result means no hit.
	Match(stmtType, sql string) []Warning
}

// allRules is the evaluation order — built once at package init.
var allRules = []Rule{
	ruleDropColumn{},
	ruleAddColumnNotNullNoDefault{},
	ruleCreateIndexBlocking{},
	ruleDropTable{},
	ruleTruncate{},
	ruleRenameColumn{},
	ruleRenameTable{},
	ruleAlterColumnType{},
	ruleAddPrimaryKey{},
}

// ----- helpers -----------------------------------------------------------

// normalize folds the SQL to uppercase and collapses whitespace, so rules
// can match against canonical token sequences without worrying about
// formatting.
func normalize(sql string) string {
	return strings.Join(strings.Fields(strings.ToUpper(sql)), " ")
}

func anyOf(driver string, drivers ...string) bool {
	for _, d := range drivers {
		if driver == d {
			return true
		}
	}
	return false
}

// ----- rules -------------------------------------------------------------

// DROP COLUMN destroys data on the dropped column. There is no in-place
// recovery once the migration runs.
type ruleDropColumn struct{}

// Name implements [Rule].
func (ruleDropColumn) Name() string { return "DropColumn" }

// AppliesTo implements [Rule].
func (ruleDropColumn) AppliesTo(driver string) bool { return true }

var reDropColumn = regexp.MustCompile(`\bALTER\s+TABLE\b.*\bDROP\s+COLUMN\b`)

// Match implements [Rule].
func (ruleDropColumn) Match(stmtType, sql string) []Warning {
	if stmtType != "alter_table" {
		return nil
	}
	if !reDropColumn.MatchString(normalize(sql)) {
		return nil
	}
	return []Warning{{
		Rule:     "DropColumn",
		Message:  "DROP COLUMN permanently removes data; backfill or two-phase deploy if the column is still read by any running app version",
		Severity: SeverityHigh,
		Risk:     RiskDataLoss,
	}}
}

// ADD COLUMN ... NOT NULL without a DEFAULT forces a table rewrite on most
// engines and holds a table-level lock for the duration. Adding a DEFAULT
// (even one computed by the engine) usually avoids both.
type ruleAddColumnNotNullNoDefault struct{}

// Name implements [Rule].
func (ruleAddColumnNotNullNoDefault) Name() string { return "AddColumnNotNullNoDefault" }

// AppliesTo implements [Rule].
func (ruleAddColumnNotNullNoDefault) AppliesTo(driver string) bool { return true }

var reAddColumn = regexp.MustCompile(`\bALTER\s+TABLE\b.*\bADD\s+(COLUMN\s+)?(\w+)\b`)

// Match implements [Rule].
func (ruleAddColumnNotNullNoDefault) Match(stmtType, sql string) []Warning {
	if stmtType != "alter_table" {
		return nil
	}
	n := normalize(sql)
	if !reAddColumn.MatchString(n) {
		return nil
	}
	if !strings.Contains(n, "NOT NULL") {
		return nil
	}
	if strings.Contains(n, "DEFAULT ") {
		return nil
	}
	return []Warning{{
		Rule:     "AddColumnNotNullNoDefault",
		Message:  "ADD COLUMN ... NOT NULL without DEFAULT rewrites every existing row and holds an exclusive table lock for the duration; supply a DEFAULT or do it in two migrations (add nullable, backfill, then add NOT NULL constraint)",
		Severity: SeverityHigh,
		Risk:     RiskLockAndFill,
	}}
}

// CREATE INDEX without CONCURRENTLY (Postgres) or ONLINE (MySQL 8 / SQL
// Server) holds a write lock on the table while the index is built. On
// large tables this is minutes to hours.
type ruleCreateIndexBlocking struct{}

// Name implements [Rule].
func (ruleCreateIndexBlocking) Name() string { return "CreateIndexBlocking" }

// AppliesTo implements [Rule].
func (ruleCreateIndexBlocking) AppliesTo(driver string) bool {
	return anyOf(driver, "postgres", "mysql", "sqlserver")
}

var reCreateIndex = regexp.MustCompile(`\bCREATE\s+(UNIQUE\s+)?INDEX\b`)

// Match implements [Rule].
func (r ruleCreateIndexBlocking) Match(stmtType, sql string) []Warning {
	if stmtType != "create_index" {
		return nil
	}
	n := normalize(sql)
	if !reCreateIndex.MatchString(n) {
		return nil
	}
	// Driver-specific safe-mode keywords.
	if strings.Contains(n, "CONCURRENTLY") { // postgres
		return nil
	}
	if strings.Contains(n, " ONLINE") { // sqlserver
		return nil
	}
	return []Warning{{
		Rule:     "CreateIndexBlocking",
		Message:  "CREATE INDEX holds a write lock on the table for its duration; add CONCURRENTLY (Postgres) or ONLINE (SQL Server) on large tables",
		Severity: SeverityMedium,
		Risk:     RiskLockTable,
	}}
}

// DROP TABLE is data-loss.
type ruleDropTable struct{}

// Name implements [Rule].
func (ruleDropTable) Name() string { return "DropTable" }

// AppliesTo implements [Rule].
func (ruleDropTable) AppliesTo(driver string) bool { return true }

// Match implements [Rule].
func (ruleDropTable) Match(stmtType, sql string) []Warning {
	if stmtType != "drop_table" {
		return nil
	}
	return []Warning{{
		Rule:     "DropTable",
		Message:  "DROP TABLE permanently removes the table and all its data; consider RENAME first as a two-phase deploy",
		Severity: SeverityHigh,
		Risk:     RiskDataLoss,
	}}
}

// TRUNCATE is also data-loss, but separate so the message can be specific.
type ruleTruncate struct{}

// Name implements [Rule].
func (ruleTruncate) Name() string { return "Truncate" }

// AppliesTo implements [Rule].
func (ruleTruncate) AppliesTo(driver string) bool { return true }

// Match implements [Rule].
func (ruleTruncate) Match(stmtType, sql string) []Warning {
	if stmtType != "truncate" {
		return nil
	}
	return []Warning{{
		Rule:     "Truncate",
		Message:  "TRUNCATE deletes every row; in Postgres it cannot be rolled back in a separate transaction",
		Severity: SeverityHigh,
		Risk:     RiskDataLoss,
	}}
}

// RENAME COLUMN breaks any running app version that still reads the old
// column name. Two-phase the rename (add new, dual-write, deploy, drop old).
type ruleRenameColumn struct{}

// Name implements [Rule].
func (ruleRenameColumn) Name() string { return "RenameColumn" }

// AppliesTo implements [Rule].
func (ruleRenameColumn) AppliesTo(driver string) bool { return true }

var reRenameColumn = regexp.MustCompile(`\bALTER\s+TABLE\b.*\bRENAME\s+COLUMN\b`)

// Match implements [Rule].
func (ruleRenameColumn) Match(stmtType, sql string) []Warning {
	if stmtType != "alter_table" {
		return nil
	}
	if !reRenameColumn.MatchString(normalize(sql)) {
		return nil
	}
	return []Warning{{
		Rule:     "RenameColumn",
		Message:  "RENAME COLUMN breaks running app versions that still read the old name; two-phase the rename (add new, dual-write, switch reads, drop old)",
		Severity: SeverityMedium,
		Risk:     RiskAppIncompat,
	}}
}

// RENAME TABLE has the same app-incompatibility risk as RENAME COLUMN, on
// every driver. Covers both `ALTER TABLE x RENAME TO y` and `RENAME TABLE`.
type ruleRenameTable struct{}

// Name implements [Rule].
func (ruleRenameTable) Name() string { return "RenameTable" }

// AppliesTo implements [Rule].
func (ruleRenameTable) AppliesTo(driver string) bool { return true }

var reRenameTable = regexp.MustCompile(`\bALTER\s+TABLE\b.*\bRENAME\s+TO\b|^RENAME\s+TABLE\b`)

// Match implements [Rule].
func (ruleRenameTable) Match(stmtType, sql string) []Warning {
	n := normalize(sql)
	if !reRenameTable.MatchString(n) {
		return nil
	}
	return []Warning{{
		Rule:     "RenameTable",
		Message:  "RENAME TABLE breaks running app versions that still query the old name; two-phase the rename (alias / view, switch reads, drop)",
		Severity: SeverityMedium,
		Risk:     RiskAppIncompat,
	}}
}

// ALTER COLUMN TYPE rewrites the entire table on most engines and holds a
// table-level lock.
type ruleAlterColumnType struct{}

// Name implements [Rule].
func (ruleAlterColumnType) Name() string { return "AlterColumnType" }

// AppliesTo implements [Rule].
func (ruleAlterColumnType) AppliesTo(driver string) bool { return true }

var reAlterType = regexp.MustCompile(`\bALTER\s+TABLE\b.*\bALTER\s+COLUMN\b.*\bTYPE\b|\bMODIFY\s+(COLUMN\s+)?\w+\s+\w+`)

// Match implements [Rule].
func (ruleAlterColumnType) Match(stmtType, sql string) []Warning {
	if stmtType != "alter_table" {
		return nil
	}
	if !reAlterType.MatchString(normalize(sql)) {
		return nil
	}
	return []Warning{{
		Rule:     "AlterColumnType",
		Message:  "ALTER COLUMN ... TYPE rewrites every row and holds an exclusive table lock; on large tables, prefer a new column + backfill + cutover",
		Severity: SeverityHigh,
		Risk:     RiskLockAndRewrite,
	}}
}

// ADD PRIMARY KEY (after the fact) is a table-level lock + rewrite event
// on most engines.
type ruleAddPrimaryKey struct{}

// Name implements [Rule].
func (ruleAddPrimaryKey) Name() string { return "AddPrimaryKey" }

// AppliesTo implements [Rule].
func (ruleAddPrimaryKey) AppliesTo(driver string) bool { return true }

var reAddPK = regexp.MustCompile(`\bALTER\s+TABLE\b.*\bADD\s+(CONSTRAINT\s+\w+\s+)?PRIMARY\s+KEY\b`)

// Match implements [Rule].
func (ruleAddPrimaryKey) Match(stmtType, sql string) []Warning {
	if stmtType != "alter_table" {
		return nil
	}
	if !reAddPK.MatchString(normalize(sql)) {
		return nil
	}
	return []Warning{{
		Rule:     "AddPrimaryKey",
		Message:  "ADD PRIMARY KEY after table creation holds an exclusive lock and rewrites the table; create the column with PRIMARY KEY in CREATE TABLE if possible",
		Severity: SeverityMedium,
		Risk:     RiskLockTable,
	}}
}
