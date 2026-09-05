package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/commands/configutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runMigrations and runMigrateUp were the original best-effort migration
// entrypoints in the dev command. The live dev pipeline now uses
// runMigrationsWithCount (which has no retry), so these helpers were
// removed from production source. They live here so the retry-behavior
// tests below (SuccessOnRetry, FailsBothAttempts, etc.) keep exercising
// the two-attempt logic verbatim.
func runMigrations() error {
	if _, err := execLookPath("migrate"); err != nil {
		return fmt.Errorf("migrate CLI not found on $PATH — install with:\n" +
			"  go install -tags 'postgres mysql sqlite3 sqlserver clickhouse' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1")
	}
	dbURL := configutil.BuildMigrationURL()
	if err := runMigrateUp(dbURL); err == nil {
		return nil
	}
	cliout.Hint("Database not ready, retrying in 2 seconds...")
	time.Sleep(2 * time.Second)
	return runMigrateUp(dbURL)
}

// runMigrations with migrate not installed — returns a clear error message
// mentioning where to install from.
func TestRunMigrations_MigrateNotFound(t *testing.T) {
	setupDevTempdir(t)
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "", fmt.Errorf("not found") }
	t.Cleanup(func() { execLookPath = origLookPath })

	err := runMigrations()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "migrate CLI not found")
	assert.Contains(t, err.Error(), "v4.18.1")
}

// runMigrations succeeds on first attempt.
func TestRunMigrations_SuccessFirstAttempt(t *testing.T) {
	setupDevTempdir(t)
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "/usr/bin/migrate", nil }
	t.Cleanup(func() { execLookPath = origLookPath })
	withFakeExec(t, 0)

	err := runMigrations()
	assert.NoError(t, err)
}

// runMigrations fails first attempt but succeeds on retry.
func TestRunMigrations_SuccessOnRetry(t *testing.T) {
	setupDevTempdir(t)
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "/usr/bin/migrate", nil }
	t.Cleanup(func() { execLookPath = origLookPath })
	// First call (migrate up) fails, second call (retry) succeeds.
	stagedFakeExec(t, 1, 0)

	err := runMigrations()
	assert.NoError(t, err)
}

// runMigrations fails both attempts — returns the error from the second try.
func TestRunMigrations_FailsBothAttempts(t *testing.T) {
	setupDevTempdir(t)
	origLookPath := execLookPath
	execLookPath = func(name string) (string, error) { return "/usr/bin/migrate", nil }
	t.Cleanup(func() { execLookPath = origLookPath })
	withFakeExec(t, 1) // both attempts fail

	err := runMigrations()
	assert.Error(t, err)
}

// TestRunMigration_DownPassesSingleStep — regression: the migrate
// down command's docs promise single-step rollback, but the old
// implementation passed bare "down" which means "rollback ALL with a
// y/N prompt". With stdin not wired (the previous bug), the prompt
// auto-aborted and the command always failed. Now we always append
// "1" for the down direction so neither the prompt nor the all-
// rollback semantics surprise a developer.
func TestRunMigration_DownPassesSingleStep(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	var captured []string
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExecCommand(0)(name, args...)
	}
	t.Cleanup(func() { execCommand = orig })

	require.NoError(t, runMigration("down"))
	require.NotEmpty(t, captured, "execCommand should have been invoked")
	assert.Equal(t, "1", captured[len(captured)-1],
		"`migrate down` must pass '1' as the step count to avoid the all-rollback prompt")
}

// TestRunMigration_UpDoesNotPassCount — symmetric assertion: `up` does
// not append a step count, since "apply all pending" is the right
// semantics for the up direction.
func TestRunMigration_UpDoesNotPassCount(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	var captured []string
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExecCommand(0)(name, args...)
	}
	t.Cleanup(func() { execCommand = orig })

	require.NoError(t, runMigration("up"))
	assert.Equal(t, "up", captured[len(captured)-1],
		"`migrate up` must end with 'up' (no step count) so all pending migrations apply")
}

// stagedFakeExec returns a fake that exits with code[i] on the i-th call,
// repeating the final code if there are more calls than codes.
//
// Like withFakeExec, this also stubs the preflight probes to report OK
// — every dev pipeline test using stagedFakeExec needs the preflight
// to pass so the staged exit codes can drive the actual shell-out
// sequence under test. See the comment on withFakeExec for the
// migrate-version → TCP-probe refactor history.
func stagedFakeExec(t *testing.T, codes ...int) {
	t.Helper()
	orig := execCommand
	call := 0
	execCommand = func(name string, args ...string) *exec.Cmd {
		code := codes[len(codes)-1]
		if call < len(codes) {
			code = codes[call]
		}
		call++
		return fakeExecCommand(code)(name, args...)
	}
	t.Cleanup(func() { execCommand = orig })
	stubProbesOK(t)
}

// fakeExecOutput returns a function usable as execCommand that spawns
// the test binary's TestHelperProcess with a scripted stdout payload.
// Like fakeExecCommand but also sets GOFASTA_FAKE_STDOUT so the child
// prints it before exiting.
//
// so future "stdout plus non-zero exit" tests don't need to redefine
// the helper.
//
//nolint:unparam // exitCode is always 0 today; keep it parameterized
func fakeExecOutput(t *testing.T, stdout string, exitCode int) {
	t.Helper()
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_WANT_HELPER_PROCESS=1",
			fakeEnvExitCode+"="+strconv.Itoa(exitCode),
			"GOFASTA_FAKE_STDOUT="+stdout,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })
}

// strconvItoa is a tiny alias — scoped to this file's exec-stubbing
// helpers that build GOFASTA_FAKE_EXIT env values.
func strconvItoa(i int) string { return strconv.Itoa(i) }

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
