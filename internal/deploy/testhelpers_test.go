package deploy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	// Mirrors the real template's deploy contract (image pinned via
	// APP_IMAGE, stable project name) — the previous "services: {}" stub
	// hid the fact that the deploy never referenced the transferred image.
	composeStub := "name: ${PROJECT_NAME:-testapp}\nservices:\n  app:\n    image: ${APP_IMAGE:-testapp:latest}\n"
	require.NoError(t, os.WriteFile("deployments/docker/compose.production.yaml", []byte(composeStub), 0o644))
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

// recordedCmds collects every command line the fake exec receives (name +
// args joined with spaces), in call order. Reset by each with*Exec helper;
// tests assert on COMMAND CONTENT and ORDER with it — an exit code alone
// can't prove the right command ran.
var recordedCmds []string

func recordCmd(name string, args []string) {
	recordedCmds = append(recordedCmds, strings.Join(append([]string{name}, args...), " "))
}

// commandsContaining returns the recorded commands that contain substr.
func commandsContaining(substr string) []string {
	var out []string
	for _, c := range recordedCmds {
		if contains(c, substr) {
			out = append(out, c)
		}
	}
	return out
}

// commandIndex returns the index of the first recorded command containing
// substr, or -1.
func commandIndex(substr string) int {
	for i, c := range recordedCmds {
		if contains(c, substr) {
			return i
		}
	}
	return -1
}

func fakeExecCommand(exitCode int, stdout string) func(name string, args ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		recordCmd(name, args)
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

// fakeRule drives respondingFakeExec: the FIRST rule whose Match substring
// appears in the command line decides that call's exit code and stdout.
type fakeRule struct {
	Match  string
	Exit   int
	Stdout string
}

// respondingFakeExec answers each command by matching its content against
// rules instead of by call index — robust to pipeline reorderings, and the
// only way to script different stdout for readlink vs cat vs ls.
func respondingFakeExec(t *testing.T, rules []fakeRule) {
	t.Helper()
	origCmd := execCommand
	origLook := execLookPath
	recordedCmds = nil
	execCommand = func(name string, args ...string) *exec.Cmd {
		joined := strings.Join(append([]string{name}, args...), " ")
		for _, r := range rules {
			if contains(joined, r.Match) {
				return fakeExecCommand(r.Exit, r.Stdout)(name, args...)
			}
		}
		return fakeExecCommand(0, "")(name, args...)
	}
	execLookPath = func(n string) (string, error) { return "/usr/bin/" + n, nil }
	t.Cleanup(func() {
		execCommand = origCmd
		execLookPath = origLook
	})
}

func withFakeExec(t *testing.T, exitCode int) {
	t.Helper()
	withFakeExecStdout(t, exitCode, "")
}

func withFakeExecStdout(t *testing.T, exitCode int, stdout string) {
	t.Helper()
	recordedCmds = nil
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
// value afterwards.
func stagedFakeExec(t *testing.T, codes []int) {
	t.Helper()
	recordedCmds = nil
	origCmd := execCommand
	origLook := execLookPath
	call := 0
	execCommand = func(name string, args ...string) *exec.Cmd {
		code := codes[len(codes)-1]
		if call < len(codes) {
			code = codes[call]
		}
		call++
		return fakeExecCommand(code, "")(name, args...)
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
	recordedCmds = nil
	origCmd := execCommand
	origLook := execLookPath
	execCommand = func(name string, args ...string) *exec.Cmd {
		code := 0
		joined := strings.Join(append([]string{name}, args...), " ")
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
