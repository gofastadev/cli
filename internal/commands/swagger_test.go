package commands

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSwaggerCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "swagger" {
			found = true
			break
		}
	}
	assert.True(t, found, "swaggerCmd should be registered on rootCmd")
}

func TestSwaggerCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, swaggerCmd.Short)
}

func TestSwaggerCmd_RunE_Fails(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	err := swaggerCmd.RunE(swaggerCmd, nil)
	assert.Error(t, err)
}

// TestRunSwagger_TextMode_Success — text mode (no --json) streams
// swag's stdout/stderr straight to the user's terminal and returns
// nil on success. Exercises the non-JSON branch of runSwagger that
// json_compliance_test.go's coverage misses.
func TestRunSwagger_TextMode_Success(t *testing.T) {
	withFakeExec(t, 0)
	require.NoError(t, runSwagger())
}

// TestRunSwagger_TextMode_Failure — text mode + swag exit code 1 →
// runSwagger returns the wrapped *exec.ExitError. Exercises the
// return swag.Run() error path that the JSON-mode tests don't touch.
func TestRunSwagger_TextMode_Failure(t *testing.T) {
	withFakeExec(t, 1)
	err := runSwagger()
	require.Error(t, err)
}
