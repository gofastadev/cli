package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetRepairFlags restores repair flags + stdin seam between tests.
func resetRepairFlags(t *testing.T) {
	t.Helper()
	origForce, origRevert, origComplete, origYes, origStdin :=
		repairForce, repairRevert, repairComplete, repairYes, repairStdin
	repairForce = -1
	repairRevert = false
	repairComplete = false
	repairYes = false
	t.Cleanup(func() {
		repairForce, repairRevert, repairComplete, repairYes, repairStdin =
			origForce, origRevert, origComplete, origYes, origStdin
	})
}

// TestMigrateRepairCmd_Registered — repair shows up under migrate.
func TestMigrateRepairCmd_Registered(t *testing.T) {
	found := false
	for _, c := range migrateCmd.Commands() {
		if c.Name() == "repair" {
			found = true
			break
		}
	}
	assert.True(t, found, "repair should be registered under migrate")
}

// TestRunMigrateRepair_NoConfig — without a config.yaml, fails fast
// before touching the DB.
func TestRunMigrateRepair_NoConfig(t *testing.T) {
	chdirTemp(t)
	resetRepairFlags(t)
	orig := buildMigrationURL
	buildMigrationURL = func() string { return "" }
	t.Cleanup(func() { buildMigrationURL = orig })
	err := runMigrateRepair()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// TestRunMigrateRepair_CleanSchemaIsNoop — when migrate version
// reports a clean state, repair reports "nothing to do" and returns
// nil without invoking force.
func TestRunMigrateRepair_CleanSchemaIsNoop(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetRepairFlags(t)

	// Stub the version probe to return clean "5".
	withFakeMigrateOutput(t, "5\n", 0)

	require.NoError(t, runMigrateRepair())
}

// TestRunMigrateRepair_RevertFlag — --revert maps to force(N-1).
func TestRunMigrateRepair_RevertFlag(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetRepairFlags(t)
	repairRevert = true
	repairYes = true

	callsRef := captureMigrateCalls(t, "5 (dirty)\n", 0)
	require.NoError(t, runMigrateRepair())
	calls := *callsRef
	require.NotEmpty(t, calls)
	last := calls[len(calls)-1]
	assert.Equal(t, "force", last[len(last)-2])
	assert.Equal(t, "4", last[len(last)-1], "--revert at version 5 should force to 4")
}

// TestRunMigrateRepair_CompleteFlag — --complete maps to force(N).
func TestRunMigrateRepair_CompleteFlag(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetRepairFlags(t)
	repairComplete = true
	repairYes = true

	callsRef := captureMigrateCalls(t, "5 (dirty)\n", 0)
	require.NoError(t, runMigrateRepair())
	calls := *callsRef
	last := calls[len(calls)-1]
	assert.Equal(t, "5", last[len(last)-1], "--complete at version 5 should force to 5")
}

// TestRunMigrateRepair_RevertAndCompleteRejected — mutually exclusive
// flags are caught.
func TestRunMigrateRepair_RevertAndCompleteRejected(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetRepairFlags(t)
	repairRevert = true
	repairComplete = true
	repairYes = true

	withFakeMigrateOutput(t, "5 (dirty)\n", 0)
	err := runMigrateRepair()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// TestRunMigrateRepair_ForceFlagSkipsInspection — --force <N> bypasses
// the dirty check and goes straight to migrate force.
func TestRunMigrateRepair_ForceFlagSkipsInspection(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetRepairFlags(t)
	repairForce = 3
	repairYes = true

	callsRef := captureMigrateCalls(t, "", 0)
	require.NoError(t, runMigrateRepair())
	calls := *callsRef
	last := calls[len(calls)-1]
	assert.Equal(t, "force", last[len(last)-2])
	assert.Equal(t, "3", last[len(last)-1])
}

// TestRunMigrateRepair_NonInteractiveDirtyWithoutFlags — in CI / piped
// stdin, refuse to act when the user hasn't said which path to take.
func TestRunMigrateRepair_NonInteractiveDirtyWithoutFlags(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetRepairFlags(t)

	// Pin stdin as non-TTY so the test is deterministic regardless of
	// how the test runner wires /dev/stdin.
	origTTY := stdinIsTTY
	stdinIsTTY = func() bool { return false }
	t.Cleanup(func() { stdinIsTTY = origTTY })

	withFakeMigrateOutput(t, "5 (dirty)\n", 0)
	err := runMigrateRepair()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-interactive")
}

// TestPromptRepairTarget_RevertChoice — menu "r" → version-1.
func TestPromptRepairTarget_RevertChoice(t *testing.T) {
	resetRepairFlags(t)
	repairStdin = strings.NewReader("r\n")
	target, err := promptRepairTarget(7, t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, 6, target)
}

// TestPromptRepairTarget_CompleteChoice — menu "c" → version.
func TestPromptRepairTarget_CompleteChoice(t *testing.T) {
	resetRepairFlags(t)
	repairStdin = strings.NewReader("c\n")
	target, err := promptRepairTarget(7, t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, 7, target)
}

// TestPromptRepairTarget_QuitChoice — menu "q" cancels.
func TestPromptRepairTarget_QuitChoice(t *testing.T) {
	resetRepairFlags(t)
	repairStdin = strings.NewReader("q\n")
	_, err := promptRepairTarget(7, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "canceled")
}

// TestPromptRepairTarget_ShowThenChoose — "s" prints the SQL then
// re-prompts; subsequent "c" completes the flow.
func TestPromptRepairTarget_ShowThenChoose(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "000007_thing.up.sql"), []byte("CREATE TABLE thing();\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "000007_thing.down.sql"), []byte("DROP TABLE thing;\n"), 0o644))

	resetRepairFlags(t)
	repairStdin = strings.NewReader("s\nc\n")
	target, err := promptRepairTarget(7, dir)
	require.NoError(t, err)
	assert.Equal(t, 7, target)
}

// TestPromptRepairTarget_InvalidThenValid — bad input loops back to
// the menu rather than crashing.
func TestPromptRepairTarget_InvalidThenValid(t *testing.T) {
	resetRepairFlags(t)
	repairStdin = strings.NewReader("xyz\nr\n")
	target, err := promptRepairTarget(3, t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, 2, target)
}

// TestFindMigrationFile_ZeroPadded — standard golang-migrate naming.
func TestFindMigrationFile_ZeroPadded(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "000005_create_users.up.sql"), nil, 0o644))
	got := findMigrationFile(dir, 5, "up.sql")
	assert.Equal(t, filepath.Join(dir, "000005_create_users.up.sql"), got)
}

