package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withFakeMigrateVersion overrides execCommand so subsequent calls
// (notably `migrate version` from checkPendingMigrations) return the
// supplied stdout and exit code, via the standard TestHelperProcess
// fake-subprocess mechanism. Restored on test cleanup.
func withFakeMigrateVersion(t *testing.T, output string, exitCode int) {
	t.Helper()
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := make([]string, 0, 3+len(args))
		cs = append(cs, "-test.run=TestHelperProcess", "--", name)
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"="+strconv.Itoa(exitCode),
			"GOFASTA_FAKE_STDOUT="+output,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })
}

func TestStatusCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "status" {
			found = true
			break
		}
	}
	assert.True(t, found, "statusCmd should be registered on rootCmd")
}

// TestCheckWireDrift_NoWireGen — projects without wire_gen.go skip.
func TestCheckWireDrift_NoWireGen(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	check := checkWireDrift()
	assert.Equal(t, "skip", check.Status)
}

func TestCheckWireDrift_Stale(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	diDir := filepath.Join("app", "di")
	require.NoError(t, os.MkdirAll(diDir, 0755))
	wireGen := filepath.Join(diDir, "wire_gen.go")
	require.NoError(t, os.WriteFile(wireGen, []byte("package di"), 0644))
	past := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(wireGen, past, past))

	// Newer input file.
	require.NoError(t, os.WriteFile(filepath.Join(diDir, "wire.go"),
		[]byte("package di"), 0644))

	check := checkWireDrift()
	assert.Equal(t, "drift", check.Status)
	assert.Contains(t, check.Message, "gofasta wire")
}

func TestCheckSwaggerDrift_NoSwagger(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	check := checkSwaggerDrift()
	assert.Equal(t, "skip", check.Status)
}

// TestCheckPendingMigrations_DBUnreachable — migrations defined on
// disk but the DB can't be reached → "skip" with a clear message
// (no false "pending" claim).
func TestCheckPendingMigrations_DBUnreachable(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	mDir := filepath.Join("db", "migrations")
	require.NoError(t, os.MkdirAll(mDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000001_init.up.sql"), []byte(""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000001_init.down.sql"), []byte(""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000002_users.up.sql"), []byte(""), 0644))

	check := checkPendingMigrations()
	assert.Equal(t, "skip", check.Status)
	assert.Contains(t, check.Message, "2 migration(s) defined")
	assert.Contains(t, check.Message, "could not check applied state")
}

func TestCheckPendingMigrations_NoDir(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	check := checkPendingMigrations()
	assert.Equal(t, "skip", check.Status)
}

// TestCheckPendingMigrations_AllApplied — when migrate version reports
// the same version as the highest defined .up.sql, status is "ok".
// This is the regression case from the user report: all migrations
// applied via server start, but the old code falsely warned "pending".
func TestCheckPendingMigrations_AllApplied(t *testing.T) {
	chdirTemp(t)
	mDir := filepath.Join("db", "migrations")
	require.NoError(t, os.MkdirAll(mDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000001_init.up.sql"), []byte(""), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000002_users.up.sql"), []byte(""), 0644))

	withFakeMigrateVersion(t, "2\n", 0)
	check := checkPendingMigrations()
	assert.Equal(t, "ok", check.Status)
	assert.Contains(t, check.Message, "2 migration(s) applied")
	assert.Contains(t, check.Message, "current version: 2")
}

// TestCheckPendingMigrations_SomePending — current applied version is
// less than the latest defined: status is "drift" with the count of
// genuinely pending migrations (not just the file count).
func TestCheckPendingMigrations_SomePending(t *testing.T) {
	chdirTemp(t)
	mDir := filepath.Join("db", "migrations")
	require.NoError(t, os.MkdirAll(mDir, 0755))
	for _, name := range []string{
		"000001_init.up.sql", "000002_users.up.sql",
		"000003_orders.up.sql", "000004_audit.up.sql",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(mDir, name), nil, 0644))
	}

	withFakeMigrateVersion(t, "2\n", 0)
	check := checkPendingMigrations()
	assert.Equal(t, "drift", check.Status)
	assert.Contains(t, check.Message, "2 migration(s) pending")
	assert.Contains(t, check.Message, "current: 2")
	assert.Contains(t, check.Message, "latest defined: 4")
}

// TestCheckPendingMigrations_DirtyState — migrate reports "X (dirty)"
// when a previous migration failed mid-step. Status should be "warn"
// with a clear remediation pointer.
func TestCheckPendingMigrations_DirtyState(t *testing.T) {
	chdirTemp(t)
	mDir := filepath.Join("db", "migrations")
	require.NoError(t, os.MkdirAll(mDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000001_init.up.sql"), nil, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000002_users.up.sql"), nil, 0644))

	withFakeMigrateVersion(t, "1 (dirty)\n", 0)
	check := checkPendingMigrations()
	assert.Equal(t, "warn", check.Status)
	assert.Contains(t, check.Message, "dirty")
	assert.Contains(t, check.Message, "1")
}

// TestCheckPendingMigrations_NoMigrationApplied — fresh DB, migrate
// version returns "no migration". Every defined migration is pending.
func TestCheckPendingMigrations_NoMigrationApplied(t *testing.T) {
	chdirTemp(t)
	mDir := filepath.Join("db", "migrations")
	require.NoError(t, os.MkdirAll(mDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000001_init.up.sql"), nil, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(mDir, "000002_users.up.sql"), nil, 0644))

	withFakeMigrateVersion(t, "no migration\n", 1)
	check := checkPendingMigrations()
	assert.Equal(t, "drift", check.Status)
	assert.Contains(t, check.Message, "2 migration(s) pending")
}

// TestStatusCmd_RunE — exercises the Cobra RunE wrapper.
func TestStatusCmd_RunE(t *testing.T) {
	chdirTemp(t)
	_ = statusCmd.RunE(statusCmd, nil)
}
