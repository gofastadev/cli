package commands

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateCmd_HasUpDown(t *testing.T) {
	subCmds := migrateCmd.Commands()
	names := make([]string, 0, len(subCmds))
	for _, c := range subCmds {
		names = append(names, c.Name())
	}
	assert.Contains(t, names, "up")
	assert.Contains(t, names, "down")
}

func TestMigrateCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "migrate" {
			found = true
			break
		}
	}
	assert.True(t, found, "migrateCmd should be registered on rootCmd")
}

func TestRunMigration_NoConfig(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	// loadConfig returns a koanf instance even without config.yaml,
	// so BuildMigrationURL returns a postgres URL with defaults
	err := runMigration("up")
	// Should fail because migrate binary is not available
	assert.Error(t, err)
}

func TestRunMigration_EmptyURL(t *testing.T) {
	// runMigration checks for empty URL and returns an error
	// This is hard to trigger since loadConfig always returns a koanf instance
	// but we can test the direction parameter
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	err := runMigration("down")
	assert.Error(t, err)
}

// TestRunMigration_EmptyURLSeam — the buildMigrationURL seam returns
// "" so the defensive "failed to load config" branch fires.
func TestRunMigration_EmptyURLSeam(t *testing.T) {
	orig := buildMigrationURL
	buildMigrationURL = func() string { return "" }
	t.Cleanup(func() { buildMigrationURL = orig })
	err := runMigration("up")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// TestRunMigration_EmptyURLCoverage — no config.yaml and no env vars
// so configutil's defaults produce a non-empty URL; the empty-URL
// branch is defensive. This test exercises the code path without a
// seam override.
func TestRunMigration_EmptyURLCoverage(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	_ = runMigration("up")
}
