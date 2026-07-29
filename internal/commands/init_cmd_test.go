package commands

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "init" {
			found = true
			break
		}
	}
	assert.True(t, found, "initCmd should be registered on rootCmd")
}

func TestInitCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, initCmd.Short)
	assert.NotEmpty(t, initCmd.Long)
}

// TestRunInit_ConfigLoadFailedBranch — buildMigrationURL seam returns
// an empty string → the "Could not load config" warning path fires.
func TestRunInit_ConfigLoadFailedBranch(t *testing.T) {
	chdirTemp(t)
	orig := buildMigrationURL
	buildMigrationURL = func() string { return "" }
	t.Cleanup(func() { buildMigrationURL = orig })
	withFakeExec(t, 0)
	assert.NoError(t, runInit())
}

// TestRunInit_ConfigLoadFailed — no config.yaml so configutil returns
// an empty URL which triggers the else branch. runInit tolerates the
// missing config file.
func TestRunInit_ConfigLoadFailed(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	_ = runInit()
}

// TestRunInit_LoadsDotEnvBeforeMigrations — regression: Step 6 of
// runInit must load .env before building the migrate URL, otherwise
// the migration step sees empty credentials and dials the wrong port.
// The .env file already exists (Step 1's no-op branch).
func TestRunInit_LoadsDotEnvBeforeMigrations(t *testing.T) {
	chdirTemp(t)
	const probe = "GOFASTA_INIT_DOTENV_PROBE"
	require.NoError(t, os.WriteFile(".env", []byte(probe+"=loaded\n"), 0o644))
	t.Cleanup(func() { _ = os.Unsetenv(probe) })

	withFakeExec(t, 0)
	_ = runInit()
	assert.Equal(t, "loaded", os.Getenv(probe),
		"runInit must call loadDotEnv before building the migrate URL")
}

// TestFinishInit_JSON_SuccessEmitsResult — JSON mode + nil err writes
// an initResult document with success=true.
func TestFinishInit_JSON_SuccessEmitsResult(t *testing.T) {
	withJSONMode(t)
	steps := initSteps{}
	steps.add("env.create", "ok", nil)

	out := captureStdout(t, func() {
		err := finishInit(steps, nil)
		require.NoError(t, err)
	})

	var got initResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "init", got.Action)
	assert.True(t, got.Success)
	assert.Empty(t, got.Error)
	assert.Len(t, got.Steps, 1)
}

// TestFinishInit_JSON_FailurePropagates — JSON mode + non-nil err: the
// result records the error string AND finishInit returns the original
// err so the exit code stays non-zero.
func TestFinishInit_JSON_FailurePropagates(t *testing.T) {
	withJSONMode(t)
	steps := initSteps{}
	steps.add("build", "fail", errors.New("build broke"))

	out := captureStdout(t, func() {
		err := finishInit(steps, errors.New("build broke"))
		require.Error(t, err)
	})

	var got initResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.False(t, got.Success)
	assert.Equal(t, "build broke", got.Error)
}

// TestFinishInit_TextMode_NoStdoutWrite — without JSON mode, finishInit
// does NOT emit any JSON; it just returns the err pass-through.
func TestFinishInit_TextMode_NoStdoutWrite(t *testing.T) {
	steps := initSteps{}
	out := captureStdout(t, func() {
		err := finishInit(steps, errors.New("x"))
		require.Error(t, err)
	})
	assert.Empty(t, strings.TrimSpace(out))
}

// TestRunCmd_JSONReroutesStdout — in JSON mode runCmd directs the
// child's stdout to stderr so the parent's structured result stays
// the only thing on stdout.
func TestRunCmd_JSONReroutesStdout(t *testing.T) {
	withJSONMode(t)
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("true")
	}
	assert.NoError(t, runCmd("ignored"))
}

// TestRunCmd_TextModeUsesStdout — in text mode the child's stdout
// streams to the parent's stdout.
func TestRunCmd_TextModeUsesStdout(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("true")
	}
	assert.NoError(t, runCmd("ignored"))
}

// TestRunInit_GoModTidyFailureStops — first failing step is go mod
// tidy. runInit returns the wrapped error without proceeding to
// generate Wire / GraphQL etc.
func TestRunInit_GoModTidyFailureStops(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 1)
	err := runInit()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go mod tidy failed")
}

// TestRunInit_BuildFailureStops — every prior step succeeds (go mod
// tidy, wire, swag, migrate) but the final go build step fails.
// Sequence: mod-tidy(0), wire(0), swag(0), migrate(0), build(1).
// gqlgen and graphql skip because no gqlgen.yml in the temp dir.
func TestRunInit_BuildFailureStops(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	stagedFakeExec(t, 0, 0, 0, 0, 1)
	err := runInit()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build verification failed")
}

// TestRunInit_GqlgenYmlPresent — the gqlgen.yml-exists branch of Step
// 4. Sequence: mod-tidy(0), wire(0), gqlgen(0), swag(0), migrate(0),
// build(0). All six steps succeed.
func TestRunInit_GqlgenYmlPresent(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	require.NoError(t, os.WriteFile("gqlgen.yml", []byte("# stub\n"), 0o644))
	stagedFakeExec(t, 0, 0, 0, 0, 0, 0)
	require.NoError(t, runInit())
}

// TestRunInit_GqlgenFails — gqlgen.yml exists but gqlgen invocation
// fails — the "gqlgen generation failed" warn branch fires while
// runInit continues to swag/migrate/build.
func TestRunInit_GqlgenFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	require.NoError(t, os.WriteFile("gqlgen.yml", []byte("# stub\n"), 0o644))
	// mod-tidy(0), wire(0), gqlgen(1=fail), swag(0), migrate(0), build(0).
	stagedFakeExec(t, 0, 0, 1, 0, 0, 0)
	require.NoError(t, runInit())
}

// TestRunInit_JSONMode — runInit in JSON mode reroutes the migrate
// child's stdout to stderr (line 131) so the structured init result
// emitted at the end stays the only thing on stdout.
func TestRunInit_JSONMode(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	withJSONMode(t)

	out := captureStdout(t, func() {
		require.NoError(t, runInit())
	})

	var got initResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "init", got.Action)
	assert.True(t, got.Success)
}

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
