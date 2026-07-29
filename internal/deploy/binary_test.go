package deploy

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeployBinary_Live_AllSuccess(t *testing.T) {
	withinProject(t)
	withFakeExec(t, 0)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	assert.NoError(t, DeployBinary(cfg))
}

func TestDeployBinary_PreflightFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	withFailOnArg(t, "echo ok")
	err := DeployBinary(cfg)
	assert.Error(t, err)
}

func TestDeployBinary_RemoteMkdirFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	// Target the specific mkdir that creates release/migrations/etc
	withFailOnArg(t, "mkdir -p /opt/test/releases")
	err := DeployBinary(cfg)
	assert.Error(t, err)
}

func TestDeployBinary_CopyBinaryFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	// scp of the binary — the binary is at tmp/testapp and dest ends with /testapp
	withFailOnArg(t, "testapp")
	err := DeployBinary(cfg)
	assert.Error(t, err)
}

func TestDeployBinary_CrossCompileFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	// Cross-compile uses go build via RunLocal
	withFailOnArg(t, "-ldflags=-s -w")
	err := DeployBinary(cfg)
	assert.Error(t, err)
}

func TestDeployBinary_MkdirFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	withFailOnArg(t, "mkdir -p")
	err := DeployBinary(cfg)
	assert.Error(t, err)
}

func TestDeployBinary_InstallFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	withFailOnArg(t, "/usr/local/bin")
	err := DeployBinary(cfg)
	assert.Error(t, err)
}

func TestDeployBinary_RestartFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	withFailOnArg(t, "systemctl restart")
	err := DeployBinary(cfg)
	assert.Error(t, err)
}

func TestDeployBinary_HealthFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	cfg.HealthTimeout = 0
	withFailOnArg(t, "curl -sf")
	err := DeployBinary(cfg)
	assert.Error(t, err)
}

func TestDeployBinary_DryRun(t *testing.T) {
	withinProject(t)
	withFakeExec(t, 0)
	cfg := newTestCfg("binary")
	assert.NoError(t, DeployBinary(cfg))
}

func TestDeployBinary_CopyMissingSvcFile(t *testing.T) {
	// Delete the systemd service file so the "else" branch fires
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	require.NoError(t, os.MkdirAll("deployments/docker", 0755))
	os.WriteFile("deployments/docker/compose.production.yaml", []byte(""), 0644)
	require.NoError(t, os.MkdirAll("app/main", 0755))
	withFakeExec(t, 0)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	// No systemd service file → PrintWarning branch fires; rest succeeds
	assert.NoError(t, DeployBinary(cfg))
}

func TestSetOrUnset(t *testing.T) {
	t.Setenv("GOFASTA_SO_TEST", "x")
	setOrUnset("GOFASTA_SO_TEST", "")
	assert.Equal(t, "", os.Getenv("GOFASTA_SO_TEST"))
	setOrUnset("GOFASTA_SO_TEST", "y")
	assert.Equal(t, "y", os.Getenv("GOFASTA_SO_TEST"))
}

// TestDeployBinary_SymlinkFails — the symlink step runs via RunRemote
// which invokes ssh. We fail on "ln -sfn" substring.
func TestDeployBinary_SymlinkFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	withFailOnArg(t, "ln -sfn")
	err := DeployBinary(cfg)
	require.Error(t, err)
}

// TestDeployBinary_CopyBinaryScpFails — fail only the scp command
// (name == "scp"). Target only those whose destination path ends with
// the app name (not .env/.yaml).
func TestDeployBinary_CopyBinaryScpFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		code := 0
		if name == "scp" && len(args) > 0 {
			last := args[len(args)-1]
			// Binary dest ends with "/testapp" and is not the service file.
			if strings.HasSuffix(last, "/testapp") {
				code = 1
			}
		}
		return fakeExecCommand(code, "")(name, args...)
	}
	t.Cleanup(func() { execCommand = orig })
	lpOrig := execLookPath
	execLookPath = func(n string) (string, error) { return "/usr/bin/" + n, nil }
	t.Cleanup(func() { execLookPath = lpOrig })

	err := DeployBinary(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to copy binary")
}

// TestDeployBinary_CopySharedFails — make copySharedFiles fail.
// copySharedFiles uses scp to send .env and config.yaml.
func TestDeployBinary_CopySharedFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	// Fail scp of .env → copySharedFiles returns err.
	withFailOnArg(t, ".env")
	err := DeployBinary(cfg)
	require.Error(t, err)
}

// TestDeployBinary_CopyServiceFileFails — serviceFile exists and scp
// fails for it. Use substring matching the service file destination.
func TestDeployBinary_CopyServiceFileFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	// Fail the scp with destination "/tmp/testapp.service".
	withFailOnArg(t, "testapp.service")
	err := DeployBinary(cfg)
	require.Error(t, err)
}

// TestDeployBinary_CleanupWarn — CleanupOldReleases fails → printed
// as a warning, DeployBinary still returns nil.
func TestDeployBinary_CleanupWarn(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	// Fail the "ls -1t" which CleanupOldReleases runs.
	withFailOnArg(t, "ls -1t")
	assert.NoError(t, DeployBinary(cfg))
}

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
