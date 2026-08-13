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
	// No systemd service file → hard error: the unit is not optional, a
	// deploy without it leaves nothing managing the process.
	assert.Error(t, DeployBinary(cfg))
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
	// Fail the cleanup script CleanupOldReleases runs — cleanup problems
	// must stay non-fatal (the deploy itself succeeded).
	withFailOnArg(t, "sort -r")
	assert.NoError(t, DeployBinary(cfg))
}

// TestDeployBinary_MigrationFailureAborts — migrations run BEFORE cutover
// with the release's own binary; a failure must abort the deploy without
// ever touching the running service.
func TestDeployBinary_MigrationFailureAborts(t *testing.T) {
	withinProject(t)
	withFailOnArg(t, "migrate up")
	cfg := newTestCfg("binary")
	cfg.DryRun = false

	err := DeployBinary(cfg)
	require.Error(t, err, "a failed migration must abort the deploy")
	assert.Empty(t, commandsContaining("systemctl restart"),
		"the running service must not be restarted when migrations fail")
	assert.Empty(t, commandsContaining("sudo cp /opt/test/releases/20260101-000000/testapp /usr/local/bin"),
		"the failed release's binary must not be installed")
}

// TestDeployBinary_NoEtcAppLayout — the /etc/<app> scheme is dead; nothing
// may reference it (the unit runs from /opt/<app>/current with
// shared/.env).
func TestDeployBinary_NoEtcAppLayout(t *testing.T) {
	withinProject(t)
	withFakeExec(t, 0)
	cfg := newTestCfg("binary")
	cfg.DryRun = false

	require.NoError(t, DeployBinary(cfg))
	assert.Empty(t, commandsContaining("/etc/testapp"),
		"binary deploys must not touch the abandoned /etc/<app> layout")
}

// TestDeployBinary_Failure_AutoRollsBack — with a previous release, a
// failed cutover must reinstall its binary and restore the pointer. Only
// the NEW release's install fails here, so the restore (a different
// release path) and its health probe succeed and the full rollback runs.
func TestDeployBinary_Failure_AutoRollsBack(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	respondingFakeExec(t, []fakeRule{
		{Match: "readlink /opt/test/current", Stdout: "/opt/test/releases/20251231-000000"},
		{Match: "sudo cp /opt/test/releases/20260101-000000/testapp", Exit: 1},
	})

	err := DeployBinary(cfg)
	require.Error(t, err)
	assert.NotEmpty(t, commandsContaining("sudo cp /opt/test/releases/20251231-000000/testapp /usr/local/bin/testapp"),
		"auto-rollback must reinstall the previous release's binary")
	assert.NotEmpty(t, commandsContaining("rm -rf /opt/test/releases/20260101-000000"),
		"the failed release directory must be removed")
}

// TestDeployBinary_CopyDirFailureIsFatal — a failed migrations upload used
// to be silently swallowed and resurfaced later as a migration error.
func TestDeployBinary_CopyDirFailureIsFatal(t *testing.T) {
	withinProject(t)
	withFailOnArg(t, "db/migrations")
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	assert.Error(t, DeployBinary(cfg), "a failed supporting-file upload must fail the deploy")
}

// TestDeployBinary_LocalTmpDirCreateFails — a regular file named "tmp" in
// the project directory makes os.MkdirAll fail before the cross-compile.
func TestDeployBinary_LocalTmpDirCreateFails(t *testing.T) {
	withinProject(t)
	require.NoError(t, os.WriteFile("tmp", []byte("not a directory"), 0o644))
	withFakeExec(t, 0)
	cfg := newTestCfg("binary")
	cfg.DryRun = false

	err := DeployBinary(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot create local tmp directory")
}

// TestDeployBinary_LinkSharedFails — the shared-config link into the release
// directory must fail the deploy loudly (compose/systemd would otherwise run
// with empty credentials).
func TestDeployBinary_LinkSharedFails(t *testing.T) {
	withinProject(t)
	withFailOnArg(t, "for f in .env")
	cfg := newTestCfg("binary")
	cfg.DryRun = false

	err := DeployBinary(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to link shared config")
}

// TestDeployBinary_InstallServiceFails — the remote install of the systemd
// unit (cp into /etc/systemd/system + daemon-reload) fails.
func TestDeployBinary_InstallServiceFails(t *testing.T) {
	withinProject(t)
	withFailOnArg(t, "daemon-reload")
	cfg := newTestCfg("binary")
	cfg.DryRun = false

	err := DeployBinary(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install systemd service")
}

// TestDeployBinary_ReadlinkFails_TreatedAsFirstDeploy — a failing readlink
// on the current pointer means "no previous release": the deploy proceeds as
// a first deploy and still succeeds.
func TestDeployBinary_ReadlinkFails_TreatedAsFirstDeploy(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	// The cleanup script also contains "readlink /opt/test/current", so this
	// rule additionally exercises the non-fatal cleanup warning.
	respondingFakeExec(t, []fakeRule{
		{Match: "readlink /opt/test/current", Exit: 1},
	})

	assert.NoError(t, DeployBinary(cfg))
}

// TestDeployBinary_AutoRollbackCutoverFails — the new release's cutover
// fails AND reinstalling the previous release fails too: the deploy must
// surface the compounded rollback failure.
func TestDeployBinary_AutoRollbackCutoverFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	// Both cutovers (new release and previous release) stage the binary at
	// /usr/local/bin/testapp.new, so one rule fails them both.
	respondingFakeExec(t, []fakeRule{
		{Match: "readlink /opt/test/current", Stdout: "/opt/test/releases/20251231-000000"},
		{Match: "/usr/local/bin/testapp.new", Exit: 1},
	})

	err := DeployBinary(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "automatic rollback failed")
	assert.NotEmpty(t, commandsContaining("sudo cp /opt/test/releases/20251231-000000/testapp /usr/local/bin/testapp.new"),
		"the previous release's reinstall must have been attempted")
}

// TestDeployBinary_AutoRollbackUnhealthy — the restored previous release
// fails its own health gate after the rollback cutover succeeded.
func TestDeployBinary_AutoRollbackUnhealthy(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	// HealthTimeout 0 fails BOTH health gates: the deploy's (triggering the
	// rollback) and the post-rollback probe of the restored release.
	cfg.HealthTimeout = 0
	respondingFakeExec(t, []fakeRule{
		{Match: "readlink /opt/test/current", Stdout: "/opt/test/releases/20251231-000000"},
	})

	err := DeployBinary(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restored release is unhealthy")
}

// TestDeployBinary_RemoveFailedReleaseWarns — removing the failed release
// directory is best-effort: its own failure is a warning, and the original
// migration error is what surfaces.
func TestDeployBinary_RemoveFailedReleaseWarns(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	respondingFakeExec(t, []fakeRule{
		{Match: "migrate up", Exit: 1},
		{Match: "rm -rf /opt/test/releases/20260101-000000", Exit: 1},
	})

	err := DeployBinary(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database migrations failed",
		"the migration failure, not the cleanup failure, must be the reported error")
	assert.NotEmpty(t, commandsContaining("rm -rf /opt/test/releases/20260101-000000"),
		"the failed release removal must have been attempted")
}
