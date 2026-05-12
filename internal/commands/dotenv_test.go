package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDotEnv_MissingFile(t *testing.T) {
	n, err := loadDotEnv("/nonexistent/dot.env")
	assert.NoError(t, err, "missing file should be a silent no-op")
	assert.Equal(t, 0, n)
}

func TestLoadDotEnv_BasicKeyValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(path, []byte("FOO_BAR=hello\n"), 0o644))
	t.Setenv("FOO_BAR", "") // clear any inherited value
	_ = os.Unsetenv("FOO_BAR")

	n, err := loadDotEnv(path)
	assert.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, "hello", os.Getenv("FOO_BAR"))
	t.Cleanup(func() { _ = os.Unsetenv("FOO_BAR") })
}

func TestLoadDotEnv_CommentsAndBlankLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `# This is a comment
# Another comment

LOADENV_A=1

# Inline comments on the value line are NOT stripped — they become part
# of the value. Skip that case.
LOADENV_B=2
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	_ = os.Unsetenv("LOADENV_A")
	_ = os.Unsetenv("LOADENV_B")

	n, err := loadDotEnv(path)
	assert.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, "1", os.Getenv("LOADENV_A"))
	assert.Equal(t, "2", os.Getenv("LOADENV_B"))
	t.Cleanup(func() {
		_ = os.Unsetenv("LOADENV_A")
		_ = os.Unsetenv("LOADENV_B")
	})
}

func TestLoadDotEnv_QuotedValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `LOADENV_DQUOTED="value with spaces"
LOADENV_SQUOTED='single quoted'
LOADENV_UNQUOTED=plain
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	_ = os.Unsetenv("LOADENV_DQUOTED")
	_ = os.Unsetenv("LOADENV_SQUOTED")
	_ = os.Unsetenv("LOADENV_UNQUOTED")

	n, err := loadDotEnv(path)
	assert.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, "value with spaces", os.Getenv("LOADENV_DQUOTED"))
	assert.Equal(t, "single quoted", os.Getenv("LOADENV_SQUOTED"))
	assert.Equal(t, "plain", os.Getenv("LOADENV_UNQUOTED"))
	t.Cleanup(func() {
		_ = os.Unsetenv("LOADENV_DQUOTED")
		_ = os.Unsetenv("LOADENV_SQUOTED")
		_ = os.Unsetenv("LOADENV_UNQUOTED")
	})
}

func TestLoadDotEnv_ValueWithEqualsSign(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(path, []byte("LOADENV_DSN=host=localhost port=5433\n"), 0o644))
	_ = os.Unsetenv("LOADENV_DSN")

	n, err := loadDotEnv(path)
	assert.NoError(t, err)
	assert.Equal(t, 1, n)
	// Only the first "=" is a separator; everything after it is the value.
	assert.Equal(t, "host=localhost port=5433", os.Getenv("LOADENV_DSN"))
	t.Cleanup(func() { _ = os.Unsetenv("LOADENV_DSN") })
}

func TestLoadDotEnv_ShellWinsOverFile(t *testing.T) {
	// Pre-existing shell env vars must NOT be overwritten by the file —
	// matches godotenv's default semantics and the principle of least
	// surprise for users who override values in their terminal.
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(path, []byte("LOADENV_WINS=from-file\n"), 0o644))
	t.Setenv("LOADENV_WINS", "from-shell")

	n, err := loadDotEnv(path)
	assert.NoError(t, err)
	assert.Equal(t, 0, n, "already-set var should not be counted")
	assert.Equal(t, "from-shell", os.Getenv("LOADENV_WINS"))
}

