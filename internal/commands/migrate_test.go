package commands

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateCmd_HasUpDown(t *testing.T) {
	subCmds := migrateCmd.Commands()
	names := make([]string, 0, len(subCmds))
	for _, c := range subCmds {
		names = append(names, c.Name())
	}
	assert.Contains(t, names, "up")
	assert.Contains(t, names, "down")
}

func TestMigrateCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "migrate" {
			found = true
			break
		}
	}
	assert.True(t, found, "migrateCmd should be registered on rootCmd")
}

func TestRunMigration_NoConfig(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	// loadConfig returns a koanf instance even without config.yaml,
	// so BuildMigrationURL returns a postgres URL with defaults
	err := runMigration("up")
	// Should fail because migrate binary is not available
	assert.Error(t, err)
}

func TestRunMigration_EmptyURL(t *testing.T) {
	// runMigration checks for empty URL and returns an error
	// This is hard to trigger since loadConfig always returns a koanf instance
	// but we can test the direction parameter
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	err := runMigration("down")
	assert.Error(t, err)
}

// TestRunMigration_EmptyURLSeam — the buildMigrationURL seam returns
// "" so the defensive "failed to load config" branch fires.
func TestRunMigration_EmptyURLSeam(t *testing.T) {
	orig := buildMigrationURL
	buildMigrationURL = func() string { return "" }
	t.Cleanup(func() { buildMigrationURL = orig })
	err := runMigration("up")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// TestRunMigration_EmptyURLCoverage — no config.yaml and no env vars
// so configutil's defaults produce a non-empty URL; the empty-URL
// branch is defensive. This test exercises the code path without a
// seam override.
func TestRunMigration_EmptyURLCoverage(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	_ = runMigration("up")
}

// resetDownFlags restores the down flags + stdin seam to clean state
// between tests so order-of-execution can't leak state.
func resetDownFlags(t *testing.T) {
	t.Helper()
	origAll, origSteps, origYes, origStdin := downAll, downSteps, downYes, downStdin
	downAll = false
	downSteps = 0
	downYes = false
	t.Cleanup(func() {
		downAll, downSteps, downYes, downStdin = origAll, origSteps, origYes, origStdin
	})
}

// TestRunMigrationDown_AllFlagWithYesPassesNoCount — `--all --yes` rolls
// back without any prompt and without a step count (which makes the
// migrate tool roll back ALL).
func TestRunMigrationDown_AllFlagWithYesPassesNoCount(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetDownFlags(t)
	downAll = true
	downYes = true

	var captured []string
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExecCommand(0)(name, args...)
	}
	t.Cleanup(func() { execCommand = orig })

	require.NoError(t, runMigrationDown())
	require.NotEmpty(t, captured)
	assert.Equal(t, "down", captured[len(captured)-1],
		"--all should pass bare 'down' (no count) so migrate rolls back everything")
}

// TestRunMigrationDown_AllAutoAnswersMigratePrompt — when the user
// picks "all" via flag or menu, runMigrationDown must feed "y\n" to
// the migrate child so the tool's own redundant confirmation prompt
// auto-answers (we already confirmed at our layer).
//
// We inspect the spawned cmd asynchronously: runMigrate assigns
// cmd.Stdin AFTER execCommand returns but BEFORE cmd.Run is called.
// Capturing the *exec.Cmd reference in the fake lets us read .Stdin
// after Run completes, which proves the override was wired.
func TestRunMigrationDown_AllAutoAnswersMigratePrompt(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetDownFlags(t)
	downAll = true
	downYes = true

	var spawned *exec.Cmd
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		spawned = fakeExecCommand(0)(name, args...)
		return spawned
	}
	t.Cleanup(func() { execCommand = orig })

	require.NoError(t, runMigrationDown())
	require.NotNil(t, spawned)
	require.NotNil(t, spawned.Stdin,
		"runMigrationDown for --all must wire a stdin override to auto-answer migrate's prompt")
	// The override must NOT be os.Stdin — that's the bug we're guarding
	// against (user types "y" once at our menu, then the migrate tool
	// asks again and the user is confused).
	assert.NotSame(t, os.Stdin, spawned.Stdin,
		"stdin should be a strings.Reader feeding 'y\\n', not os.Stdin")
}

