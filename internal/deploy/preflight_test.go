package deploy

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPreflightChecks_DryRun(t *testing.T) {
	withFakeExec(t, 0)
	cfg := newTestCfg("docker")
	assert.NoError(t, PreflightChecks(cfg))
}

func TestPreflightChecks_DryRunBinary(t *testing.T) {
	withFakeExec(t, 0)
	cfg := newTestCfg("binary")
	assert.NoError(t, PreflightChecks(cfg))
}

func TestPreflightChecks_ToolsMissing(t *testing.T) {
	origLook := execLookPath
	execLookPath = func(name string) (string, error) { return "", fmt.Errorf("not found") }
	t.Cleanup(func() { execLookPath = origLook })
	cfg := newTestCfg("docker")
	err := PreflightChecks(cfg)
	assert.Error(t, err)
}

func TestPreflightChecks_LiveSSHFail(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	// SSH fails on first call
	stagedFakeExec(t, []int{1}, nil)
	err := PreflightChecks(cfg)
	assert.Error(t, err)
}

func TestPreflightChecks_LiveAllSucceed_Docker(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	// ssh echo ok, docker --version, docker compose version
	stagedFakeExec(t, []int{0, 0, 0}, nil)
	assert.NoError(t, PreflightChecks(cfg))
}

func TestPreflightChecks_LiveDockerMissing(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	// ssh echo ok, docker --version fails
	stagedFakeExec(t, []int{0, 1, 1}, nil)
	err := PreflightChecks(cfg)
	assert.Error(t, err)
}

func TestPreflightChecks_LiveSystemdOk(t *testing.T) {
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	// ssh echo ok, systemctl --version ok
	stagedFakeExec(t, []int{0, 0}, nil)
	assert.NoError(t, PreflightChecks(cfg))
}

func TestPreflightChecks_LiveSystemdMissing(t *testing.T) {
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	stagedFakeExec(t, []int{0, 1}, nil)
	err := PreflightChecks(cfg)
	assert.Error(t, err)
}

func TestPrintCheck(t *testing.T) {
	assert.NotPanics(t, func() {
		printCheck("x", "ok", true)
		printCheck("x", "fail", false)
	})
}
