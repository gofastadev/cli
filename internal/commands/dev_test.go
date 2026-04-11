package commands

import (
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
func setupDevTempdir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("database:\n  driver: postgres\n  name: testdb\n"), 0o644))
	require.NoError(t, os.Chdir(dir))
	return dir
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

	err := runDev()
	// Air also fails (same fakeExec) — runDev returns the air error.
	// The important coverage is that the migration warn branch fired before
	// reaching the air invocation.
	assert.Error(t, err)
}