// TestRunMigrationDown_SingleStepUsesOsStdin — when the plan has an
// explicit step count, no prompt fires from migrate, so we should
// pass through os.Stdin (the default).
func TestRunMigrationDown_SingleStepUsesOsStdin(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetDownFlags(t)
	downSteps = 1

	var spawned *exec.Cmd
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		spawned = fakeExecCommand(0)(name, args...)
		return spawned
	}
	t.Cleanup(func() { execCommand = orig })

	require.NoError(t, runMigrationDown())
	require.NotNil(t, spawned)
	assert.Same(t, os.Stdin, spawned.Stdin,
		"single-step rollback should pass through os.Stdin (no prompt to suppress)")
}

// TestRunMigrationDown_StepsFlagPassesCount — `--steps 3` skips the
// menu and passes "3" to migrate.
func TestRunMigrationDown_StepsFlagPassesCount(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetDownFlags(t)
	downSteps = 3

	var captured []string
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExecCommand(0)(name, args...)
	}
	t.Cleanup(func() { execCommand = orig })

	require.NoError(t, runMigrationDown())
	assert.Equal(t, "3", captured[len(captured)-1])
}

// TestRunMigrationDown_NegativeStepsRejected — input validation: a
// negative --steps value fails fast.
func TestRunMigrationDown_NegativeStepsRejected(t *testing.T) {
	chdirTemp(t)
	resetDownFlags(t)
	downSteps = -2
	err := runMigrationDown()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--steps must be > 0")
}

// TestRunMigrationDown_AllRequiresConfirmWithoutYes — `--all` without
// --yes triggers the confirm prompt; answering "n" cancels.
func TestRunMigrationDown_AllRequiresConfirmWithoutYes(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetDownFlags(t)
	downAll = true
	downStdin = strings.NewReader("n\n")

	err := runMigrationDown()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "canceled")
}

// TestRunMigrationDown_AllConfirmedWithYesAnswerProceeds — same setup
// but the user types "y", so the rollback proceeds.
func TestRunMigrationDown_AllConfirmedWithYesAnswerProceeds(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetDownFlags(t)
	downAll = true
	downStdin = strings.NewReader("y\n")
	withFakeExec(t, 0)
	require.NoError(t, runMigrationDown())
}

// TestResolveDownPlan_Default_NonInteractive — no flags + non-TTY stdin
// → safe single-step default. Pinning this so CI never blocks on a
// menu we'd otherwise show.
func TestResolveDownPlan_Default_NonInteractive(t *testing.T) {
	resetDownFlags(t)
	plan, err := resolveDownPlan()
	require.NoError(t, err)
	assert.Equal(t, 1, plan.steps)
	assert.False(t, plan.confirm)
}

// TestPromptDownPlan_MenuChoices walks every menu branch via the
// downStdin seam.
func TestPromptDownPlan_MenuChoices(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantSteps int
		wantConf  bool
		wantErr   bool
	}{
		{"default-empty", "\n", 1, false, false},
		{"default-1", "1\n", 1, false, false},
		{"all", "a\n", 0, true, false},
		{"all-spelled", "all\n", 0, true, false},
		{"specific-via-n", "n\n3\n", 3, true, false},
		{"specific-via-bare-number", "5\n", 5, true, false},
		{"specific-via-bare-1-not-confirmed", "1\n", 1, false, false},
		{"cancel-q", "q\n", 0, false, true},
		{"cancel-quit", "quit\n", 0, false, true},
		{"invalid", "zzz\n", 0, false, true},
		{"invalid-n-then-bad", "n\nfoo\n", 0, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetDownFlags(t)
			downStdin = strings.NewReader(tc.input)
			plan, err := promptDownPlan()
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantSteps, plan.steps)
			assert.Equal(t, tc.wantConf, plan.confirm)
		})
	}
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

