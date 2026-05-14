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

// ── mergeIntoDotEnv ───────────────────────────────────────────────────

// TestMergeIntoDotEnv_ReplacesExistingKeyInPlace — an existing key in
// the file has its value swapped on the SAME line. Surrounding
// content (comments, ordering of unrelated keys, blank lines) is
// preserved.
func TestMergeIntoDotEnv_ReplacesExistingKeyInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	original := "# header comment\n" +
		"DATA_DATABASE_HOST=localhost\n" +
		"DATA_DATABASE_PORT=5433\n" +
		"\n" +
		"# unrelated section\n" +
		"OTHER_VAR=keep\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o644))

	require.NoError(t, mergeIntoDotEnv(path, map[string]string{
		"DATA_DATABASE_HOST": "neon.tech",
		"DATA_DATABASE_PORT": "5432",
	}))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	want := "# header comment\n" +
		"DATA_DATABASE_HOST=neon.tech\n" +
		"DATA_DATABASE_PORT=5432\n" +
		"\n" +
		"# unrelated section\n" +
		"OTHER_VAR=keep\n"
	assert.Equal(t, want, string(got),
		"existing keys must be edited in place, all other content byte-identical")
}

// TestMergeIntoDotEnv_AppendsNewKeysAtEnd — keys not already present
// are appended at the end of the file, in sorted order (stable diffs).
func TestMergeIntoDotEnv_AppendsNewKeysAtEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(path, []byte("EXISTING=keep\n"), 0o644))

	require.NoError(t, mergeIntoDotEnv(path, map[string]string{
		"DATA_DATABASE_SSLMODE": "require",
		"DATA_DATABASE_USER":    "neon_user",
	}))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	// A blank line separates user content from appended keys —
	// cosmetic only, but pinned here so accidental reflow doesn't
	// quietly drift.
	want := "EXISTING=keep\n" +
		"\n" +
		"DATA_DATABASE_SSLMODE=require\n" +
		"DATA_DATABASE_USER=neon_user\n"
	assert.Equal(t, want, string(got))
}

// TestMergeIntoDotEnv_RemovesDuplicatesOfPersistedKeys — if a persist-
// target key appears twice in the file (which can happen if a user
// hand-added a copy of an existing key, or after legacy managed-block
// migration), the merge keeps only the first occurrence with the new
// value. Duplicates of keys NOT being persisted are LEFT alone.
func TestMergeIntoDotEnv_RemovesDuplicatesOfPersistedKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	original := "DATA_DATABASE_HOST=first\n" +
		"OTHER=untouched-first\n" +
		"DATA_DATABASE_HOST=second\n" +
		"OTHER=untouched-second\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o644))

	require.NoError(t, mergeIntoDotEnv(path, map[string]string{
		"DATA_DATABASE_HOST": "merged",
	}))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	gotStr := string(got)
	assert.Equal(t, 1, strings.Count(gotStr, "DATA_DATABASE_HOST="),
		"persist-target duplicates must be collapsed to one occurrence")
	assert.Contains(t, gotStr, "DATA_DATABASE_HOST=merged")
	// Non-target duplicates must survive untouched.
	assert.Equal(t, 2, strings.Count(gotStr, "OTHER="),
		"non-persist-target duplicates must be left alone")
}

// TestMergeIntoDotEnv_MigratesLegacyManagedBlock — files saved by the
// PREVIOUS implementation contain `# >>> auto-managed` markers. On
// next merge, those markers are stripped and the inner lines fold
// into the regular file body — then the in-place edit runs as
// normal. End result: no markers, no duplicate keys.
func TestMergeIntoDotEnv_MigratesLegacyManagedBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	original := "DATA_DATABASE_HOST=localhost\n" +
		"DATA_DATABASE_PORT=5433\n" +
		"\n" +
		managedBlockBegin + "\n" +
		"DATA_DATABASE_HOST=stale-from-block\n" +
		"DATA_DATABASE_NAME=stale_db\n" +
		managedBlockEnd + "\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o644))

	require.NoError(t, mergeIntoDotEnv(path, map[string]string{
		"DATA_DATABASE_HOST": "fresh.example.com",
		"DATA_DATABASE_NAME": "fresh_db",
	}))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	gotStr := string(got)
	assert.NotContains(t, gotStr, managedBlockBegin, "markers must be gone")
	assert.NotContains(t, gotStr, managedBlockEnd)
	assert.NotContains(t, gotStr, "stale-from-block", "legacy values must be replaced")
	assert.Equal(t, 1, strings.Count(gotStr, "DATA_DATABASE_HOST="),
		"only one HOST line should remain after migration")
	assert.Contains(t, gotStr, "DATA_DATABASE_HOST=fresh.example.com")
	assert.Contains(t, gotStr, "DATA_DATABASE_NAME=fresh_db")
	// The PORT key wasn't in this merge call — must survive its
	// original value untouched.
	assert.Contains(t, gotStr, "DATA_DATABASE_PORT=5433")
}

// TestMergeIntoDotEnv_EmptyKVsIsNoOp — passing an empty map leaves
// the file untouched.
func TestMergeIntoDotEnv_EmptyKVsIsNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(path, []byte("ORIGINAL=here\n"), 0o644))

	require.NoError(t, mergeIntoDotEnv(path, map[string]string{}))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "ORIGINAL=here\n", string(got))
}

// TestMergeIntoDotEnv_CreatesFileWhenMissing — a fresh project might
// not have a `.env` yet; the merge creates one rather than erroring.
func TestMergeIntoDotEnv_CreatesFileWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	// Note: file does not exist yet.

	require.NoError(t, mergeIntoDotEnv(path, map[string]string{
		"DATA_DATABASE_HOST": "neon.tech",
	}))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "DATA_DATABASE_HOST=neon.tech\n", string(got))
}

// TestLoadDotEnv_ManagedBlockBackwardCompat — files written by the
// PREVIOUS managed-block implementation are still loaded correctly:
// the managed block's KVs are applied first (so they win over
// un-managed lines above), preserving the contract a user might
// rely on between updating the CLI and saving via the menu again.
// After the next save, mergeIntoDotEnv strips the markers and
// promotes the values to in-place lines — this test pins the
// transition window.
func TestLoadDotEnv_ManagedBlockBackwardCompat(t *testing.T) {
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
		"legacy managed-block value must beat the un-managed entry above it")
}

// TestLoadDotEnv_ShellStillWinsOverManagedBlock — explicit shell
// exports beat the file, including a legacy managed block. Pinning
// this so persisted overrides can't shadow a developer's deliberate
// shell export.
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
