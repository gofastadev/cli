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
	withFailOnArg(t, "compose.yaml up -d")
	err := DeployDocker(cfg)
	assert.Error(t, err)
}

// TestDeployDocker_HealthCheckFails — first deploy (no previous release):
// the failure must surface AND nothing may be rolled back to; the broken
// release stays up for inspection.
func TestDeployDocker_HealthCheckFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	cfg.HealthTimeout = 0
	withFailOnArg(t, "wget -qO /dev/null")
	err := DeployDocker(cfg)
	require.Error(t, err)
	assert.Empty(t, commandsContaining("rm -rf /opt/test/releases/20260101-000000"),
		"a failed FIRST deploy is left in place for inspection")
}

// TestDeployDocker_Failure_AutoRollsBack — with a previous release on
// record, a failed deploy must restore it: previous image up, symlink
// back, failed release directory and image removed.
func TestDeployDocker_Failure_AutoRollsBack(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	// Migration failure triggers the same auto-rollback path as a failed
	// health gate; here the restored release's health probe succeeds, so
	// the FULL rollback (restore + pointer + failed-release removal) runs.
	respondingFakeExec(t, []fakeRule{
		{Match: "readlink /opt/test/current", Stdout: "/opt/test/releases/20251231-000000"},
		{Match: "cat /opt/test/releases/20251231-000000/RELEASE_IMAGE", Stdout: "testapp:20251231-000000"},
		{Match: "migrate up", Exit: 1},
	})

	err := DeployDocker(cfg)
	require.Error(t, err)

	restore := commandsContaining("APP_IMAGE=testapp:20251231-000000")
	require.NotEmpty(t, restore, "auto-rollback must restart the previous release's pinned image")
	assert.NotEmpty(t, commandsContaining("ln -sfn /opt/test/releases/20251231-000000 /opt/test/current"),
		"auto-rollback must restore the current pointer")
	assert.NotEmpty(t, commandsContaining("rm -rf /opt/test/releases/20260101-000000"),
		"the failed release directory must be removed so `deploy rollback` can never select it")
}

// TestDeployDocker_PinsImageAndProject — the deploy's cutover must reference
// the transferred image (APP_IMAGE) under the FIXED compose project, and
// must record the tag in RELEASE_IMAGE before cutover.
func TestDeployDocker_PinsImageAndProject(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	respondingFakeExec(t, nil)

	require.NoError(t, DeployDocker(cfg))

	up := commandsContaining("compose.yaml up -d")
	require.NotEmpty(t, up)
	assert.Contains(t, up[0], "APP_IMAGE=testapp:20260101-000000",
		"the transferred image must be the one compose runs")
	assert.Contains(t, up[0], "docker compose -p testapp",
		"a fixed project name is required for container/volume reuse across releases")
	assert.NotEmpty(t, commandsContaining("printf '%s' testapp:20260101-000000 > /opt/test/releases/20260101-000000/RELEASE_IMAGE"),
		"the release must record its image tag for rollback")

	migrate := commandsContaining("/app migrate up")
	require.NotEmpty(t, migrate)
	assert.Contains(t, migrate[0], "docker compose -p testapp -f compose.yaml exec -T app",
		"migrations must target the compose service, not a hardcoded container name")

	// Ordering: image transfer < cutover < health probe.
	assert.Less(t, commandIndex("docker save testapp:20260101-000000"), commandIndex("compose.yaml up -d"))
	assert.Less(t, commandIndex("compose.yaml up -d"), commandIndex("wget -qO /dev/null"))
}

// TestDeployDocker_CleanupExcludesCurrentAndPrunesImages — the cleanup
// script must be name-sorted, keep whatever `current` resolves to, and
// remove pruned releases' images.
func TestDeployDocker_CleanupExcludesCurrentAndPrunesImages(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	respondingFakeExec(t, nil)

	require.NoError(t, DeployDocker(cfg))

	cleanup := commandsContaining("sort -r")
	require.NotEmpty(t, cleanup, "cleanup must order releases by name, not mtime")
	assert.Contains(t, cleanup[0], `[ "$r" = "$current" ]`, "the live release must never be pruned")
	assert.Contains(t, cleanup[0], "docker rmi", "pruned releases' images must be removed")
	assert.NotContains(t, cleanup[0], "ls -1t", "mtime ordering is the bug this replaced")
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

// TestDeployDocker_MigrationFailureAborts — a failed migration must fail
// the deploy (new code must not serve against a schema it didn't get).
func TestDeployDocker_MigrationFailureAborts(t *testing.T) {
	withinProject(t)
	withFailOnArg(t, "migrate up")
	cfg := newTestCfg("docker")
	cfg.DryRun = false

	err := DeployDocker(cfg)
	require.Error(t, err, "a failed migration must abort the deploy")
}
