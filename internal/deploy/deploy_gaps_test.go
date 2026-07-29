package deploy

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Host / app-name validation (the ssh argument-injection boundary) ---

// TestLoadDeployConfig_RejectsUnsafeHost covers the deployHostPattern branch.
// A host is interpolated into an ssh argv, so a value starting with "-" would
// be read by ssh as an option — "-oProxyCommand=..." executes a local command
// (CVE-2017-1000117 class). config.yaml can come from a cloned repo, so this
// rejection is a security boundary, not input tidiness.
func TestLoadDeployConfig_RejectsUnsafeHost(t *testing.T) {
	unsafe := map[string]string{
		"leading dash option":  "-oProxyCommand=touch /tmp/pwned",
		"shell metacharacter":  "server.com; rm -rf /",
		"command substitution": "$(whoami)@server.com",
		"space separated":      "server.com extra",
		"backtick":             "`id`",
	}

	for name, host := range unsafe {
		t.Run(name, func(t *testing.T) {
			chdirProject(t, "deploy:\n  host: \""+host+"\"\n  method: docker\n")
			_, err := LoadDeployConfig(nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid deploy.host")
		})
	}
}

// TestLoadDeployConfig_AcceptsValidHosts is the counterpart, so the pattern
// cannot be tightened into rejecting ordinary hosts without a test failing.
func TestLoadDeployConfig_AcceptsValidHosts(t *testing.T) {
	for _, host := range []string{"server.com", "user@server.com", "10.0.0.1", "deploy-1.eu-west.example.com"} {
		t.Run(host, func(t *testing.T) {
			chdirProject(t, "deploy:\n  host: \""+host+"\"\n  method: docker\n")
			cfg, err := LoadDeployConfig(nil)
			require.NoError(t, err)
			assert.Equal(t, host, cfg.Host)
		})
	}
}

// TestLoadDeployConfig_RejectsUnsafeAppName covers the deployAppNamePattern
// branch. The app name is derived from go.mod's module path and lands in image
// tags and remote command strings, so the same argument-injection reasoning
// applies to a hostile module line.
func TestLoadDeployConfig_RejectsUnsafeAppName(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	// The last path segment becomes the app name.
	require.NoError(t, os.WriteFile("go.mod", []byte("module github.com/test/app;rm -rf /\n\ngo 1.21\n"), 0o644))
	require.NoError(t, os.WriteFile("config.yaml", []byte("deploy:\n  host: server.com\n  method: docker\n"), 0o644))

	_, err = LoadDeployConfig(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid app name")
}

// --- strictHostKeyPolicy ---

// TestStrictHostKeyPolicy covers the configured-value branch. The default is
// accept-new (trust on first use) so an automated first deploy is not blocked;
// an operator who wants the key pinned in advance sets "yes".
func TestStrictHostKeyPolicy(t *testing.T) {
	assert.Equal(t, "accept-new", strictHostKeyPolicy(&DeployConfig{}),
		"an unset policy must not silently become strict and break first deploys")
	assert.Equal(t, "yes", strictHostKeyPolicy(&DeployConfig{StrictHostKey: "yes"}))
	assert.Equal(t, "no", strictHostKeyPolicy(&DeployConfig{StrictHostKey: "no"}))
}

// --- RunLocalPiped error branches ---

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

// --- Migrations-failed warnings (deploy continues deliberately) ---

// TestDeployBinary_MigrationFailureIsNonFatal pins the choice that a failing
// migration warns rather than aborts: the new binary is already installed, so
// stopping midway would leave the release half-applied.
func TestDeployBinary_MigrationFailureIsNonFatal(t *testing.T) {
	withinProject(t)
	withFailOnArg(t, "migrate up")
	cfg := newTestCfg("binary")
	cfg.DryRun = false

	assert.NoError(t, DeployBinary(cfg), "a failed migration must not fail the deploy")
}

func TestDeployDocker_MigrationFailureIsNonFatal(t *testing.T) {
	withinProject(t)
	withFailOnArg(t, "migrate up")
	cfg := newTestCfg("docker")
	cfg.DryRun = false

	assert.NoError(t, DeployDocker(cfg), "a failed migration must not fail the deploy")
}

// TestSetupServer_MigrateInstallFailureIsNonFatal covers the warning branch
// when the migrate CLI cannot be fetched: setup still completes, leaving the
// operator to install it by hand.
func TestSetupServer_MigrateInstallFailureIsNonFatal(t *testing.T) {
	withinProject(t)
	withFailOnArg(t, "command -v migrate")
	cfg := newTestCfg("binary")
	cfg.DryRun = false

	assert.NoError(t, SetupServer(cfg))
}