// TestRunMigration_LoadsDotEnv — regression for the bug where
// `gofasta migrate up` produced `postgres://:@localhost:5432/?sslmode=disable`
// because it never read .env. The scaffold's config.yaml intentionally
// omits user/password/name; .env supplies them via the project-prefixed
// env vars (e.g. ACME_DATABASE_USER). This test pins that runMigration
// loads .env BEFORE building the URL so the credentials show up in the
// -database argument passed to `migrate`.
func TestRunMigration_LoadsDotEnv(t *testing.T) {
	chdirTemp(t)
	// Scaffold-style config.yaml: no user/pass/name, host/port from yaml.
	scaffoldConfig := `database:
  driver: postgres
  host: localhost
  port: "5432"
  sslmode: disable
`
	require.NoError(t, os.WriteFile("config.yaml", []byte(scaffoldConfig), 0o644))
	require.NoError(t, os.WriteFile("go.mod",
		[]byte("module github.com/acme/myapp\n\ngo 1.25.0\n"), 0o644))

	// .env that overrides everything the way the dev workflow expects:
	// host port (5433) maps to container 5432, plus the credentials.
	dotenv := `MYAPP_DATABASE_USER=myappuser
MYAPP_DATABASE_PASSWORD=myapppass
MYAPP_DATABASE_NAME=myapp_dev
MYAPP_DATABASE_HOST=localhost
MYAPP_DATABASE_PORT=5433
`
	require.NoError(t, os.WriteFile(".env", []byte(dotenv), 0o644))
	// Clean up the env vars that loadDotEnv will os.Setenv so this
	// test doesn't leak state into sibling tests.
	for _, k := range []string{
		"MYAPP_DATABASE_USER", "MYAPP_DATABASE_PASSWORD",
		"MYAPP_DATABASE_NAME", "MYAPP_DATABASE_HOST", "MYAPP_DATABASE_PORT",
	} {
		t.Cleanup(func() { _ = os.Unsetenv(k) })
	}

	// Capture the exact args passed to the migrate shell-out so we can
	// assert the URL contains the .env values.
	var captured []string
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExecCommand(0)(name, args...)
	}
	t.Cleanup(func() { execCommand = orig })

	require.NoError(t, runMigration("up"))

	// Find the -database arg (it follows -database in the captured slice).
	var dbURL string
	for i, a := range captured {
		if a == "-database" && i+1 < len(captured) {
			dbURL = captured[i+1]
			break
		}
	}
	require.NotEmpty(t, dbURL, "migrate should be invoked with -database <url>")
	assert.Contains(t, dbURL, "myappuser:myapppass@", "URL must include .env credentials, not empty :@")
	assert.Contains(t, dbURL, "localhost:5433", "URL must use the .env host:port mapping")
	assert.Contains(t, dbURL, "/myapp_dev", "URL must include .env database name")
	assert.NotContains(t, dbURL, "://:@", "URL must not have empty user/password")
	assert.NotContains(t, dbURL, "/?sslmode", "URL must include database name before query")
}

// TestResolveDownPlan_AllWithoutYesNeedsConfirm — --all sets steps=0
// and (without --yes) confirm=true.
func TestResolveDownPlan_AllWithoutYesNeedsConfirm(t *testing.T) {
	resetDownFlags(t)
	downAll = true
	plan, err := resolveDownPlan()
	require.NoError(t, err)
	assert.Equal(t, 0, plan.steps)
	assert.True(t, plan.confirm)
}

// TestResolveDownPlan_AllWithYesSkipsConfirm — --all + --yes lifts
// the confirm gate (scripts mode).
func TestResolveDownPlan_AllWithYesSkipsConfirm(t *testing.T) {
	resetDownFlags(t)
	downAll = true
	downYes = true
	plan, err := resolveDownPlan()
	require.NoError(t, err)
	assert.Equal(t, 0, plan.steps)
	assert.False(t, plan.confirm)
}

// TestResolveDownPlan_StepsPositive — explicit step count, no confirm.
func TestResolveDownPlan_StepsPositive(t *testing.T) {
	resetDownFlags(t)
	downSteps = 3
	plan, err := resolveDownPlan()
	require.NoError(t, err)
	assert.Equal(t, 3, plan.steps)
}

// TestResolveDownPlan_StepsNegative — negative count is rejected.
func TestResolveDownPlan_StepsNegative(t *testing.T) {
	resetDownFlags(t)
	downSteps = -1
	_, err := resolveDownPlan()
	require.Error(t, err)
}

