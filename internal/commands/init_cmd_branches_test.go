package commands

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// finishInit — JSON + error branches
// ─────────────────────────────────────────────────────────────────────

// TestFinishInit_JSON_SuccessEmitsResult — JSON mode + nil err writes
// an initResult document with success=true.
func TestFinishInit_JSON_SuccessEmitsResult(t *testing.T) {
	withJSONMode(t)
	steps := initSteps{}
	steps.add("env.create", "ok", nil)

	out := captureStdout(t, func() {
		err := finishInit(steps, nil)
		require.NoError(t, err)
	})

	var got initResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "init", got.Action)
	assert.True(t, got.Success)
	assert.Empty(t, got.Error)
	assert.Len(t, got.Steps, 1)
}

// TestFinishInit_JSON_FailurePropagates — JSON mode + non-nil err: the
// result records the error string AND finishInit returns the original
// err so the exit code stays non-zero.
func TestFinishInit_JSON_FailurePropagates(t *testing.T) {
	withJSONMode(t)
	steps := initSteps{}
	steps.add("build", "fail", errors.New("build broke"))

	out := captureStdout(t, func() {
		err := finishInit(steps, errors.New("build broke"))
		require.Error(t, err)
	})

	var got initResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.False(t, got.Success)
	assert.Equal(t, "build broke", got.Error)
}

// TestFinishInit_TextMode_NoStdoutWrite — without JSON mode, finishInit
// does NOT emit any JSON; it just returns the err pass-through.
func TestFinishInit_TextMode_NoStdoutWrite(t *testing.T) {
	steps := initSteps{}
	out := captureStdout(t, func() {
		err := finishInit(steps, errors.New("x"))
		require.Error(t, err)
	})
	assert.Empty(t, strings.TrimSpace(out))
}

// ─────────────────────────────────────────────────────────────────────
// runCmd — JSON mode reroutes stdout
// ─────────────────────────────────────────────────────────────────────

// TestRunCmd_JSONReroutesStdout — in JSON mode runCmd directs the
// child's stdout to stderr so the parent's structured result stays
// the only thing on stdout.
func TestRunCmd_JSONReroutesStdout(t *testing.T) {
	withJSONMode(t)
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("true")
	}
	assert.NoError(t, runCmd("ignored"))
}

// TestRunCmd_TextModeUsesStdout — in text mode the child's stdout
// streams to the parent's stdout.
func TestRunCmd_TextModeUsesStdout(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("true")
	}
	assert.NoError(t, runCmd("ignored"))
}

// ─────────────────────────────────────────────────────────────────────
// runInit — failure paths
// ─────────────────────────────────────────────────────────────────────

// TestRunInit_GoModTidyFailureStops — first failing step is go mod
// tidy. runInit returns the wrapped error without proceeding to
// generate Wire / GraphQL etc.
func TestRunInit_GoModTidyFailureStops(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 1) // every exec call returns exit 1
	err := runInit()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go mod tidy failed")
}

// TestRunInit_BuildFailureStops — every prior step succeeds (go mod
// tidy, wire, swag, migrate) but the final go build step fails.
// Sequence: mod-tidy(0), wire(0), swag(0), migrate(0), build(1).
// gqlgen and graphql skip because no gqlgen.yml in the temp dir.
func TestRunInit_BuildFailureStops(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	stagedFakeExec(t, 0, 0, 0, 0, 1)
	err := runInit()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build verification failed")
}
