package commands

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
)

// fakeExitCode is the exit code the fake child process will exit with.
// Tests set this via env before invoking a command that uses execCommand.
const fakeEnvExitCode = "GOFASTA_FAKE_EXIT"

// fakeExecCommand returns a function suitable for assignment to execCommand
// which re-execs the test binary as a fake subprocess. The subprocess runs
// TestHelperProcess and exits with the code provided in GOFASTA_FAKE_EXIT
// (default 0). This is the canonical os/exec testing pattern from the Go stdlib.
func fakeExecCommand(exitCode int) func(name string, args ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		cs := make([]string, 0, 3+len(args))
		cs = append(cs, "-test.run=TestHelperProcess", "--", name)
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"="+strconv.Itoa(exitCode),
		)
		return cmd
	}
}

// withFakeExec swaps execCommand to a fake with the given exit code for the
// duration of the test and restores the original afterwards.
func withFakeExec(t *testing.T, exitCode int) {
	t.Helper()
	orig := execCommand
	execCommand = fakeExecCommand(exitCode)
	t.Cleanup(func() { execCommand = orig })
}

// TestHelperProcess is not a real test — it's the fake subprocess invoked by
// fakeExecCommand. It simply exits with the code in GOFASTA_FAKE_EXIT.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GOFASTA_WANT_HELPER_PROCESS") != "1" {
		return
	}
	code, _ := strconv.Atoi(os.Getenv(fakeEnvExitCode))
	os.Exit(code)
}