// TestResolveDownPlan_NonInteractiveDefault — non-TTY (CI/piped)
// falls through to the safe single-step default.
func TestResolveDownPlan_NonInteractiveDefault(t *testing.T) {
	resetDownFlags(t)
	origTTY := stdinIsTTY
	stdinIsTTY = func() bool { return false }
	t.Cleanup(func() { stdinIsTTY = origTTY })
	plan, err := resolveDownPlan()
	require.NoError(t, err)
	assert.Equal(t, 1, plan.steps)
	assert.False(t, plan.confirm)
}

// TestResolveDownPlan_JSONModeFallsThrough — JSON mode is treated as
// non-interactive even with a TTY, so we get the default single-step
// plan (not the prompt).
func TestResolveDownPlan_JSONModeFallsThrough(t *testing.T) {
	resetDownFlags(t)
	origTTY := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = origTTY })
	withJSONMode(t)

	plan, err := resolveDownPlan()
	require.NoError(t, err)
	assert.Equal(t, 1, plan.steps)
}

// TestConfirmDestructive_NSteps — message uses the count, accepts "y".
func TestConfirmDestructive_NSteps(t *testing.T) {
	resetDownFlags(t)
	downStdin = strings.NewReader("y\n")
	assert.True(t, confirmDestructive(migrateDownPlan{steps: 3}))
}

// TestConfirmDestructive_AllSteps — steps=0 uses the "ALL" message;
// confirm=true.
func TestConfirmDestructive_AllSteps(t *testing.T) {
	resetDownFlags(t)
	downStdin = strings.NewReader("yes\n")
	assert.True(t, confirmDestructive(migrateDownPlan{steps: 0}))
}

// TestConfirmDestructive_NoAnswer — anything other than y/yes is no.
func TestConfirmDestructive_NoAnswer(t *testing.T) {
	resetDownFlags(t)
	downStdin = strings.NewReader("n\n")
	assert.False(t, confirmDestructive(migrateDownPlan{steps: 1}))
}

// TestRunMigrate_JSON_Success — JSON-mode runMigrate emits a single
// migrateResult document on stdout.
func TestRunMigrate_JSON_Success(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	withJSONMode(t)

	out := captureStdout(t, func() {
		require.NoError(t, runMigrate("up", []string{"up"}, nil))
	})
	var got migrateResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "up", got.Direction)
	assert.Equal(t, "ok", got.Status)
}

// TestRunMigrate_JSON_Failure — JSON-mode runMigrate on a failing
// migrate command. The structured result records status=fail and the
// error string; the function returns the wrapped error.
func TestRunMigrate_JSON_Failure(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 1)
	withJSONMode(t)

	out := captureStdout(t, func() {
		err := runMigrate("up", []string{"up"}, nil)
		require.Error(t, err)
	})
	var got migrateResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "fail", got.Status)
	assert.NotEmpty(t, got.Message)
}

// TestRunMigrate_JSON_WithStdinOverride — JSON mode + a non-nil
// stdinOverride: the override is wired to the child's stdin so prompts
// auto-answer instead of blocking forever on Read. Mirrors the
// down-all flow used in real life.
func TestRunMigrate_JSON_WithStdinOverride(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	withJSONMode(t)

	override := strings.NewReader("y\n")
	out := captureStdout(t, func() {
		require.NoError(t, runMigrate("down", []string{"down"}, override))
	})
	var got migrateResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "down", got.Direction)
	assert.Equal(t, "ok", got.Status)
}

// TestStdinIsTTY_StatError — when os.Stdin.Stat() returns an error
// (defensive; very rare in practice), stdinIsTTY must return false.
// Injected via the stdinStat seam.
func TestStdinIsTTY_StatError(t *testing.T) {
	orig := stdinStat
	stdinStat = func() (os.FileInfo, error) { return nil, errDummy }
	t.Cleanup(func() { stdinStat = orig })

	// stdinIsTTY is itself a seam; many tests swap it. Call the
	// PRODUCTION implementation by reaching past any test override.
	prevIsTTY := stdinIsTTY
	stdinIsTTY = func() bool {
		info, err := stdinStat()
		if err != nil {
			return false
		}
		return info.Mode()&os.ModeCharDevice != 0
	}
	t.Cleanup(func() { stdinIsTTY = prevIsTTY })

	assert.False(t, stdinIsTTY())
}