func TestLoadDotEnv_IgnoresMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `NOT_A_KEY_VALUE_PAIR
=missing-key
LOADENV_GOOD=ok
   LOADENV_WHITESPACE  =  trimmed
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	_ = os.Unsetenv("LOADENV_GOOD")
	_ = os.Unsetenv("LOADENV_WHITESPACE")

	n, err := loadDotEnv(path)
	assert.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, "ok", os.Getenv("LOADENV_GOOD"))
	assert.Equal(t, "trimmed", os.Getenv("LOADENV_WHITESPACE"))
	t.Cleanup(func() {
		_ = os.Unsetenv("LOADENV_GOOD")
		_ = os.Unsetenv("LOADENV_WHITESPACE")
	})
}

func TestParseDotEnvLine_LeadingEqualsRejected(t *testing.T) {
	// A line like "   =value" trims to "=value". The `=` is at position 0
	// so eq==0 and the function returns ok=false via the eq<=0 guard.
	_, _, ok := parseDotEnvLine("   \t  =val")
	assert.False(t, ok, "line with no key before `=` should be rejected")
}

func TestLoadDotEnv_HandlesHugeLine(t *testing.T) {
	// The original implementation used bufio.Scanner, whose default
	// token limit is 64KB — a single line longer than that would
	// trigger ErrTooLong and the file would be unusable. The current
	// implementation splits on '\n' directly so there is no per-line
	// limit. This test pins that contract: a 200KB value loads
	// successfully and lands in os.Setenv as written.
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	bigValue := strings.Repeat("x", 200000)
	giant := "BIG_LINE=" + bigValue + "\n"
	require.NoError(t, os.WriteFile(path, []byte(giant), 0o644))
	_ = os.Unsetenv("BIG_LINE")
	t.Cleanup(func() { _ = os.Unsetenv("BIG_LINE") })

	count, err := loadDotEnv(path)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "huge line should still load as a single var")
	assert.Equal(t, bigValue, os.Getenv("BIG_LINE"))
}

func TestLoadDotEnv_UnreadableFile(t *testing.T) {
	// Verify the error path for a file that exists but can't be opened.
	// On unix we simulate this by creating a file in a directory that is
	// read-only and then chmod'ing it 0, which makes open(2) return EACCES.
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod-based read denial")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(path, []byte("FOO=bar\n"), 0o644))
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	_, err := loadDotEnv(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "open")
}

// ── managed-block round trip ──────────────────────────────────────────

// TestWriteManagedBlock_AppendsAndIsIdempotent — first call adds the
// block to a user-authored .env; second call REPLACES it (no nesting,
// no drift). User-authored lines outside the block must be preserved
// byte-for-byte, including comments and blank lines.
func TestWriteManagedBlock_AppendsAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	original := "# user comment\nUSER_VAR=keep\n\n# another section\nOTHER_VAR=stay\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o644))

	require.NoError(t, writeManagedBlock(path, map[string]string{
		"DATA_DATABASE_HOST": "neon.tech",
		"DATA_DATABASE_PORT": "5432",
	}))

	first, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(first)
	assert.Contains(t, got, "USER_VAR=keep", "user content must survive")
	assert.Contains(t, got, "OTHER_VAR=stay")
	assert.Contains(t, got, managedBlockBegin)
	assert.Contains(t, got, "DATA_DATABASE_HOST=neon.tech")
	assert.Contains(t, got, "DATA_DATABASE_PORT=5432")
	assert.Contains(t, got, managedBlockEnd)

	// Second write with DIFFERENT keys must replace the previous block,
	// not append a second one.
	require.NoError(t, writeManagedBlock(path, map[string]string{
		"DATA_DATABASE_HOST":    "different.host",
		"DATA_DATABASE_SSLMODE": "require",
	}))

	second, err := os.ReadFile(path)
	require.NoError(t, err)
	got = string(second)
	assert.Equal(t, 1, strings.Count(got, managedBlockBegin), "must not nest blocks")
	assert.Equal(t, 1, strings.Count(got, managedBlockEnd))
	assert.NotContains(t, got, "neon.tech", "old managed values must be gone")
	assert.NotContains(t, got, "5432", "old managed values must be gone")
	assert.Contains(t, got, "DATA_DATABASE_HOST=different.host")
	assert.Contains(t, got, "DATA_DATABASE_SSLMODE=require")
	assert.Contains(t, got, "USER_VAR=keep", "user content still intact")
}

// TestWriteManagedBlock_EmptyMapRemovesBlock — passing an empty map is
// the "revert" affordance: any existing block is removed and the file
// returns to user-only content.
func TestWriteManagedBlock_EmptyMapRemovesBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	require.NoError(t, writeManagedBlock(path, map[string]string{"X_K": "v"}))
	require.NoError(t, writeManagedBlock(path, map[string]string{}))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(got), managedBlockBegin)
	assert.NotContains(t, string(got), "X_K=")
}

// TestLoadDotEnv_ManagedBlockWinsOverRest — when both the managed
// block AND the rest of the file set the same key, the managed
// value wins. This is the load-side guarantee that makes persistence
// useful: a saved override beats the user's hand-edited default.
func TestLoadDotEnv_ManagedBlockWinsOverRest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "DATA_DATABASE_HOST=localhost\n" +
		managedBlockBegin + "\n" +
		"DATA_DATABASE_HOST=neon.example.com\n" +
		managedBlockEnd + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	_ = os.Unsetenv("DATA_DATABASE_HOST")
	t.Cleanup(func() { _ = os.Unsetenv("DATA_DATABASE_HOST") })

	_, err := loadDotEnv(path)
	require.NoError(t, err)
	assert.Equal(t, "neon.example.com", os.Getenv("DATA_DATABASE_HOST"),
		"managed-block value must override the un-managed entry above it")
}

// TestLoadDotEnv_ShellStillWinsOverManagedBlock — explicit shell
// exports must keep beating the file, including the managed block.
// Otherwise persisting a stale override to disk would silently
// override a developer who deliberately exported a different value
// in their current shell.
func TestLoadDotEnv_ShellStillWinsOverManagedBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := managedBlockBegin + "\n" +
		"DATA_DATABASE_HOST=from-managed\n" +
		managedBlockEnd + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	t.Setenv("DATA_DATABASE_HOST", "from-shell")

	_, err := loadDotEnv(path)
	require.NoError(t, err)
	assert.Equal(t, "from-shell", os.Getenv("DATA_DATABASE_HOST"))
}

// TestQuoteDotEnvValue_QuotesOnlyWhenNeeded — plain values pass
// through unquoted; values with whitespace/special chars get wrapped.
func TestQuoteDotEnvValue_QuotesOnlyWhenNeeded(t *testing.T) {
	// Plain backslashes pass through unquoted because parseDotEnvLine
	// doesn't process escape sequences — a `\n` in the value reads as
	// the literal two-char sequence, not a newline. Only chars a naive
	// parser would mis-interpret (space, tab, '#', '"', actual newline)
	// trigger quoting.
	cases := map[string]string{
		"":                                    "",
		"plain":                               "plain",
		"with spaces":                         `"with spaces"`,
		"with\ttab":                           "\"with\ttab\"",
		"has#hash":                            `"has#hash"`,
		`has"quote`:                           `"has\"quote"`,
		`has\backslash`:                       `has\backslash`,
		"postgres://u:p@h/db?sslmode=require": "postgres://u:p@h/db?sslmode=require",
	}
	for in, want := range cases {
		assert.Equal(t, want, quoteDotEnvValue(in), "in=%q", in)
	}
}
