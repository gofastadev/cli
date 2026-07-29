package deploy

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRollback_DryRun_DockerMethod(t *testing.T) {
	cfg := newTestCfg("docker")
	// Dry-run means RunRemoteCapture returns "". Rollback sees 1 release, errors.
	err := Rollback(cfg)
	assert.Error(t, err)
}

func TestRollback_DryRun_BinaryMethod(t *testing.T) {
	cfg := newTestCfg("binary")
	err := Rollback(cfg)
	assert.Error(t, err)
}

func TestRollback_WithStagedOutputs(t *testing.T) {
	// Non dry-run, so we use a fake exec that returns staged stdouts.
	// Call sequence: ls releases, readlink current, compose-up, symlink, curl health
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	stdouts := []string{
		"20260102-000000\n20260101-000000\n", // ls releases
		"/opt/test/releases/20260102-000000", // readlink current
		"",                                   // compose up
		"",                                   // symlink
		"",                                   // health check
	}
	codes := []int{0, 0, 0, 0, 0}
	stagedFakeExec(t, codes, stdouts)
	assert.NoError(t, Rollback(cfg))
}

func TestRollback_Binary_WithStagedOutputs(t *testing.T) {
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	stdouts := []string{
		"20260102-000000\n20260101-000000\n",
		"/opt/test/releases/20260102-000000",
		"", "", "",
	}
	stagedFakeExec(t, []int{0, 0, 0, 0, 0}, stdouts)
	assert.NoError(t, Rollback(cfg))
}

func TestRollback_ListFails(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	stagedFakeExec(t, []int{1}, nil)
	err := Rollback(cfg)
	assert.Error(t, err)
}

func TestRollback_ReadlinkFails(t *testing.T) {
	// list succeeds, readlink fails (warning path)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	stdouts := []string{"a\nb\n", "", "", "", ""}
	codes := []int{0, 1, 0, 0, 0}
	stagedFakeExec(t, codes, stdouts)
	// should still succeed if non-current release is found
	err := Rollback(cfg)
	// With empty readlink output, "current" becomes "." and "a" becomes previous.
	// compose-up + symlink + health all succeed.
	assert.NoError(t, err)
}

// Helper that returns staged stdouts but fails exec when a substring matches.
func withFailOnArgAndStdouts(t *testing.T, failSubstr string, stdouts []string) {
	t.Helper()
	origCmd := execCommand
	origLook := execLookPath
	call := 0
	execCommand = func(name string, args ...string) *exec.Cmd {
		joined := name
		for _, a := range args {
			joined += " " + a
		}
		code := 0
		if contains(joined, failSubstr) {
			code = 1
		}
		out := ""
		if call < len(stdouts) {
			out = stdouts[call]
		}
		call++
		return fakeExecCommand(code, out)(name, args...)
	}
	execLookPath = func(n string) (string, error) { return "/usr/bin/" + n, nil }
	t.Cleanup(func() {
		execCommand = origCmd
		execLookPath = origLook
	})
}

func TestRollback_NoPreviousFound(t *testing.T) {
	// Only one release, and it matches current — previous is empty string.
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	stdouts := []string{"only-release\n", "/opt/test/releases/only-release"}
	stagedFakeExec(t, []int{0, 0}, stdouts)
	err := Rollback(cfg)
	assert.Error(t, err)
}

func TestRollback_ComposeUpFails(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	stdouts := []string{"a\nb\n", "/opt/test/releases/a", ""}
	withFailOnArgAndStdouts(t, "docker compose -f compose.yaml up", stdouts)
	err := Rollback(cfg)
	assert.Error(t, err)
}

func TestRollback_BinaryInstallFails(t *testing.T) {
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	stdouts := []string{"a\nb\n", "/opt/test/releases/a", ""}
	withFailOnArgAndStdouts(t, "systemctl restart", stdouts)
	err := Rollback(cfg)
	assert.Error(t, err)
}

func TestRollback_SymlinkFails(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	stdouts := []string{"a\nb\n", "/opt/test/releases/a", "", "", ""}
	withFailOnArgAndStdouts(t, "ln -sfn", stdouts)
	err := Rollback(cfg)
	assert.Error(t, err)
}

func TestRollback_HealthCheckFails(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	cfg.HealthTimeout = 0
	stdouts := []string{"a\nb\n", "/opt/test/releases/a", "", "", ""}
	withFailOnArgAndStdouts(t, "curl -sf", stdouts)
	err := Rollback(cfg)
	assert.Error(t, err)
}

// TestRollback_NoPreviousRelease — no previous release found → error.
// Empty listing fires the singleton-only check.
func TestRollback_NoPreviousRelease(t *testing.T) {
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	withFakeExecStdout(t, 0, "")
	err := Rollback(cfg)
	require.Error(t, err)
}

// TestRollback_PreviousEmpty — every listed release equals current →
// previous stays "" → error.
func TestRollback_PreviousEmpty(t *testing.T) {
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	stagedFakeExec(t, []int{0, 0}, []string{"r1\nr1\n", "r1\n"})
	err := Rollback(cfg)
	require.Error(t, err)
}