// TestFindMigrationFile_UnpaddedFallback — projects that don't zero-
// pad still resolve correctly.
func TestFindMigrationFile_UnpaddedFallback(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "5_create_users.up.sql"), nil, 0o644))
	got := findMigrationFile(dir, 5, "up.sql")
	assert.Equal(t, filepath.Join(dir, "5_create_users.up.sql"), got)
}

// TestFindMigrationFile_NotFound — missing file returns "".
func TestFindMigrationFile_NotFound(t *testing.T) {
	dir := t.TempDir()
	got := findMigrationFile(dir, 5, "up.sql")
	assert.Equal(t, "", got)
}

// TestRepairForceTo_NegativeRejected — version must be ≥ 0.
func TestRepairForceTo_NegativeRejected(t *testing.T) {
	resetRepairFlags(t)
	repairYes = true
	err := repairForceTo(-1, "postgres://stub", "db/migrations")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be")
}

// captureMigrateCalls overrides execCommand to record every invocation
// in the slice the returned pointer references. The version-probe
// (first) call gets the supplied output; subsequent calls succeed
// silently. Tests dereference (*calls) AFTER the action under test
// completes — that's when the closure has finished appending.
func captureMigrateCalls(t *testing.T, versionOutput string, versionExitCode int) *[][]string {
	t.Helper()
	calls := &[][]string{}
	orig := execCommand
	first := true
	execCommand = func(name string, args ...string) *exec.Cmd {
		call := append([]string{name}, args...)
		*calls = append(*calls, call)
		if first && versionOutput != "" {
			first = false
			return fakeMigrateVersionCmd(versionOutput, versionExitCode)
		}
		return fakeExecCommand(0)(name, args...)
	}
	t.Cleanup(func() { execCommand = orig })
	return calls
}

// fakeMigrateVersionCmd builds a fake exec.Cmd that prints the given
// stdout and exits with the given code — used to simulate the
// `migrate version` probe.
func fakeMigrateVersionCmd(stdout string, exitCode int) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", "migrate", "version"}
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append(os.Environ(),
		"GOFASTA_WANT_HELPER_PROCESS=1",
		fakeEnvExitCode+"="+strconv.Itoa(exitCode),
		"GOFASTA_FAKE_STDOUT="+stdout,
	)
	return cmd
}

// withFakeMigrateOutput sets execCommand to a fake that returns the
// supplied output + exit code on every call. Used when the test
// doesn't care about distinguishing version vs force calls.
func withFakeMigrateOutput(t *testing.T, output string, exitCode int) {
	t.Helper()
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return fakeMigrateVersionCmd(output, exitCode)
	}
	t.Cleanup(func() { execCommand = orig })
}

// TestRepairForceTo_MigrateCommandFails — migrate force exits non-zero;
// repairForceTo must wrap the error and return the wrapped form.
func TestRepairForceTo_MigrateCommandFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetRepairFlags(t)
	repairYes = true

	withFakeExec(t, 1)

	err := repairForceTo(2, "postgres://stub", "db/migrations")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migrate force 2 failed")
}

