package commands

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "init" {
			found = true
			break
		}
	}
	assert.True(t, found, "initCmd should be registered on rootCmd")
}

func TestInitCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, initCmd.Short)
	assert.NotEmpty(t, initCmd.Long)
}

// TestRunInit_ConfigLoadFailedBranch — buildMigrationURL seam returns
// an empty string → the "Could not load config" warning path fires.
func TestRunInit_ConfigLoadFailedBranch(t *testing.T) {
	chdirTemp(t)
	orig := buildMigrationURL
	buildMigrationURL = func() string { return "" }
	t.Cleanup(func() { buildMigrationURL = orig })
	withFakeExec(t, 0)
	assert.NoError(t, runInit())
}

// TestRunInit_ConfigLoadFailed — no config.yaml so configutil returns
// an empty URL which triggers the else branch. runInit tolerates the
// missing config file.
func TestRunInit_ConfigLoadFailed(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	_ = runInit()
}

// TestRunInit_LoadsDotEnvBeforeMigrations — regression: Step 6 of
// runInit must load .env before building the migrate URL, otherwise
// the migration step sees empty credentials and dials the wrong port.
// The .env file already exists (Step 1's no-op branch).
func TestRunInit_LoadsDotEnvBeforeMigrations(t *testing.T) {
	chdirTemp(t)
	const probe = "GOFASTA_INIT_DOTENV_PROBE"
	require.NoError(t, os.WriteFile(".env", []byte(probe+"=loaded\n"), 0o644))
	t.Cleanup(func() { _ = os.Unsetenv(probe) })

	withFakeExec(t, 0)
	_ = runInit()
	assert.Equal(t, "loaded", os.Getenv(probe),
		"runInit must call loadDotEnv before building the migrate URL")
}
