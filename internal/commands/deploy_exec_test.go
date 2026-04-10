package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gofastadev/cli/internal/deploy"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubDeployLookPath forces deploy.execLookPath to always succeed.
func stubDeployLookPath(t *testing.T) {
	t.Helper()
	deploy.SetLookPathForTest(func(name string) (string, error) {
		return "/usr/bin/" + name, nil
	})
	t.Cleanup(func() { deploy.ResetLookPathForTest() })
}

// stubDeployExec stubs both exec.LookPath and exec.Command in the deploy
// package so non-dry-run code paths don't actually shell out.
func stubDeployExec(t *testing.T, exitCode int) {
	t.Helper()
	stubDeployLookPath(t)
	deploy.SetExecCommandForTest(fakeExecCommand(exitCode))
	t.Cleanup(func() { deploy.ResetExecCommandForTest() })
}

// setupDeployProject writes a minimal gofasta project structure in a tempdir
// for the deploy commands to consume.
func setupDeployProject(t *testing.T, host string) string {
	t.Helper()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	// go.mod — required for readAppName
	require.NoError(t, os.WriteFile("go.mod", []byte("module github.com/test/myapp\n\ngo 1.21\n"), 0644))

	// config.yaml with deploy section
	yaml := ""
	if host != "" {
		yaml = "deploy:\n  host: " + host + "\n  method: docker\n"
	}
	require.NoError(t, os.WriteFile("config.yaml", []byte(yaml), 0644))

	// compose.yaml for docker method
	require.NoError(t, os.MkdirAll("deployments/docker", 0755))
	require.NoError(t, os.WriteFile("deployments/docker/compose.production.yaml", []byte("services: {}\n"), 0644))

	return dir
}

func makeDeployCmd(dryRun bool, flags map[string]string) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().String("host", "", "")
	c.Flags().String("method", "", "")
	c.Flags().Int("port", 0, "")
	c.Flags().String("path", "", "")
	c.Flags().String("arch", "", "")
	c.Flags().Bool("dry-run", false, "")
	if dryRun {
		c.Flags().Set("dry-run", "true")
	}
	for k, v := range flags {
		c.Flags().Set(k, v)
	}
	return c
}

// --- runDeploy (docker, dry-run) ---

func TestRunDeploy_DockerDryRun(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	stubDeployLookPath(t)
	cmd := makeDeployCmd(true, nil)
	assert.NoError(t, runDeploy(cmd))
}

func TestRunDeploy_BinaryDryRun(t *testing.T) {
	dir := setupDeployProject(t, "user@example.com")
	stubDeployLookPath(t)
	// Binary method needs a buildable ./app/main package
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "app/main"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app/main/main.go"), []byte("package main\nfunc main() {}\n"), 0644))
	cmd := makeDeployCmd(true, map[string]string{"method": "binary"})
	// Binary path actually calls `go build` (dry-run only dry-runs SSH/SCP, not RunLocal).
	// Wait — RunLocal also respects DryRun. So go build is also skipped. Good.
	assert.NoError(t, runDeploy(cmd))
}

func TestRunDeploy_UnknownMethod(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	stubDeployLookPath(t)
	cmd := makeDeployCmd(true, map[string]string{"method": "weird"})
	err := runDeploy(cmd)
	assert.Error(t, err)
}

func TestRunDeploy_NoHost(t *testing.T) {
	setupDeployProject(t, "")
	stubDeployLookPath(t)
	cmd := makeDeployCmd(true, nil)
	err := runDeploy(cmd)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "deploy host")
}

// --- runDeploySetup ---

func TestRunDeploySetup_DryRun(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	stubDeployLookPath(t)
	cmd := makeDeployCmd(true, nil)
	assert.NoError(t, runDeploySetup(cmd))
}

func TestRunDeploySetup_MissingHost(t *testing.T) {
	setupDeployProject(t, "")
	cmd := makeDeployCmd(true, nil)
	err := runDeploySetup(cmd)
	assert.Error(t, err)
}

// --- runDeployStatus ---

func TestRunDeployStatus_DockerDryRun(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	cmd := makeDeployCmd(true, nil)
	assert.NoError(t, runDeployStatus(cmd))
}

func TestRunDeployStatus_BinaryDryRun(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	cmd := makeDeployCmd(true, map[string]string{"method": "binary"})
	assert.NoError(t, runDeployStatus(cmd))
}

// --- runDeployLogs ---

func TestRunDeployLogs_DockerDryRun(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	cmd := makeDeployCmd(true, nil)
	assert.NoError(t, runDeployLogs(cmd))
}

func TestRunDeployLogs_BinaryDryRun(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	cmd := makeDeployCmd(true, map[string]string{"method": "binary"})
	assert.NoError(t, runDeployLogs(cmd))
}

// --- runDeployRollback ---

func TestRunDeployRollback_NoHost(t *testing.T) {
	setupDeployProject(t, "")
	cmd := makeDeployCmd(true, nil)
	err := runDeployRollback(cmd)
	assert.Error(t, err)
}

// --- cobra RunE wrapper coverage for the real command objects ---

// The real deployCmd has flags set up in init(); we just set dry-run + host
// directly on it for the duration of the test.
func withDeployFlags(t *testing.T, c *cobra.Command, flags map[string]string) {
	t.Helper()
	prev := map[string]string{}
	for k, v := range flags {
		if f := c.Flags().Lookup(k); f != nil {
			prev[k] = f.Value.String()
			c.Flags().Set(k, v)
		}
	}
	t.Cleanup(func() {
		for k, v := range prev {
			c.Flags().Set(k, v)
		}
	})
}

func TestDeployCmd_RunE(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	stubDeployLookPath(t)
	withDeployFlags(t, deployCmd, map[string]string{"dry-run": "true"})
	assert.NoError(t, deployCmd.RunE(deployCmd, nil))
}

func TestDeploySetupCmd_RunE(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	stubDeployExec(t, 0)
	assert.NoError(t, deploySetupCmd.RunE(deploySetupCmd, nil))
}

func TestDeployStatusCmd_RunE(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	stubDeployExec(t, 0)
	assert.NoError(t, deployStatusCmd.RunE(deployStatusCmd, nil))
}

func TestDeployLogsCmd_RunE(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	stubDeployExec(t, 0)
	assert.NoError(t, deployLogsCmd.RunE(deployLogsCmd, nil))
}

func TestDeployRollbackCmd_RunE(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	// Stub returns two releases so rollback can find a previous one.
	// Using a multi-call fake: first RunRemoteCapture (ls releases) returns two lines,
	// second RunRemoteCapture (readlink) returns the newest, then RunRemote for compose-up,
	// RunRemote for symlink, RunRemoteCapture for health. All must succeed.
	// Simpler: use the helper stub that exits 0; capture stdout is empty which gives
	// "no previous release" error — so configure with method docker to take the docker path.
	// Since fake exec returns empty stdout, rollback will see only 1 release and error.
	// That's still acceptable: we exercise the RunE wrapper.
	stubDeployExec(t, 0)
	err := deployRollbackCmd.RunE(deployRollbackCmd, nil)
	assert.Error(t, err)
}
