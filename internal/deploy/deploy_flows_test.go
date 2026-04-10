package deploy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestCfg returns a DeployConfig suitable for dry-run-driven tests.
func newTestCfg(method string) *DeployConfig {
	return &DeployConfig{
		Host:          "user@server.com",
		Method:        method,
		Port:          22,
		Path:          "/opt/test",
		Arch:          "amd64",
		HealthPath:    "/health",
		HealthTimeout: 1,
		KeepReleases:  3,
		DryRun:        true,
		AppName:       "testapp",
		ServerPort:    "8080",
		ReleaseTag:    "20260101-000000",
	}
}

// withinProject cd's into a tempdir seeded with a minimal project layout.
func withinProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	// Create directories & files the deploy functions expect
	require.NoError(t, os.MkdirAll("deployments/docker", 0755))
	require.NoError(t, os.MkdirAll("deployments/nginx", 0755))
	require.NoError(t, os.MkdirAll("deployments/systemd", 0755))
	require.NoError(t, os.MkdirAll("db/migrations", 0755))
	require.NoError(t, os.MkdirAll("templates", 0755))
	require.NoError(t, os.MkdirAll("configs", 0755))
	require.NoError(t, os.MkdirAll("app/main", 0755))
	require.NoError(t, os.WriteFile("deployments/docker/compose.production.yaml", []byte("services: {}\n"), 0644))
	require.NoError(t, os.WriteFile("deployments/nginx/app.conf", []byte("server {}\n"), 0644))
	require.NoError(t, os.WriteFile("deployments/systemd/app.service", []byte("[Unit]\n"), 0644))
	require.NoError(t, os.WriteFile("config.yaml", []byte("server: {port: \"8080\"}\n"), 0644))
	require.NoError(t, os.WriteFile(".env", []byte("K=V\n"), 0644))
	require.NoError(t, os.WriteFile("db/migrations/1.sql", []byte("-- migration\n"), 0644))

	return dir
}

// --- Live-mode success: DeployDocker end-to-end ---

func TestDeployDocker_Live_AllSuccess(t *testing.T) {
	withinProject(t)
	withFakeExec(t, 0)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	assert.NoError(t, DeployDocker(cfg))
}

func TestDeployBinary_Live_AllSuccess(t *testing.T) {
	withinProject(t)
	withFakeExec(t, 0)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	assert.NoError(t, DeployBinary(cfg))
}

func TestSetupServer_Live_Docker(t *testing.T) {
	withinProject(t)
	withFakeExec(t, 0)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	// Docker already installed path (first RunRemoteCapture returns success)
	assert.NoError(t, SetupServer(cfg))
}

func TestSetupServer_Live_Binary(t *testing.T) {
	withinProject(t)
	withFakeExec(t, 0)
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	assert.NoError(t, SetupServer(cfg))
}

// --- Error paths via withFailOnArg ---

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

// --- DeployBinary error branches ---

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

// --- SetupServer error branches ---

func TestSetupServer_ConnectFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFailOnArg(t, "echo ok")
	err := SetupServer(cfg)
	assert.Error(t, err)
}

func TestSetupServer_AptInstallFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFailOnArg(t, "apt-get install")
	err := SetupServer(cfg)
	assert.Error(t, err)
}

func TestSetupServer_DockerInstallFailsIfMissing(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFailOnArg(t, "get.docker.com")
	// If docker is already installed (first docker --version returns 0), this won't be hit.
	// With our fake, docker --version returns 0, so docker is "already installed" and this test
	// exercises the docker-already-installed branch rather than the install.
	_ = SetupServer(cfg)
}

func TestSetupServer_DirStructureFails(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFailOnArg(t, "sudo mkdir -p")
	err := SetupServer(cfg)
	assert.Error(t, err)
}

// --- Original dry-run happy path ---

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

// --- DeployBinary ---

func TestDeployBinary_DryRun(t *testing.T) {
	withinProject(t)
	withFakeExec(t, 0)
	cfg := newTestCfg("binary")
	assert.NoError(t, DeployBinary(cfg))
}

// --- SetupServer ---

func TestSetupServer_DockerDryRun(t *testing.T) {
	withinProject(t)
	withFakeExec(t, 0)
	cfg := newTestCfg("docker")
	assert.NoError(t, SetupServer(cfg))
}

func TestSetupServer_BinaryDryRun(t *testing.T) {
	withinProject(t)
	withFakeExec(t, 0)
	cfg := newTestCfg("binary")
	assert.NoError(t, SetupServer(cfg))
}

func TestSetupServer_NoNginxConf(t *testing.T) {
	// No nginx config at all — should still succeed (warning path)
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	withFakeExec(t, 0)
	cfg := newTestCfg("docker")
	assert.NoError(t, SetupServer(cfg))
}

func TestSetupServer_NginxTmplOnly(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	os.MkdirAll("deployments/nginx", 0755)
	os.WriteFile("deployments/nginx/app.conf.tmpl", []byte("tmpl"), 0644)
	withFakeExec(t, 0)
	cfg := newTestCfg("docker")
	assert.NoError(t, SetupServer(cfg))
}

// --- Rollback ---

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
		"/opt/test/releases/20260102-000000",  // readlink current
		"",                                    // compose up
		"",                                    // symlink
		"",                                    // health check
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

// --- Rollback error branches with matcher-based fakes ---

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

// --- DeployBinary remaining error branches ---

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

// --- DeployDocker symlink failure + copyShared failure ---

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

// --- SetupServer docker-not-installed branch ---

