package commands

import (
	"fmt"
	"os/exec"
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
