package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// migrate.go — resolveDownPlan, confirmDestructive, runMigrate
// ─────────────────────────────────────────────────────────────────────

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
// migrateResult document on stdout; captured.String() is empty under
// fakeExec but that's the contract.
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