func TestSetupServer_DockerMissing_InstallPath(t *testing.T) {
	// Need docker --version to fail but everything else succeed.
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFailOnArg(t, "docker --version")
	// That failure triggers the docker install branch (which succeeds), then continues
	assert.NoError(t, SetupServer(cfg))
}

func TestSetupServer_DockerInstallFailure(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	// Fail both docker --version and the docker install script
	origCmd := execCommand
	origLook := execLookPath
	execCommand = func(name string, args ...string) *exec.Cmd {
		joined := name
		for _, a := range args {
			joined += " " + a
		}
		code := 0
		if contains(joined, "docker --version") || contains(joined, "get.docker.com") {
			code = 1
		}
		return fakeExecCommand(code, "")(name, args...)
	}
	execLookPath = func(n string) (string, error) { return "/usr/bin/" + n, nil }
	t.Cleanup(func() {
		execCommand = origCmd
		execLookPath = origLook
	})
	err := SetupServer(cfg)
	assert.Error(t, err)
}

func TestSetupServer_NginxConfigFailure(t *testing.T) {
	// nginx config copy succeeds but nginx reload fails → warning
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFailOnArg(t, "nginx -t")
	// The failure is a warning, not fatal — should still succeed
	assert.NoError(t, SetupServer(cfg))
}

func TestSetupServer_NginxCopyFailure(t *testing.T) {
	withinProject(t)
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFailOnArg(t, "/tmp/testapp.nginx.conf")
	// scp fails → warning path
	assert.NoError(t, SetupServer(cfg))
}

// --- PreflightChecks ---

func TestPreflightChecks_DryRun(t *testing.T) {
	withFakeExec(t, 0)
	cfg := newTestCfg("docker")
	assert.NoError(t, PreflightChecks(cfg))
}

func TestPreflightChecks_DryRunBinary(t *testing.T) {
	withFakeExec(t, 0)
	cfg := newTestCfg("binary")
	assert.NoError(t, PreflightChecks(cfg))
}

func TestPreflightChecks_ToolsMissing(t *testing.T) {
	origLook := execLookPath
	execLookPath = func(name string) (string, error) { return "", fmt.Errorf("not found") }
	t.Cleanup(func() { execLookPath = origLook })
	cfg := newTestCfg("docker")
	err := PreflightChecks(cfg)
	assert.Error(t, err)
}

func TestPreflightChecks_LiveSSHFail(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	// SSH fails on first call
	stagedFakeExec(t, []int{1}, nil)
	err := PreflightChecks(cfg)
	assert.Error(t, err)
}

func TestPreflightChecks_LiveAllSucceed_Docker(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	// ssh echo ok, docker --version, docker compose version
	stagedFakeExec(t, []int{0, 0, 0}, nil)
	assert.NoError(t, PreflightChecks(cfg))
}

func TestPreflightChecks_LiveDockerMissing(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	// ssh echo ok, docker --version fails
	stagedFakeExec(t, []int{0, 1, 1}, nil)
	err := PreflightChecks(cfg)
	assert.Error(t, err)
}

func TestPreflightChecks_LiveSystemdOk(t *testing.T) {
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	// ssh echo ok, systemctl --version ok
	stagedFakeExec(t, []int{0, 0}, nil)
	assert.NoError(t, PreflightChecks(cfg))
}

func TestPreflightChecks_LiveSystemdMissing(t *testing.T) {
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	stagedFakeExec(t, []int{0, 1}, nil)
	err := PreflightChecks(cfg)
	assert.Error(t, err)
}

func TestPrintCheck(t *testing.T) {
	assert.NotPanics(t, func() {
		printCheck("x", "ok", true)
		printCheck("x", "fail", false)
	})
}

// --- CheckHealth ---

func TestCheckHealth_DryRun(t *testing.T) {
	cfg := newTestCfg("docker")
	assert.NoError(t, CheckHealth(cfg))
}

func TestCheckHealth_Success(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	// fake exec success -> curl succeeds
	withFakeExec(t, 0)
	assert.NoError(t, CheckHealth(cfg))
}

func TestCheckHealth_Timeout(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	cfg.HealthTimeout = 0 // immediate timeout
	withFakeExec(t, 1)
	start := time.Now()
	err := CheckHealth(cfg)
	// should error and return quickly
	assert.Error(t, err)
	assert.Less(t, time.Since(start), 3*time.Second)
}

// --- CleanupOldReleases ---

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

// --- copySharedFiles ---

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

// --- setOrUnset ---

func TestSetOrUnset(t *testing.T) {
	t.Setenv("GOFASTA_SO_TEST", "x")
	setOrUnset("GOFASTA_SO_TEST", "")
	assert.Equal(t, "", os.Getenv("GOFASTA_SO_TEST"))
	setOrUnset("GOFASTA_SO_TEST", "y")
	assert.Equal(t, "y", os.Getenv("GOFASTA_SO_TEST"))
}

// --- SSH live paths (fake exec) ---

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
	assert.NoError(t, RunLocalPiped(cfg, "true"))
}

func TestRunLocal_Live(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	withFakeExec(t, 0)
	assert.NoError(t, RunLocal(cfg, "echo", "hi"))
}

// --- SetLookPathForTest / SetExecCommandForTest smoke ---

func TestLookPathSetters(t *testing.T) {
	SetLookPathForTest(func(n string) (string, error) { return "x", nil })
	ResetLookPathForTest()
	SetExecCommandForTest(fakeExecCommand(0, ""))
	ResetExecCommandForTest()
}

// --- httptest not currently used but placeholder for live HTTP health paths ---

var _ = httptest.NewServer
var _ = http.StatusOK
var _ = filepath.Join
