package commands

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withJSONMode flips cliout into JSON mode for the test and restores
// it on cleanup. Centralizes the toggle so individual tests don't have
// to remember to defer the restore.
func withJSONMode(t *testing.T) {
	t.Helper()
	cliout.SetJSONMode(true)
	t.Cleanup(func() { cliout.SetJSONMode(false) })
}

// --- console -----------------------------------------------------------------

// TestConsole_JSONModeRefuses — console is a REPL; it must refuse with
// CodeInteractiveOnly in JSON mode rather than launching yaegi (whose
// interactive output would corrupt the JSON stream).
func TestConsole_JSONModeRefuses(t *testing.T) {
	withJSONMode(t)
	err := runConsole()
	require.Error(t, err)
	var ce *clierr.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, string(clierr.CodeInteractiveOnly), ce.Code)
	assert.Contains(t, strings.ToLower(ce.Message), "console")
}

// --- swagger -----------------------------------------------------------------

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

// --- exitCodeOf --------------------------------------------------------------

// TestExitCodeOf_Cases pins the helper's three branches: nil → 0,
// ExitError → its code, anything else → -1.
func TestExitCodeOf_Cases(t *testing.T) {
	assert.Equal(t, 0, exitCodeOf(nil))
	assert.Equal(t, -1, exitCodeOf(errors.New("not an exit error")))
	// ExitError is exercised by TestSwagger_JSONEmitsResultOnFailure
	// where withFakeExec(t, 1) produces a real *exec.ExitError.
}

// --- deploy logs -------------------------------------------------------------

// TestDeployLogs_JSONModeRefuses — `deploy logs` tails a remote stream
// (Ctrl+C to stop). Refuse in JSON mode so an agent doesn't hang
// waiting for unstructured text.
func TestDeployLogs_JSONModeRefuses(t *testing.T) {
	withJSONMode(t)
	err := runDeployLogs(deployCmd)
	require.Error(t, err)
	var ce *clierr.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, string(clierr.CodeInteractiveOnly), ce.Code)
}

// --- errString ---------------------------------------------------------------

// TestErrString_Branches — nil → empty, otherwise the error's
// .Error() string. Used by every deploy result emitter.
func TestErrString_Branches(t *testing.T) {
	assert.Equal(t, "", errString(nil))
	assert.Equal(t, "boom", errString(errors.New("boom")))
}

// --- ifText ------------------------------------------------------------------

// TestIfText_GatesOnJSONMode — ifText runs the closure in text mode
// and skips it in JSON mode. The init steps depend on this contract
// to keep the JSON stdout stream clean.
func TestIfText_GatesOnJSONMode(t *testing.T) {
	called := false
	ifText(func() { called = true })
	assert.True(t, called, "ifText must run fn in text mode")

	withJSONMode(t)
	called = false
	ifText(func() { called = true })
	assert.False(t, called, "ifText must skip fn in JSON mode")
}
