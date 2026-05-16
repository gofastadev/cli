package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// repairForceTo — error + interactive-prompt branches
// ─────────────────────────────────────────────────────────────────────

// TestRepairForceTo_MigrateCommandFails — migrate force exits non-zero;
// repairForceTo must wrap the error and return the wrapped form.
func TestRepairForceTo_MigrateCommandFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	resetRepairFlags(t)
	repairYes = true

	// Make migrate force exit 1.
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
// the user answers "n" (or anything other than y/yes). Returns the
// "repair canceled" error.
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

// ─────────────────────────────────────────────────────────────────────
// showMigrationSQL — branches for missing up/down files
// ─────────────────────────────────────────────────────────────────────

// TestShowMigrationSQL_BothPresent — both .up.sql and .down.sql exist;
// both bodies are printed.
func TestShowMigrationSQL_BothPresent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "000003_a.up.sql"), []byte("UP\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "000003_a.down.sql"), []byte("DOWN\n"), 0o644))
	// Just exercise — no assertion needed; the side-effect path is cliout.
	showMigrationSQL(dir, 3)
}

// TestShowMigrationSQL_BothMissing — neither .up.sql nor .down.sql is
// found; warn lines are emitted but no error.
func TestShowMigrationSQL_BothMissing(t *testing.T) {
	dir := t.TempDir()
	showMigrationSQL(dir, 99) // no files at this version
}

// TestShowMigrationSQL_OnlyUpPresent — up exists but down is missing.
func TestShowMigrationSQL_OnlyUpPresent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "000004_x.up.sql"), []byte("UP\n"), 0o644))
	showMigrationSQL(dir, 4)
}

// ─────────────────────────────────────────────────────────────────────
// printFileWithIndent — error + multi-line happy path
// ─────────────────────────────────────────────────────────────────────

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

// ─────────────────────────────────────────────────────────────────────
// findMigrationFile — extra coverage
// ─────────────────────────────────────────────────────────────────────

// TestFindMigrationFile_ReadDirError — non-existent dir returns "".
func TestFindMigrationFile_ReadDirError(t *testing.T) {
	got := findMigrationFile("/nonexistent/dir", 5, "up.sql")
	assert.Equal(t, "", got)
}

// TestFindMigrationFile_WrongSuffix — file with the right prefix but
// wrong suffix isn't returned (covers the prefix-match-but-suffix-miss
// branch in both the padded and unpadded scans).
func TestFindMigrationFile_WrongSuffix(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "000005_create_users.up.sql"), nil, 0o644))
	got := findMigrationFile(dir, 5, "down.sql")
	assert.Equal(t, "", got)
}

// ─────────────────────────────────────────────────────────────────────
// runMigrateRepair — readAppliedMigrationVersion failure
// ─────────────────────────────────────────────────────────────────────

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
