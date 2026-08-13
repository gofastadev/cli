// Coverage for ssh.go — host-key policy and the local pipeline runner.

package deploy

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunRemote_Live(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFakeExec(t, 0)
	assert.NoError(t, RunRemote(cfg, "echo ok"))
}

func TestRunRemote_LiveFail(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFakeExec(t, 1)
	assert.Error(t, RunRemote(cfg, "false"))
}

func TestRunRemoteInteractive_DryRun(t *testing.T) {
	cfg := newTestCfg("docker")
	assert.NoError(t, RunRemoteInteractive(cfg, "ls"))
}

func TestRunRemoteInteractive_Live(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFakeExec(t, 0)
	assert.NoError(t, RunRemoteInteractive(cfg, "ls"))
}

func TestRunRemoteCapture_DryRun(t *testing.T) {
	cfg := newTestCfg("docker")
	out, err := RunRemoteCapture(cfg, "ls")
	assert.NoError(t, err)
	assert.Empty(t, out)
}

func TestRunRemoteCapture_Live(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFakeExecStdout(t, 0, "hello\n")
	out, err := RunRemoteCapture(cfg, "echo hello")
	require.NoError(t, err)
	assert.Equal(t, "hello", out)
}

func TestRunRemoteCapture_LiveFail(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFakeExec(t, 1)
	_, err := RunRemoteCapture(cfg, "ls")
	assert.Error(t, err)
}

func TestCopyFile_Live(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFakeExec(t, 0)
	assert.NoError(t, CopyFile(cfg, "/src", "/dst"))
}

func TestCopyDir_Live(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFakeExec(t, 0)
	assert.NoError(t, CopyDir(cfg, "/src", "/dst"))
}

func TestRunLocalPiped_Live(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFakeExec(t, 0)
	assert.NoError(t, RunLocalPiped(cfg, []string{"true"}, []string{"cat"}))
}

func TestRunLocal_Live(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFakeExec(t, 0)
	assert.NoError(t, RunLocal(cfg, "echo", "hi"))
}

func TestSSHBaseArgs(t *testing.T) {
	cfg := &DeployConfig{
		Host: "user@server.com",
		Port: 2222,
	}
	args := sshBaseArgs(cfg)
	require.Len(t, args, 6)
	assert.Equal(t, "-p", args[0])
	assert.Equal(t, "2222", args[1])
	assert.Equal(t, "-o", args[2])
	assert.Equal(t, "StrictHostKeyChecking=accept-new", args[3])
	assert.Equal(t, "-o", args[4])
	assert.Equal(t, "ConnectTimeout=10", args[5])
}

func TestDryRun_RunRemote(t *testing.T) {
	cfg := &DeployConfig{
		Host:   "user@server.com",
		Port:   22,
		DryRun: true,
	}
	// Should not actually connect — dry run just prints
	err := RunRemote(cfg, "echo hello")
	assert.NoError(t, err)
}

func TestDryRun_CopyFile(t *testing.T) {
	cfg := &DeployConfig{
		Host:   "user@server.com",
		Port:   22,
		DryRun: true,
	}
	err := CopyFile(cfg, "/local/file", "/remote/file")
	assert.NoError(t, err)
}

func TestDryRun_CopyDir(t *testing.T) {
	cfg := &DeployConfig{
		Host:   "user@server.com",
		Port:   22,
		DryRun: true,
	}
	err := CopyDir(cfg, "/local/dir", "/remote/dir")
	assert.NoError(t, err)
}

func TestDryRun_RunLocalPiped(t *testing.T) {
	cfg := &DeployConfig{DryRun: true}
	err := RunLocalPiped(cfg, []string{"echo", "hello"}, []string{"cat"})
	assert.NoError(t, err)
}

func TestDryRun_RunLocal(t *testing.T) {
	cfg := &DeployConfig{DryRun: true}
	err := RunLocal(cfg, "echo", "hello")
	assert.NoError(t, err)
}

// TestStrictHostKeyPolicy covers the configured-value branch. The default is
// accept-new (trust on first use) so an automated first deploy is not blocked;
// an operator who wants the key pinned in advance sets "yes".
func TestStrictHostKeyPolicy(t *testing.T) {
	assert.Equal(t, "accept-new", strictHostKeyPolicy(&DeployConfig{}),
		"an unset policy must not silently become strict and break first deploys")
	assert.Equal(t, "yes", strictHostKeyPolicy(&DeployConfig{StrictHostKey: "yes"}))
	assert.Equal(t, "no", strictHostKeyPolicy(&DeployConfig{StrictHostKey: "no"}))
}

func TestRunLocalPiped_RequiresBothHalves(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false

	assert.Error(t, RunLocalPiped(cfg, nil, []string{"cat"}))
	assert.Error(t, RunLocalPiped(cfg, []string{"echo"}, nil))
}

// TestRunLocalPiped_StdoutPipeFails covers the StdoutPipe error return.
// StdoutPipe refuses when Stdout is already assigned, which is the only way to
// make it fail without starting the process first.
func TestRunLocalPiped_StdoutPipeFails(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = func(name string, args ...string) *exec.Cmd {
		c := exec.Command(name, args...)
		c.Stdout = os.Stdout // makes StdoutPipe return "Stdout already set"
		return c
	}

	cfg := newTestCfg("docker")
	cfg.DryRun = false
	err := RunLocalPiped(cfg, []string{"echo", "hi"}, []string{"cat"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Stdout already set")
}

// TestRunLocalPiped_RightStartFails covers the branch where the receiving half
// of the pipeline cannot start at all.
func TestRunLocalPiped_RightStartFails(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = exec.Command

	cfg := newTestCfg("docker")
	cfg.DryRun = false
	err := RunLocalPiped(cfg, []string{"echo", "hi"}, []string{"/nonexistent/binary/for/test"})
	require.Error(t, err)
}

// TestRunLocalPiped_LeftRunFails covers the left-hand failure path, which must
// still reap the right-hand process rather than leave it running.
func TestRunLocalPiped_LeftRunFails(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = exec.Command

	cfg := newTestCfg("docker")
	cfg.DryRun = false
	err := RunLocalPiped(cfg, []string{"/nonexistent/left/binary"}, []string{"cat"})
	require.Error(t, err)
}
