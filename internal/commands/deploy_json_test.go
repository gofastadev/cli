package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runDeploy in JSON mode emits a deployResult document on stdout. We run
// with --dry-run + an unknown method override so the switch's default
// branch fires and we get a failure result without shelling out.
func TestRunDeploy_JSON_UnknownMethod(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	stubDeployLookPath(t)
	withJSONMode(t)
	cmd := makeDeployCmd(true, nil)
	deployMethodOverride = "weird"
	t.Cleanup(func() { deployMethodOverride = "" })

	out := captureStdout(t, func() {
		err := runDeploy(cmd)
		require.Error(t, err)
	})

	var got deployResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "deploy", got.Action)
	assert.Equal(t, "weird", got.Method)
	assert.False(t, got.Success)
	assert.Contains(t, got.Error, "unknown deploy method")
}

// runDeploy in JSON mode with docker dry-run succeeds — empty error,
// success=true.
func TestRunDeploy_JSON_DockerDryRunSuccess(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	stubDeployLookPath(t)
	withJSONMode(t)
	cmd := makeDeployCmd(true, nil)

	out := captureStdout(t, func() {
		require.NoError(t, runDeploy(cmd))
	})

	var got deployResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "deploy", got.Action)
	assert.Equal(t, "docker", got.Method)
	assert.True(t, got.Success)
	assert.Empty(t, got.Error)
}

// runDeploySetup in JSON mode emits a deploy.setup result. Dry-run keeps
// the setup steps no-op so the outcome is success.
func TestRunDeploySetup_JSON_Success(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	stubDeployLookPath(t)
	withJSONMode(t)
	cmd := makeDeployCmd(true, nil)

	out := captureStdout(t, func() {
		require.NoError(t, runDeploySetup(cmd))
	})

	var got deployResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "deploy.setup", got.Action)
	assert.True(t, got.Success)
}

// runDeployStatus in JSON mode for docker — RunRemoteCapture returns "" in
// dry-run, so service_status is empty but the JSON document is well-formed
// and discriminated by Action.
func TestRunDeployStatus_JSON_Docker(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	withJSONMode(t)
	cmd := makeDeployCmd(true, nil)

	out := captureStdout(t, func() {
		require.NoError(t, runDeployStatus(cmd))
	})

	var got struct {
		deployResult
		CurrentRelease string `json:"current_release,omitempty"`
		ServiceStatus  string `json:"service_status,omitempty"`
	}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "deploy.status", got.Action)
	assert.Equal(t, "docker", got.Method)
	assert.True(t, got.Success)
}

// runDeployStatus in JSON mode for binary — exercises the systemctl branch
// of the switch instead of the docker branch.
func TestRunDeployStatus_JSON_Binary(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	withJSONMode(t)
	cmd := makeDeployCmd(true, map[string]string{"method": "binary"})

	out := captureStdout(t, func() {
		require.NoError(t, runDeployStatus(cmd))
	})

	var got struct {
		deployResult
		CurrentRelease string `json:"current_release,omitempty"`
		ServiceStatus  string `json:"service_status,omitempty"`
	}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "deploy.status", got.Action)
	assert.Equal(t, "binary", got.Method)
	assert.True(t, got.Success)
}

// runDeployLogs is an interactive log tail — in JSON mode it must refuse
// with CodeInteractiveOnly rather than dumping unstructured log lines
// into the JSON stream.
func TestRunDeployLogs_JSON_Refuses(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	withJSONMode(t)
	cmd := makeDeployCmd(true, nil)

	err := runDeployLogs(cmd)
	require.Error(t, err)
	var ce *clierr.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, string(clierr.CodeInteractiveOnly), ce.Code)
}

// runDeployRollback in JSON mode — rollback fails in dry-run (no prior
// release) and the failure is reflected in the JSON document rather than
// raw text.
func TestRunDeployRollback_JSON_Failure(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	stubDeployLookPath(t)
	withJSONMode(t)
	cmd := makeDeployCmd(true, nil)

	out := captureStdout(t, func() {
		err := runDeployRollback(cmd)
		require.Error(t, err)
	})

	var got deployResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "deploy.rollback", got.Action)
	assert.False(t, got.Success)
	assert.NotEmpty(t, got.Error)
}
