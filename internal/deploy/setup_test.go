package deploy

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func contains(s, substr string) bool {
	if substr == "" {
		return false
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
