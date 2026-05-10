package commands

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for dev_services.go functions that invoke docker — start,
// stop, reset, detect, query. Uses the existing execCommand stubbing
// pattern from commands_exec_test.go so no real docker is required.
// ─────────────────────────────────────────────────────────────────────

func TestComposeFileExists_Missing(t *testing.T) {
	chdirTemp(t)
	assert.False(t, composeFileExists())
}

func TestComposeFileExists_Present(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("compose.yaml", []byte("services:\n"), 0o644))
	assert.True(t, composeFileExists())
}

// TestComposeAvailable_DockerMissing — execLookPath stubbed to return
// an error (docker not found) → composeAvailable returns false.
func TestComposeAvailable_DockerMissing(t *testing.T) {
	orig := execLookPath
	execLookPath = func(_ string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { execLookPath = orig })
	assert.False(t, composeAvailable())
}

// TestComposeAvailable_DaemonUp — docker on $PATH + `docker info`
// exits 0 → true.
func TestComposeAvailable_DaemonUp(t *testing.T) {
	orig := execLookPath
	execLookPath = func(_ string) (string, error) { return "/usr/bin/docker", nil }
	t.Cleanup(func() { execLookPath = orig })
	withFakeExec(t, 0)
	assert.True(t, composeAvailable())
}

// TestComposeAvailable_DaemonDown — docker on $PATH, `docker info`
// exits 1 → false.
func TestComposeAvailable_DaemonDown(t *testing.T) {
	orig := execLookPath
	execLookPath = func(_ string) (string, error) { return "/usr/bin/docker", nil }
	t.Cleanup(func() { execLookPath = orig })
	withFakeExec(t, 1)
	assert.False(t, composeAvailable())
}

// TestStartServices_Empty — nil / empty names is a no-op.
func TestStartServices_Empty(t *testing.T) {
	assert.NoError(t, startServices(nil, nil))
	assert.NoError(t, startServices([]string{}, []string{"cache"}))
}

func TestStartServices_HappyPath(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, startServices([]string{"db"}, nil))
}

func TestStartServices_WithProfile(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, startServices([]string{"cache"}, []string{"cache"}))
}

func TestStartServices_MultipleProfiles(t *testing.T) {
	withFakeExec(t, 0)
	// Multi-profile activation per the compose docs is additive — we
	// just verify the call doesn't error when both profiles are passed.
	assert.NoError(t, startServices([]string{"cache", "queue"}, []string{"cache", "queue"}))
}

func TestStartServices_DockerFails(t *testing.T) {
	withFakeExec(t, 1)
	err := startServices([]string{"db"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker compose up")
}

func TestStopServices_Empty(t *testing.T) {
	assert.NoError(t, stopServices(nil))
}

func TestStopServices_HappyPath(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, stopServices([]string{"db"}))
}

func TestStopServices_Failure(t *testing.T) {
	withFakeExec(t, 1)
	assert.Error(t, stopServices([]string{"db"}))
}

func TestResetVolumes_HappyPath(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, resetVolumes())
}

func TestResetVolumes_Failure(t *testing.T) {
	withFakeExec(t, 1)
	assert.Error(t, resetVolumes())
}

func TestQueryServiceStates_ExecFails(t *testing.T) {
	withFakeExec(t, 1)
	_, err := queryServiceStates()
	require.Error(t, err)
}

func TestDetectComposeServices_ExecFails(t *testing.T) {
	withFakeExec(t, 1)
	_, _, err := detectComposeServices(nil, false)
	require.Error(t, err)
}

// TestRunSeedDelegation_FakeSuccess — `gofasta seed` delegation,
// stubbed exec.
func TestRunSeedDelegation_FakeSuccess(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	assert.NoError(t, runSeedDelegation())
}

func TestRunSeedDelegation_FakeFailure(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 1)
	assert.Error(t, runSeedDelegation())
}

// ── do.go helpers ────────────────────────────────────────────────────

func TestStepStatusMark_EveryBranch(t *testing.T) {
	// Every status string supported by the do.go step renderer.
	for _, in := range []string{"ok", "error", "skip", "unknown"} {
		got := stripANSI(stepStatusMark(in))
		assert.NotEmpty(t, got, "in=%s produced empty mark", in)
	}
}
