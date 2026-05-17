package sqllint

import (
	"strings"
	"testing"
)

// ----- splitter ----------------------------------------------------------

func TestSplitStatements_BasicSemicolonDelimited(t *testing.T) {
	in := "CREATE TABLE a (id int);CREATE TABLE b (id int);"
	got, err := SplitStatements(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (got %#v)", len(got), got)
	}
}

func TestSplitStatements_IgnoresSemicolonInStringLiteral(t *testing.T) {
	in := "INSERT INTO t (msg) VALUES ('hi; there');SELECT 1;"
	got, err := SplitStatements(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (got %#v)", len(got), got)
	}
	if !strings.Contains(got[0], "'hi; there'") {
		t.Errorf("string literal lost: %q", got[0])
	}
}

func TestSplitStatements_IgnoresSemicolonInDollarQuoteBlock(t *testing.T) {
	in := `
DO $$
BEGIN
  RAISE NOTICE 'one;two';
END;
$$;
SELECT 1;
`
	got, err := SplitStatements(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (got %#v)", len(got), got)
	}
}

func TestSplitStatements_IgnoresSemicolonInTaggedDollarQuoteBlock(t *testing.T) {
	in := "CREATE FUNCTION f() RETURNS void AS $tag$BEGIN x; END;$tag$ LANGUAGE plpgsql;SELECT 1;"
	got, err := SplitStatements(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (got %#v)", len(got), got)
	}
}

func TestSplitStatements_IgnoresSemicolonInLineComment(t *testing.T) {
	in := "SELECT 1; -- end of stmt; really\nSELECT 2;"
	got, err := SplitStatements(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (got %#v)", len(got), got)
	}
}

func TestSplitStatements_IgnoresSemicolonInBlockComment(t *testing.T) {
	in := "SELECT 1; /* a; b; c */ SELECT 2;"
	got, err := SplitStatements(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (got %#v)", len(got), got)
	}
}

func TestSplitStatements_HandlesDoubledQuoteEscape(t *testing.T) {
	in := "INSERT INTO t (msg) VALUES ('it''s ok');SELECT 1;"
	got, err := SplitStatements(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (got %#v)", len(got), got)
	}
}

func TestSplitStatements_UnterminatedStringErrors(t *testing.T) {
	_, err := SplitStatements("SELECT 'oops")
	if err == nil {
		t.Fatal("expected error on unterminated string, got nil")
	}
}

func TestSplitStatements_UnterminatedDollarBlockErrors(t *testing.T) {
	_, err := SplitStatements("DO $$ BEGIN RAISE NOTICE 'x' END;")
	if err == nil {
		t.Fatal("expected error on unterminated dollar-quote, got nil")
	}
}

func TestSplitStatements_NoTrailingSemicolonOK(t *testing.T) {
	in := "SELECT 1"
	got, err := SplitStatements(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
}

func TestSplitStatements_EmptyInput(t *testing.T) {
	got, err := SplitStatements("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

// ----- rules -------------------------------------------------------------

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
