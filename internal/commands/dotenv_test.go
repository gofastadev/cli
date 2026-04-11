package commands

import (
	"os"
	"path/filepath"
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
