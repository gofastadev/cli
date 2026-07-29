package sqllint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAllRules_NamesUnique exercises every Rule.Name() implementation
// in one shot (gives the 9 Name methods 100% coverage) and at the same
// time asserts they're distinct — duplicate names in JSON output would
// confuse the agents that pattern-match on them.
func TestAllRules_NamesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range allRules {
		name := r.Name()
		require.NotEmpty(t, name, "rule name should not be empty")
		require.False(t, strings.Contains(name, " "),
			"rule name %q should not contain spaces", name)
		require.False(t, seen[name], "duplicate rule name: %s", name)
		seen[name] = true
	}
	require.Equal(t, len(allRules), len(seen))
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

// TestRuleCreateIndexBlocking_RegexMismatch — stmtType says create_index
// but the SQL doesn't contain CREATE INDEX (forced classifier
// disagreement) → regex misses, returns nil.
func TestRuleCreateIndexBlocking_RegexMismatch(t *testing.T) {
	r := ruleCreateIndexBlocking{}
	got := r.Match("create_index", "SOMETHING ELSE")
	require.Nil(t, got)
}

type ruleCase struct {
	name     string
	driver   string
	sql      string
	wantHit  string // rule name that should fire (empty = no hit)
	wantRisk Risk
}

func TestLintRules(t *testing.T) {
	cases := []ruleCase{
		// DropColumn — universal data-loss.
		{
			name:     "drop-column-postgres",
			driver:   "postgres",
			sql:      "ALTER TABLE orders DROP COLUMN archive_reason;",
			wantHit:  "DropColumn",
			wantRisk: RiskDataLoss,
		},
		{
			name:     "drop-column-mysql",
			driver:   "mysql",
			sql:      "ALTER TABLE orders DROP COLUMN archive_reason;",
			wantHit:  "DropColumn",
			wantRisk: RiskDataLoss,
		},

		// AddColumnNotNullNoDefault — fires when no DEFAULT.
		{
			name:     "add-column-not-null-no-default",
			driver:   "postgres",
			sql:      "ALTER TABLE orders ADD COLUMN archive_reason VARCHAR(255) NOT NULL;",
			wantHit:  "AddColumnNotNullNoDefault",
			wantRisk: RiskLockAndFill,
		},
		{
			name:    "add-column-not-null-with-default-ok",
			driver:  "postgres",
			sql:     "ALTER TABLE orders ADD COLUMN archive_reason VARCHAR(255) NOT NULL DEFAULT '';",
			wantHit: "",
		},
		{
			name:    "add-column-nullable-ok",
			driver:  "postgres",
			sql:     "ALTER TABLE orders ADD COLUMN archive_reason VARCHAR(255);",
			wantHit: "",
		},

		// CreateIndexBlocking — driver-gated.
		{
			name:     "create-index-without-concurrently-postgres",
			driver:   "postgres",
			sql:      "CREATE INDEX idx_orders_status ON orders (status);",
			wantHit:  "CreateIndexBlocking",
			wantRisk: RiskLockTable,
		},
		{
			name:    "create-index-with-concurrently-postgres-ok",
			driver:  "postgres",
			sql:     "CREATE INDEX CONCURRENTLY idx_orders_status ON orders (status);",
			wantHit: "",
		},
		{
			name:    "create-index-sqlite-skipped",
			driver:  "sqlite",
			sql:     "CREATE INDEX idx_orders_status ON orders (status);",
			wantHit: "", // rule doesn't apply to sqlite
		},

		// DropTable / Truncate — universal data-loss.
		{
			name:     "drop-table",
			driver:   "postgres",
			sql:      "DROP TABLE orders;",
			wantHit:  "DropTable",
			wantRisk: RiskDataLoss,
		},
		{
			name:     "truncate",
			driver:   "postgres",
			sql:      "TRUNCATE orders;",
			wantHit:  "Truncate",
			wantRisk: RiskDataLoss,
		},

		// RenameColumn / RenameTable — app-incompat.
		{
			name:     "rename-column",
			driver:   "postgres",
			sql:      "ALTER TABLE orders RENAME COLUMN total TO amount_cents;",
			wantHit:  "RenameColumn",
			wantRisk: RiskAppIncompat,
		},
		{
			name:     "rename-table",
			driver:   "postgres",
			sql:      "ALTER TABLE orders RENAME TO customer_orders;",
			wantHit:  "RenameTable",
			wantRisk: RiskAppIncompat,
		},

		// AlterColumnType — lock + rewrite.
		{
			name:     "alter-column-type-postgres",
			driver:   "postgres",
			sql:      "ALTER TABLE orders ALTER COLUMN total TYPE BIGINT;",
			wantHit:  "AlterColumnType",
			wantRisk: RiskLockAndRewrite,
		},

		// AddPrimaryKey — lock.
		{
			name:     "add-primary-key",
			driver:   "postgres",
			sql:      "ALTER TABLE orders ADD CONSTRAINT pk_orders PRIMARY KEY (id);",
			wantHit:  "AddPrimaryKey",
			wantRisk: RiskLockTable,
		},

		// SELECT/INSERT are safe.
		{
			name:    "plain-select-safe",
			driver:  "postgres",
			sql:     "SELECT 1;",
			wantHit: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r, err := Lint(tc.driver, tc.sql)
			if err != nil {
				t.Fatalf("Lint returned error: %v", err)
			}
			if len(r.Statements) != 1 {
				t.Fatalf("expected exactly 1 statement, got %d", len(r.Statements))
			}
			s := r.Statements[0]
			if tc.wantHit == "" {
				if len(s.Warnings) != 0 {
					t.Fatalf("expected no warnings, got %#v", s.Warnings)
				}
				return
			}
			if len(s.Warnings) == 0 {
				t.Fatalf("expected rule %q to fire, got no warnings", tc.wantHit)
			}
			found := false
			for _, w := range s.Warnings {
				if w.Rule == tc.wantHit {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("rule %q did not fire; warnings = %#v", tc.wantHit, s.Warnings)
			}
			if s.Risk != tc.wantRisk {
				t.Errorf("Risk = %q, want %q", s.Risk, tc.wantRisk)
			}
		})
	}
}
