package commands

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateEnvFile_WriteFailureIsReportedAsFail covers the write-error branch.
//
// .env holds database credentials and API keys. A write that fails silently
// while the step reports "ok" would leave the developer believing their
// secrets file exists — so the failure has to surface as a "fail" step
// carrying the cause.
//
// Getting here needs .env to look ABSENT to the initial os.Stat (otherwise the
// function returns early with "already exists") while still being unwritable.
// A symlink pointing into a directory that does not exist does both: Stat
// follows it and reports not-found, and the write follows it and fails with
// ENOENT. Unlike a permissions trick, this behaves the same under root.
func TestCreateEnvFile_WriteFailureIsReportedAsFail(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.Remove(".env.example"))
	_ = os.Remove(".env")

	require.NoError(t, os.Symlink("no-such-dir/target", ".env"))

	_, err := os.Stat(".env")
	require.Error(t, err, "the dangling symlink must read as absent")

	var steps initSteps
	createEnvFile(&steps)

	require.Len(t, steps, 1)
	assert.Equal(t, "env.create", steps[0].Name)
	assert.Equal(t, "fail", steps[0].Status)
	assert.NotEmpty(t, steps[0].Error, "the failure must carry its cause")
}

// TestCreateEnvFile_CopiesFromExample and _CreatesEmpty pin the two success
// paths alongside it, so the branch above is not the only documented outcome.
func TestCreateEnvFile_CopiesFromExample(t *testing.T) {
	inRenderedProject(t)
	_ = os.Remove(".env")
	require.NoError(t, os.WriteFile(".env.example", []byte("KEY=value\n"), 0o644))

	var steps initSteps
	createEnvFile(&steps)

	require.Len(t, steps, 1)
	assert.Equal(t, "ok", steps[0].Status)
	body, err := os.ReadFile(".env")
	require.NoError(t, err)
	assert.Equal(t, "KEY=value\n", string(body))
}

func TestCreateEnvFile_CreatesEmptyWithoutExample(t *testing.T) {
	inRenderedProject(t)
	_ = os.Remove(".env")
	_ = os.Remove(".env.example")

	var steps initSteps
	createEnvFile(&steps)

	require.Len(t, steps, 1)
	assert.Equal(t, "ok", steps[0].Status)
	body, err := os.ReadFile(".env")
	require.NoError(t, err)
	assert.Contains(t, string(body), "Environment config")
}

func TestCreateEnvFile_SkipsWhenPresent(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, os.WriteFile(".env", []byte("EXISTING=1\n"), 0o600))

	var steps initSteps
	createEnvFile(&steps)

	require.Len(t, steps, 1)
	assert.Equal(t, "skip", steps[0].Status)
	body, err := os.ReadFile(".env")
	require.NoError(t, err)
	assert.Equal(t, "EXISTING=1\n", string(body), "an existing .env must never be overwritten")
}
