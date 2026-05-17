package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
)

// fakeExitCode is the exit code the fake child process will exit with.
// Tests set this via env before invoking a command that uses execCommand.
const (
	fakeEnvExitCode = "GOFASTA_FAKE_EXIT"
	fakeEnvVersion  = "GOFASTA_FAKE_VERSION"
)

// fakeExecCommand returns a function suitable for assignment to execCommand
// which re-execs the test binary as a fake subprocess. The subprocess runs
// TestHelperProcess and exits with the code provided in GOFASTA_FAKE_EXIT
// (default 0). This is the canonical os/exec testing pattern from the Go stdlib.
func fakeExecCommand(exitCode int) func(name string, args ...string) *exec.Cmd {
	return fakeExecCommandWithVersion(exitCode, "")
}

// fakeExecCommandWithVersion is like fakeExecCommand but also injects a version
// string that the helper process will print when any arg is "--version". Used
// by upgrade tests that need readBinaryVersion to return a specific value.
func fakeExecCommandWithVersion(exitCode int, version string) func(name string, args ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		cs := make([]string, 0, 3+len(args))
		cs = append(cs, "-test.run=TestHelperProcess", "--", name)
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"="+strconv.Itoa(exitCode),
			fakeEnvVersion+"="+version,
		)
		return cmd
	}
}

// withFakeExec swaps execCommand to a fake with the given exit code for the
// duration of the test and restores the original afterwards.
//
// It ALSO stubs the three preflight probe functions to return probeOK.
// Before the migrate-version → TCP-probe refactor, the DB probe shelled
// out to `migrate` and was fully covered by execCommand's fake — so
// every dev/runDev test got an "OK" probe automatically just by calling
// withFakeExec. After the refactor the DB probe is a `net.DialTimeout`
// that bypasses execCommand, which would dial real localhost:5432
// during tests and fail. Stubbing the seams here preserves the implicit
// contract every dev test was already relying on: withFakeExec means
// "all deps are happy, focus the test on the shell-out path".
func withFakeExec(t *testing.T, exitCode int) {
	t.Helper()
	orig := execCommand
	execCommand = fakeExecCommand(exitCode)
	t.Cleanup(func() { execCommand = orig })
	stubProbesOK(t)
}

// stubProbesOK swaps all three preflight probe functions to report OK
// for the duration of the test. Tests that want to exercise the
// preflight menu directly assign their own probe stubs *after*
// calling withFakeExec (the last assignment wins; t.Cleanup restores
// the original on exit either way).
func stubProbesOK(t *testing.T) {
	t.Helper()
	origDB, origCache, origQueue := probeDatabaseFn, probeCacheFn, probeQueueFn
	probeDatabaseFn = func() probeResult {
		return probeResult{Dep: "database", Status: probeOK, Endpoint: "stubbed"}
	}
	probeCacheFn = func() probeResult {
		return probeResult{Dep: "cache", Status: probeNotConfigured}
	}
	probeQueueFn = func() probeResult {
		return probeResult{Dep: "queue", Status: probeNotConfigured}
	}
	t.Cleanup(func() {
		probeDatabaseFn = origDB
		probeCacheFn = origCache
		probeQueueFn = origQueue
	})
}

// withFakeExecVersion is withFakeExec with a scripted --version response.
//
//nolint:unparam // exitCode kept symmetrical with fakeExecCommandWithVersion for future use.
func withFakeExecVersion(t *testing.T, exitCode int, version string) {
	t.Helper()
	orig := execCommand
	execCommand = fakeExecCommandWithVersion(exitCode, version)
	t.Cleanup(func() { execCommand = orig })
}

// TestHelperProcess is not a real test — it's the fake subprocess invoked by
// fakeExecCommand. If any argument is "--version" and GOFASTA_FAKE_VERSION is
// set, it prints a Cobra-style version line and exits 0. Otherwise it exits
// with GOFASTA_FAKE_EXIT.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GOFASTA_WANT_HELPER_PROCESS") != "1" {
		return
	}
	// Find the `--` separator: everything after it is the fake command + args.
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	if v := os.Getenv(fakeEnvVersion); v != "" {
		for _, a := range args {
			if a == "--version" {
				fmt.Fprintf(os.Stdout, "gofasta version %s\n", v)
				os.Exit(0)
			}
		}
	}
	// GOFASTA_FAKE_STDOUT lets callers script the child's stdout —
	// used by dev_services_success_test.go to simulate the JSON that
	// `docker compose config` / `docker compose ps` emit.
	if stdout := os.Getenv("GOFASTA_FAKE_STDOUT"); stdout != "" {
		fmt.Fprint(os.Stdout, stdout)
	}
	code, _ := strconv.Atoi(os.Getenv(fakeEnvExitCode))
	os.Exit(code)
}
