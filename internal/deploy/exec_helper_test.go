package deploy

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
)

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

func TestDeployHelperProcess(t *testing.T) {
	if os.Getenv("GOFASTA_WANT_DEPLOY_HELPER") != "1" {
		return
	}
	if out := os.Getenv(fakeEnvStdout); out != "" {
		os.Stdout.WriteString(out)
	}
	code, _ := strconv.Atoi(os.Getenv(fakeEnvExitCode))
	os.Exit(code)
}
