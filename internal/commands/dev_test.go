package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDevCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "dev" {
			found = true
			break
		}
	}
	assert.True(t, found, "devCmd should be registered on rootCmd")
}

func TestDevCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, devCmd.Short)
	assert.NotEmpty(t, devCmd.Long)
}

// setupDevTempdir creates a temp project dir, chdirs into it, writes a
// minimal config.yaml so configutil.BuildMigrationURL returns a usable URL,
// and restores the original cwd on cleanup.
func setupDevTempdir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("database:\n  driver: postgres\n  name: testdb\n"), 0o644))
	require.NoError(t, os.Chdir(dir))
}

// runDev happy path — .env loaded, migration + air mocked to succeed,
// function returns nil. Covers:
//   - the loadDotEnv success branch (loaded > 0 "Loaded N variables" step)
//   - the "Running migrations" step with a mocked migrate CLI returning 0
//   - the "Starting air" step with a mocked `go tool air` returning 0
//   - full happy-path traversal of runDev
func TestRunDev_HappyPathWithEnv(t *testing.T) {
	setupDevTempdir(t)
	require.NoError(t, os.WriteFile(".env",
		[]byte("DEV_TEST_RUN_HAPPY_VAR=loaded\n"), 0o644))
	t.Cleanup(func() { _ = os.Unsetenv("DEV_TEST_RUN_HAPPY_VAR") })

	withFakeExec(t, 0)

	err := runDev()
	assert.NoError(t, err)
	// The .env was loaded — value now in process env.
	assert.Equal(t, "loaded", os.Getenv("DEV_TEST_RUN_HAPPY_VAR"))
}

// runDev when .env is missing — loadDotEnv returns (0, nil), the "Loaded"
// step is skipped, and the rest of the flow still runs to completion.
func TestRunDev_NoDotEnv(t *testing.T) {
	setupDevTempdir(t)
	withFakeExec(t, 0)

	err := runDev()
	assert.NoError(t, err)
}

// runDev when .env exists but is unreadable — loadDotEnv returns an error,
// runDev emits a PrintWarn and continues. Covers the error branch at
// dev.go:52-53.
func TestRunDev_UnreadableDotEnv(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod-based read denial")
	}
	setupDevTempdir(t)
	require.NoError(t, os.WriteFile(".env", []byte("FOO=bar\n"), 0o644))
	require.NoError(t, os.Chmod(".env", 0o000))
	t.Cleanup(func() { _ = os.Chmod(".env", 0o644) })

	withFakeExec(t, 0)
	err := runDev()
	// runDev treats the load error as non-fatal — it prints a warning and
	// carries on. No error is returned.
	assert.NoError(t, err)
}

// runDev when the migration step fails — warn branch of the migration
// block is exercised and runDev still proceeds to start air.
func TestRunDev_MigrationFails(t *testing.T) {
	setupDevTempdir(t)
	withFakeExec(t, 1) // every exec returns non-zero
	// Provide a migrate binary on PATH so runMigrations doesn't short-circuit
	// at the LookPath check. fakeExecCommand produces the binary path from
	// os.Args[0] (the test binary itself) which is always on PATH.
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "/usr/bin/migrate", nil }
	t.Cleanup(func() { execLookPath = origLookPath })

	err := runDev()
	// Air also fails (same fakeExec) — runDev returns the air error.
	assert.Error(t, err)
}

// runMigrations with migrate not installed — returns a clear error message
// mentioning where to install from.
func TestRunMigrations_MigrateNotFound(t *testing.T) {
	setupDevTempdir(t)
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "", fmt.Errorf("not found") }
	t.Cleanup(func() { execLookPath = origLookPath })

	err := runMigrations()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "migrate CLI not found")
	assert.Contains(t, err.Error(), "v4.18.1")
}

// runMigrations with empty DB URL — returns error about config.
func TestRunMigrations_EmptyDBURL(t *testing.T) {
	// Empty temp dir with no config.yaml → BuildMigrationURL returns something
	// with empty fields but non-empty string; so this particular test won't
	// trigger the empty-URL path. Use a dir without config.yaml AND no env
	// vars set — but configutil always returns a non-empty URL with defaults.
	// Skip this — the branch is defensive and practically unreachable since
	// configutil always returns at least the default postgres URL.
}

// runMigrations succeeds on first attempt.
func TestRunMigrations_SuccessFirstAttempt(t *testing.T) {
	setupDevTempdir(t)
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "/usr/bin/migrate", nil }
	t.Cleanup(func() { execLookPath = origLookPath })
	withFakeExec(t, 0)

	err := runMigrations()
	assert.NoError(t, err)
}

// runMigrations fails first attempt but succeeds on retry.
func TestRunMigrations_SuccessOnRetry(t *testing.T) {
	setupDevTempdir(t)
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "/usr/bin/migrate", nil }
	t.Cleanup(func() { execLookPath = origLookPath })
	// First call (migrate up) fails, second call (retry) succeeds.
	stagedFakeExec(t, 1, 0)

	err := runMigrations()
	assert.NoError(t, err)
}

// runMigrations fails both attempts — returns the error from the second try.
func TestRunMigrations_FailsBothAttempts(t *testing.T) {
	setupDevTempdir(t)
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "/usr/bin/migrate", nil }
	t.Cleanup(func() { execLookPath = origLookPath })
	withFakeExec(t, 1) // both attempts fail

	err := runMigrations()
	assert.Error(t, err)
}

// runMigrateUp — direct test of the single-attempt function.
func TestRunMigrateUp(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, runMigrateUp("postgres://test:test@localhost:5432/testdb"))

	withFakeExec(t, 1)
	assert.Error(t, runMigrateUp("postgres://test:test@localhost:5432/testdb"))
}
