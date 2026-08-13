package commands

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
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
// package so non-dry-run code paths don't actually shell out. The exitCode
// parameter is kept for future call sites that want non-zero exits.
//
//nolint:unparam // exitCode kept for future flexibility.
func stubDeployExec(t *testing.T, exitCode int) {
	t.Helper()
	stubDeployLookPath(t)
	deploy.SetExecCommandForTest(fakeExecCommand(exitCode))
	t.Cleanup(func() { deploy.ResetExecCommandForTest() })
}

// setupDeployProject writes a minimal gofasta project structure in a tempdir
// for the deploy commands to consume.
func setupDeployProject(t *testing.T, host string) {
	t.Helper()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	// go.mod — required for readAppName
	require.NoError(t, os.WriteFile("go.mod", []byte("module github.com/test/myapp\n\ngo 1.21\n"), 0o644))

	// config.yaml with deploy section
	yaml := ""
	if host != "" {
		yaml = "deploy:\n  host: " + host + "\n  method: docker\n"
	}
	require.NoError(t, os.WriteFile("config.yaml", []byte(yaml), 0o644))

	// compose.yaml for docker method
	require.NoError(t, os.MkdirAll("deployments/docker", 0o755))
	require.NoError(t, os.WriteFile("deployments/docker/compose.production.yaml", []byte("services: {}\n"), 0o644))

	// systemd unit for binary method (required — its absence fails the deploy)
	require.NoError(t, os.MkdirAll("deployments/systemd", 0o755))
	require.NoError(t, os.WriteFile("deployments/systemd/app.service", []byte("[Unit]\n"), 0o644))
}

// makeDeployCmd builds a cobra.Command with the deploy flag set for test use.
// The dryRun parameter is kept for future non-dry-run test cases.
//
//nolint:unparam // dryRun kept for future flexibility.
func makeDeployCmd(dryRun bool, flags map[string]string) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().String("host", "", "")
	c.Flags().String("method", "", "")
	c.Flags().Int("port", 0, "")
	c.Flags().String("path", "", "")
	c.Flags().String("arch", "", "")
	c.Flags().Bool("dry-run", false, "")
	if dryRun {
		_ = c.Flags().Set("dry-run", "true")
	}
	for k, v := range flags {
		_ = c.Flags().Set(k, v)
	}
	return c
}

func TestRunDeploy_DockerDryRun(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	stubDeployLookPath(t)
	cmd := makeDeployCmd(true, nil)
	assert.NoError(t, runDeploy(cmd))
}

func TestRunDeploy_BinaryDryRun(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	stubDeployLookPath(t)
	// Binary method needs a buildable ./app/main package
	require.NoError(t, os.MkdirAll("app/main", 0o755))
	require.NoError(t, os.WriteFile("app/main/main.go", []byte("package main\nfunc main() {}\n"), 0o644))
	cmd := makeDeployCmd(true, map[string]string{"method": "binary"})
	// Binary path actually calls `go build` but RunLocal respects DryRun.
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

func TestRunDeployRollback_NoHost(t *testing.T) {
	setupDeployProject(t, "")
	cmd := makeDeployCmd(true, nil)
	err := runDeployRollback(cmd)
	assert.Error(t, err)
}

// The real deployCmd has flags set up in init(); we just set dry-run + host
// directly on it for the duration of the test.
func withDeployFlags(t *testing.T, c *cobra.Command, flags map[string]string) {
	t.Helper()
	// The deploy flags are persistent (shared with subcommands); merge them
	// into Flags() the same way cobra's Execute path does, so direct RunE
	// invocations see them.
	_ = c.ParseFlags(nil)
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

// TestRunDeploy_NoConfig — blank cmd → LoadDeployConfig errors →
// propagates out.
func TestRunDeploy_NoConfig(t *testing.T) {
	chdirTemp(t)
	cmd := &cobra.Command{}
	err := runDeploy(cmd)
	require.Error(t, err)
}

// TestRunDeployStatus_NoConfig — same as above for runDeployStatus.
func TestRunDeployStatus_NoConfig(t *testing.T) {
	chdirTemp(t)
	cmd := &cobra.Command{}
	err := runDeployStatus(cmd)
	require.Error(t, err)
}

// TestRunDeployLogs_NoConfig — same as above for runDeployLogs.
func TestRunDeployLogs_NoConfig(t *testing.T) {
	chdirTemp(t)
	cmd := &cobra.Command{}
	err := runDeployLogs(cmd)
	require.Error(t, err)
}

// TestRunDeploy_UnknownMethodCoverage — uses the deployMethodOverride
// seam to force the switch's default branch. LoadDeployConfig would
// normally reject "bogus" before runDeploy reached the switch.
func TestRunDeploy_UnknownMethodCoverage(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.WriteFile("go.mod",
		[]byte("module example.com/t\n\ngo 1.25.0\n"), 0o644))
	require.NoError(t, os.WriteFile("config.yaml",
		[]byte("deploy:\n  host: user@example.com\n  method: docker\n"), 0o644))
	// LoadDeployConfig validates Method, so the default branch is only
	// reachable with a hand-built config — which is exactly why the
	// dispatch lives in runDeployMethod instead of a mutable test global.
	err := runDeployMethod(&deploy.DeployConfig{Method: "bogus"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown deploy method")
}

func TestDeployCmd_IsRegistered(t *testing.T) {
	cmds := rootCmd.Commands()
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Name())
	}
	assert.Contains(t, names, "deploy", "rootCmd should have deploy subcommand")
}

func TestDeployCmd_HasSubcommands(t *testing.T) {
	cmds := deployCmd.Commands()
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Name())
	}

	expected := []string{"setup", "status", "logs", "rollback"}
	for _, exp := range expected {
		assert.Contains(t, names, exp, "deployCmd should have subcommand: %s", exp)
	}
}

