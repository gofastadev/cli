package deploy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

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
func withinProject(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	// Create directories & files the deploy functions expect
	require.NoError(t, os.MkdirAll("deployments/docker", 0o755))
	require.NoError(t, os.MkdirAll("deployments/nginx", 0o755))
	require.NoError(t, os.MkdirAll("deployments/systemd", 0o755))
	require.NoError(t, os.MkdirAll("db/migrations", 0o755))
	require.NoError(t, os.MkdirAll("templates", 0o755))
	require.NoError(t, os.MkdirAll("configs", 0o755))
	require.NoError(t, os.MkdirAll("app/main", 0o755))
	require.NoError(t, os.WriteFile("deployments/docker/compose.production.yaml", []byte("services: {}\n"), 0o644))
	require.NoError(t, os.WriteFile("deployments/nginx/app.conf", []byte("server {}\n"), 0o644))
	require.NoError(t, os.WriteFile("deployments/systemd/app.service", []byte("[Unit]\n"), 0o644))
	require.NoError(t, os.WriteFile("config.yaml", []byte("server: {port: \"8080\"}\n"), 0o644))
	require.NoError(t, os.WriteFile(".env", []byte("K=V\n"), 0o644))
	require.NoError(t, os.WriteFile("db/migrations/1.sql", []byte("-- migration\n"), 0o644))
}

var _ = httptest.NewServer

var _ = http.StatusOK

var _ = filepath.Join

const fakeEnvExitCode = "GOFASTA_DEPLOY_FAKE_EXIT"

const fakeEnvStdout = "GOFASTA_DEPLOY_FAKE_STDOUT"

func fakeExecCommand(exitCode int, stdout string) func(name string, args ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		cs := make([]string, 0, 3+len(args))
		cs = append(cs, "-test.run=TestDeployHelperProcess", "--", name)
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_DEPLOY_HELPER=1",
			fakeEnvExitCode+"="+strconv.Itoa(exitCode),
			fakeEnvStdout+"="+stdout,
		)
		return cmd
	}
}

func withFakeExec(t *testing.T, exitCode int) {
	t.Helper()
	withFakeExecStdout(t, exitCode, "")
}

func withFakeExecStdout(t *testing.T, exitCode int, stdout string) {
	t.Helper()
	origCmd := execCommand
	origLook := execLookPath
	execCommand = fakeExecCommand(exitCode, stdout)
	execLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	t.Cleanup(func() {
		execCommand = origCmd
		execLookPath = origLook
	})
}

// stagedFakeExec exits with codes[i] on the i-th call and repeats the final
// value afterwards. stdouts follows the same pattern (empty if omitted).
func stagedFakeExec(t *testing.T, codes []int, stdouts []string) {
	t.Helper()
	origCmd := execCommand
	origLook := execLookPath
	call := 0
	execCommand = func(name string, args ...string) *exec.Cmd {
		code := codes[len(codes)-1]
		if call < len(codes) {
			code = codes[call]
		}
		out := ""
		if len(stdouts) > 0 {
			out = stdouts[len(stdouts)-1]
			if call < len(stdouts) {
				out = stdouts[call]
			}
		}
		call++
		return fakeExecCommand(code, out)(name, args...)
	}
	execLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	t.Cleanup(func() {
		execCommand = origCmd
		execLookPath = origLook
	})
}

// withFailOnArg sets execCommand to fail (exit 1) whenever the concatenated
// command line (name + joined args) contains the given substring.
func withFailOnArg(t *testing.T, substr string) {
	t.Helper()
	origCmd := execCommand
	origLook := execLookPath
	execCommand = func(name string, args ...string) *exec.Cmd {
		code := 0
		joined := name
		for _, a := range args {
			joined += " " + a
		}
		if contains(joined, substr) {
			code = 1
		}
		return fakeExecCommand(code, "")(name, args...)
	}
	execLookPath = func(n string) (string, error) { return "/usr/bin/" + n, nil }
	t.Cleanup(func() {
		execCommand = origCmd
		execLookPath = origLook
	})
}
