package commands

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "serve" {
			found = true
			break
		}
	}
	assert.True(t, found, "serveCmd should be registered on rootCmd")
}

func TestServeCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, serveCmd.Short)
}

func TestServeCmd_RunE_Fails(t *testing.T) {
	// serveCmd delegates to `go run ./app/main serve` which won't exist
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	err := serveCmd.RunE(serveCmd, nil)
	assert.Error(t, err)
}

// TestServeCmd_LoadsDotEnv — regression: serve must load .env before
// spawning the project binary so the framework's pkg/config can read
// project-prefixed env vars (DATABASE_USER/PASSWORD/NAME). Without
// this, the spawned `go run ./app/main serve` child would only see
// the parent's bare environment and fail to connect to the DB.
func TestServeCmd_LoadsDotEnv(t *testing.T) {
	chdirTemp(t)
	const probe = "GOFASTA_SERVE_DOTENV_PROBE"
	require.NoError(t, os.WriteFile(".env", []byte(probe+"=loaded\n"), 0o644))
	t.Cleanup(func() { _ = os.Unsetenv(probe) })

	withFakeExec(t, 0)
	require.NoError(t, serveCmd.RunE(serveCmd, nil))
	assert.Equal(t, "loaded", os.Getenv(probe),
		"serveCmd must call loadDotEnv before spawning the project binary")
}
