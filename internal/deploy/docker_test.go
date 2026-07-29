package deploy

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeployDocker_Live_AllSuccess(t *testing.T) {
	withinProject(t)
	withFakeExec(t, 0)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	assert.NoError(t, DeployDocker(cfg))
}

func TestDeployDocker_BuildFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFailOnArg(t, "docker build")
	err := DeployDocker(cfg)
	assert.Error(t, err)
}

func TestDeployDocker_PreflightFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	// Make ssh echo fail — preflight ssh connectivity check
	withFailOnArg(t, "echo ok")
	err := DeployDocker(cfg)
	assert.Error(t, err)
}

func TestDeployDocker_MkdirFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFailOnArg(t, "mkdir -p")
	err := DeployDocker(cfg)
	assert.Error(t, err)
}

func TestDeployDocker_SaveFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFailOnArg(t, "docker save")
	err := DeployDocker(cfg)
	assert.Error(t, err)
}

func TestDeployDocker_ComposeUpFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFailOnArg(t, "docker compose -f compose.yaml up")
	err := DeployDocker(cfg)
	assert.Error(t, err)
}

func TestDeployDocker_HealthCheckFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	cfg.HealthTimeout = 0
	withFailOnArg(t, "curl -sf")
	err := DeployDocker(cfg)
	assert.Error(t, err)
}

func TestDeployDocker_SymlinkFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFailOnArg(t, "ln -sfn")
	err := DeployDocker(cfg)
	assert.Error(t, err)
}

func TestDeployDocker_CleanupWarning(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	// Cleanup failure is non-fatal (warning)
	withFailOnArg(t, "ls -1t")
	assert.NoError(t, DeployDocker(cfg))
}

func TestDeployDocker_DryRun(t *testing.T) {
	withinProject(t)
	withFakeExec(t, 0) // for PreflightChecks exec.LookPath
	cfg := newTestCfg("docker")
	assert.NoError(t, DeployDocker(cfg))
}

func TestDeployDocker_MissingComposeFile(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	withFakeExec(t, 0)
	cfg := newTestCfg("docker")
	err := DeployDocker(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "compose")
}

func TestDeployDocker_CopySharedFileFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	// The only scp inside copySharedFiles is for .env and config.yaml.
	// Fail on the shared path in the destination.
	withFailOnArg(t, "/opt/test/shared/.env")
	err := DeployDocker(cfg)
	assert.Error(t, err)
}

func TestCleanupOldReleases_DryRun(t *testing.T) {
	cfg := newTestCfg("docker")
	assert.NoError(t, CleanupOldReleases(cfg))
}

func TestCleanupOldReleases_Live(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFakeExec(t, 0)
	assert.NoError(t, CleanupOldReleases(cfg))
}

func TestCopySharedFiles_DryRun(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	assert.NoError(t, copySharedFiles(cfg))
}

func TestCopySharedFiles_NoFiles(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	cfg := newTestCfg("docker")
	assert.NoError(t, copySharedFiles(cfg))
}

// TestDeployDocker_CopyComposeFails — docker deploy needs the compose
// file copied; we fail the scp that uploads it.
func TestDeployDocker_CopyComposeFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFailOnArg(t, "compose.yaml")
	err := DeployDocker(cfg)
	require.Error(t, err)
}

// TestCopySharedFiles_ConfigYamlCopyFails — config.yaml exists in the
// project but the scp copy fails → copySharedFiles returns the error.
func TestCopySharedFiles_ConfigYamlCopyFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFailOnArg(t, "config.yaml")
	err := copySharedFiles(cfg)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "config.yaml")
}

func TestDeployDocker_MigrationFailureIsNonFatal(t *testing.T) {
	withinProject(t)
	withFailOnArg(t, "migrate up")
	cfg := newTestCfg("docker")
	cfg.DryRun = false

	assert.NoError(t, DeployDocker(cfg), "a failed migration must not fail the deploy")
}