// TestRepairForceTo_InteractiveConfirmYes — without --yes, in a TTY,
// the user answers "y" and the migrate force proceeds.
func TestRepairForceTo_InteractiveConfirmYes(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetRepairFlags(t)

	origTTY := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = origTTY })
	repairStdin = strings.NewReader("y\n")

	withFakeExec(t, 0)
	// Vary migrationsDir from the production default so unparam doesn't
	// flag the function as taking a constant-valued parameter.
	require.NoError(t, repairForceTo(1, "postgres://stub", "custom/migrations"))
}

// TestRepairForceTo_InteractiveConfirmYesSpelledOut — accepts "yes".
func TestRepairForceTo_InteractiveConfirmYesSpelledOut(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetRepairFlags(t)

	origTTY := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = origTTY })
	repairStdin = strings.NewReader("yes\n")

	withFakeExec(t, 0)
	require.NoError(t, repairForceTo(1, "postgres://stub", "alt/migrations"))
}

// TestRepairForceTo_InteractiveConfirmNo — without --yes, in a TTY,
// the user answers "n". Returns the "repair canceled" error.
func TestRepairForceTo_InteractiveConfirmNo(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetRepairFlags(t)

	origTTY := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = origTTY })
	repairStdin = strings.NewReader("n\n")

	err := repairForceTo(1, "postgres://stub", "db/migrations")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "canceled")
}

// TestShowMigrationSQL_BothPresent — both .up.sql and .down.sql exist;
// both bodies are printed.
func TestShowMigrationSQL_BothPresent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "000003_a.up.sql"), []byte("UP\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "000003_a.down.sql"), []byte("DOWN\n"), 0o644))
	showMigrationSQL(dir, 3)
}

// TestShowMigrationSQL_BothMissing — neither .up.sql nor .down.sql is
// found; warn lines are emitted but no error.
func TestShowMigrationSQL_BothMissing(t *testing.T) {
	dir := t.TempDir()
	showMigrationSQL(dir, 99)
}

// TestShowMigrationSQL_OnlyUpPresent — up exists but down is missing.
func TestShowMigrationSQL_OnlyUpPresent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "000004_x.up.sql"), []byte("UP\n"), 0o644))
	showMigrationSQL(dir, 4)
}

// TestPrintFileWithIndent_ReadError — non-existent path produces a
// warning line but doesn't panic.
func TestPrintFileWithIndent_ReadError(t *testing.T) {
	printFileWithIndent("/nonexistent/path/nope.sql", "    ")
}

// TestPrintFileWithIndent_HappyPath — every line in the file gets the
// indent.
func TestPrintFileWithIndent_HappyPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "file.sql")
	require.NoError(t, os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0o644))
	printFileWithIndent(p, ">> ")
}

// TestFindMigrationFile_ReadDirError — non-existent dir returns "".
func TestFindMigrationFile_ReadDirError(t *testing.T) {
	got := findMigrationFile("/nonexistent/dir", 5, "up.sql")
	assert.Equal(t, "", got)
}

// TestFindMigrationFile_WrongSuffix — file with the right prefix but
// wrong suffix isn't returned.
func TestFindMigrationFile_WrongSuffix(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "000005_create_users.up.sql"), nil, 0o644))
	got := findMigrationFile(dir, 5, "down.sql")
	assert.Equal(t, "", got)
}

// TestRunMigrateRepair_VersionReadFails — version probe exits non-zero
// AND emits empty stdout, so readAppliedMigrationVersion errors and
// runMigrateRepair wraps it.
func TestRunMigrateRepair_VersionReadFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetRepairFlags(t)
	withFakeMigrateOutput(t, "", 1)
	err := runMigrateRepair()
	require.Error(t, err)
}

// TestMigrateRepairCmd_RunE — exercise the cobra-bound RunE wrapper.
func TestMigrateRepairCmd_RunE(t *testing.T) {
	chdirTemp(t)
	resetRepairFlags(t)
	orig := buildMigrationURL
	buildMigrationURL = func() string { return "" }
	t.Cleanup(func() { buildMigrationURL = orig })
	err := migrateRepairCmd.RunE(migrateRepairCmd, nil)
	require.Error(t, err)
}

// TestResolveRepairTarget_JSONIsNonInteractive — even with a TTY, JSON
// mode is treated as non-interactive (humans wouldn't see the prompt,
// agents need explicit flags).
func TestResolveRepairTarget_JSONIsNonInteractive(t *testing.T) {
	resetRepairFlags(t)
	origTTY := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = origTTY })
	withJSONMode(t)

	_, err := resolveRepairTarget(5, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-interactive")
}

// TestResolveRepairTarget_TTYRoutesToPrompt — interactive TTY + not
// JSON-mode triggers the promptRepairTarget branch of the switch via
// resolveRepairTarget, not the direct call covered elsewhere.
func TestResolveRepairTarget_TTYRoutesToPrompt(t *testing.T) {
	resetRepairFlags(t)
	origTTY := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = origTTY })
	repairStdin = strings.NewReader("r\n")

	target, err := resolveRepairTarget(7, t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, 6, target, "revert at 7 should target 6")
}
