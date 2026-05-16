package commands

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// status.go — branches around readDefinedMigrations + readAppliedMigrationVersion
// ─────────────────────────────────────────────────────────────────────

// TestReadDefinedMigrations_SkipsMalformed — files without an underscore
// (idx<=0) and files whose numeric prefix doesn't parse are silently
// skipped; the rest still parse.
func TestReadDefinedMigrations_SkipsMalformed(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"000001_init.up.sql",           // valid
		"notanumber_x.up.sql",          // Atoi err
		"badname.up.sql",               // no underscore  → idx <= 0
		"000002_users.up.sql",          // valid
		"000003_extras.down.sql",       // wrong suffix → ignored
		"000001_init.duplicate.up.sql", // dedup via seen map
	}
	for _, f := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), nil, 0o644))
	}
	got, err := readDefinedMigrations(dir)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, got)
}

// TestReadDefinedMigrations_ReadDirError — missing dir surfaces the
// os.ReadDir error.
func TestReadDefinedMigrations_ReadDirError(t *testing.T) {
	_, err := readDefinedMigrations("/nonexistent/path/x")
	require.Error(t, err)
}

// TestReadAppliedMigrationVersion_UnexpectedOutputErrors — migrate
// version returns text that doesn't start with a number → parse error.
func TestReadAppliedMigrationVersion_UnexpectedOutputErrors(t *testing.T) {
	chdirTemp(t)
	withFakeMigrateOutput(t, "garbage text\n", 0)
	_, _, err := readAppliedMigrationVersion("db/migrations", "postgres://stub")
	require.Error(t, err)
}

// TestRunStatus_DriftReturnsError — when checkWireDrift reports drift,
// runStatus exits with CodeVerifyFailed and a non-nil error.
func TestRunStatus_DriftReturnsError(t *testing.T) {
	chdirTemp(t)
	// Create wire_gen.go older than a sibling .go file → drift.
	wireDir := filepath.Join("app", "di")
	require.NoError(t, os.MkdirAll(wireDir, 0o755))
	wireGen := filepath.Join(wireDir, "wire_gen.go")
	require.NoError(t, os.WriteFile(wireGen, []byte("// generated\n"), 0o644))
	// Make a sibling .go file with a newer mtime.
	sibling := filepath.Join(wireDir, "wire.go")
	require.NoError(t, os.WriteFile(sibling, []byte("// input\n"), 0o644))
	// Touch wire_gen.go BACK in time.
	old := time.Now().Add(-365 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(wireGen, old, old))

	_ = captureStdout(t, func() {
		err := runStatus()
		require.Error(t, err)
	})
}
