package commands

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSwaggerCmd_RunE_FakeSuccess(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, swaggerCmd.RunE(swaggerCmd, nil))
}

// TestSwagger_JSONEmitsResult — in JSON mode swagger captures swag's
// stdout/stderr and emits a single swaggerResult JSON document.
// Drives the success branch via a fake exec returning exit 0.
func TestSwagger_JSONEmitsResult(t *testing.T) {
	withJSONMode(t)
	withFakeExec(t, 0)
	out := captureStdout(t, func() {
		require.NoError(t, runSwagger())
	})

	var got swaggerResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "swagger.init", got.Action)
	assert.Equal(t, 0, got.ExitCode)
	assert.Empty(t, got.Error)
}

// TestSwagger_JSONEmitsResultOnFailure — swag exits non-zero; the JSON
// result reflects the exit code and surfaces the error message.
func TestSwagger_JSONEmitsResultOnFailure(t *testing.T) {
	withJSONMode(t)
	withFakeExec(t, 1)
	var runErr error
	out := captureStdout(t, func() {
		runErr = runSwagger()
	})
	require.Error(t, runErr)

	var got swaggerResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "swagger.init", got.Action)
	assert.Equal(t, 1, got.ExitCode)
	assert.NotEmpty(t, got.Error)
}

// TestExitCodeOf_Cases pins the helper's three branches: nil → 0,
// ExitError → its code, anything else → -1.
func TestExitCodeOf_Cases(t *testing.T) {
	assert.Equal(t, 0, exitCodeOf(nil))
	assert.Equal(t, -1, exitCodeOf(errors.New("not an exit error")))
	// ExitError is exercised by TestSwagger_JSONEmitsResultOnFailure
	// where withFakeExec(t, 1) produces a real *exec.ExitError.
}

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
