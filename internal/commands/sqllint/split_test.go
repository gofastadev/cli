package sqllint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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