func TestDeployCmd_HasFlags(t *testing.T) {
	flags := deployCmd.PersistentFlags()

	assert.NotNil(t, flags.Lookup("host"), "deploy should have --host flag")
	assert.NotNil(t, flags.Lookup("method"), "deploy should have --method flag")
	assert.NotNil(t, flags.Lookup("port"), "deploy should have --port flag")
	assert.NotNil(t, flags.Lookup("path"), "deploy should have --path flag")
	assert.NotNil(t, flags.Lookup("arch"), "deploy should have --arch flag")
	assert.NotNil(t, flags.Lookup("dry-run"), "deploy should have --dry-run flag")
}

// TestDeployCmd_SubcommandsInheritFlags — the flags are PERSISTENT so every
// subcommand accepts them; the previous per-command copies silently omitted
// --dry-run from all four subcommands, which no test could catch because
// the tests rebuilt their own flag sets.
func TestDeployCmd_SubcommandsInheritFlags(t *testing.T) {
	for _, sub := range []*cobra.Command{deploySetupCmd, deployStatusCmd, deployLogsCmd, deployRollbackCmd} {
		t.Run(sub.Name(), func(t *testing.T) {
			inherited := sub.InheritedFlags()
			for _, flag := range []string{"host", "method", "port", "path", "arch", "dry-run"} {
				assert.NotNil(t, inherited.Lookup(flag),
					"deploy %s must accept --%s", sub.Name(), flag)
			}
		})
	}
}

// TestDeployCmd_RejectsPositionalArgs — `gofasta deploy statuss` (a typo)
// must be an error, never a silent full production deploy.
func TestDeployCmd_RejectsPositionalArgs(t *testing.T) {
	for _, cmd := range []*cobra.Command{deployCmd, deploySetupCmd, deployStatusCmd, deployLogsCmd, deployRollbackCmd} {
		t.Run(cmd.Name(), func(t *testing.T) {
			require.NotNil(t, cmd.Args, "%s must declare an Args validator", cmd.Name())
			assert.Error(t, cmd.Args(cmd, []string{"statuss"}),
				"%s must reject positional arguments", cmd.Name())
		})
	}
}

func TestDeployCmd_HasDescriptions(t *testing.T) {
	assert.NotEmpty(t, deployCmd.Short)
	assert.NotEmpty(t, deployCmd.Long)
	assert.NotEmpty(t, deploySetupCmd.Short)
	assert.NotEmpty(t, deployStatusCmd.Short)
	assert.NotEmpty(t, deployLogsCmd.Short)
	assert.NotEmpty(t, deployRollbackCmd.Short)
}

// A failed deploy in JSON mode emits a deployResult document with
// success=false and the error message.
func TestRunDeploy_JSON_FailureDocument(t *testing.T) {
	withJSONMode(t)

	out := captureStdout(t, func() {
		emitDeployResult("deploy", &deploy.DeployConfig{Method: "weird"},
			runDeployMethod(&deploy.DeployConfig{Method: "weird"}))
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
// Dry-run rollback describes the plan and succeeds; the JSON document
// reflects that. (Failure documents are covered by
// TestRunDeploy_JSON_FailureDocument.)
func TestRunDeployRollback_JSON_DryRun(t *testing.T) {
	setupDeployProject(t, "user@example.com")
	stubDeployLookPath(t)
	withJSONMode(t)
	cmd := makeDeployCmd(true, nil)

	out := captureStdout(t, func() {
		require.NoError(t, runDeployRollback(cmd))
	})

	var got deployResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "deploy.rollback", got.Action)
	assert.True(t, got.Success)
	assert.Empty(t, got.Error)
}

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

// TestErrString_Branches — nil → empty, otherwise the error's
// .Error() string. Used by every deploy result emitter.
func TestErrString_Branches(t *testing.T) {
	assert.Equal(t, "", errString(nil))
	assert.Equal(t, "boom", errString(errors.New("boom")))
}
