// Coverage for ssh.go — host-key policy and the local pipeline runner.

package deploy

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
