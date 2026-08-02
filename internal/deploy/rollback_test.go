package deploy

import (
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Rollback target selection contract: releases sort by NAME, and the target
// is the newest release STRICTLY OLDER than what `current` points at. A
// readlink failure is a hard error — guessing a target is how a rollback
// lands on the wrong release.

func TestRollback_DryRun_DockerMethod(t *testing.T) {
	cfg := newTestCfg("docker")
	// Dry-run has no remote state to inspect — it describes the plan and
	// succeeds instead of tripping the readlink safety check.
	assert.NoError(t, Rollback(cfg))
}

func TestRollback_DryRun_BinaryMethod(t *testing.T) {
	cfg := newTestCfg("binary")
	assert.NoError(t, Rollback(cfg))
}

// rollbackRules is the happy-path remote state: two releases, current on the
// newer one, the older one carrying its pinned image.
func rollbackRules() []fakeRule {
	return []fakeRule{
		{Match: "ls -1 /opt/test/releases", Stdout: "20260101-000000\n20260102-000000\n"},
		{Match: "readlink /opt/test/current", Stdout: "/opt/test/releases/20260102-000000"},
		{Match: "cat /opt/test/releases/20260101-000000/RELEASE_IMAGE", Stdout: "testapp:20260101-000000"},
	}
}

func TestRollback_Docker_ActivatesPinnedImageThenFlips(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	respondingFakeExec(t, rollbackRules())

	require.NoError(t, Rollback(cfg))

	up := commandsContaining("docker compose -p testapp -f compose.yaml up -d")
	require.NotEmpty(t, up, "rollback must start the target via the fixed compose project")
	assert.Contains(t, up[0], "APP_IMAGE=testapp:20260101-000000",
		"rollback must pin the target release's recorded image")
	assert.Contains(t, up[0], "cd /opt/test/releases/20260101-000000")

	// The pointer must not move before the target is activated.
	flip := commandIndex("ln -sfn /opt/test/releases/20260101-000000 /opt/test/current")
	require.GreaterOrEqual(t, flip, 0, "rollback must repoint current at the target")
	assert.Greater(t, flip, commandIndex("compose.yaml up -d"))
}

func TestRollback_Binary_ReinstallsPreviousBinary(t *testing.T) {
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	respondingFakeExec(t, rollbackRules())

	require.NoError(t, Rollback(cfg))

	install := commandsContaining("sudo cp /opt/test/releases/20260101-000000/testapp /usr/local/bin/testapp")
	require.NotEmpty(t, install, "rollback must reinstall the target release's binary")
	assert.Contains(t, install[0], "systemctl restart testapp")
}

// TestRollback_SkipsNewerLeftoverRelease — a directory NEWER than current
// (e.g. from an interrupted deploy) must never be selected: it is the
// release that just failed.
func TestRollback_SkipsNewerLeftoverRelease(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	respondingFakeExec(t, []fakeRule{
		{Match: "ls -1 /opt/test/releases", Stdout: "20260101-000000\n20260102-000000\n20260103-000000\n"},
		{Match: "readlink /opt/test/current", Stdout: "/opt/test/releases/20260102-000000"},
		{Match: "cat /opt/test/releases/20260101-000000/RELEASE_IMAGE", Stdout: "testapp:20260101-000000"},
	})

	require.NoError(t, Rollback(cfg))

	assert.Empty(t, commandsContaining("cd /opt/test/releases/20260103-000000"),
		"the leftover release newer than current must not be activated")
	assert.NotEmpty(t, commandsContaining("cd /opt/test/releases/20260101-000000"),
		"the newest release older than current is the rollback target")
}

func TestRollback_ReadlinkFails(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	respondingFakeExec(t, []fakeRule{
		{Match: "ls -1 /opt/test/releases", Stdout: "20260101-000000\n20260102-000000\n"},
		{Match: "readlink", Exit: 1},
	})

	err := Rollback(cfg)
	require.Error(t, err, "an unreadable current pointer must abort the rollback, not guess a target")
	ce, ok := clierr.As(err)
	require.True(t, ok)
	assert.Equal(t, string(clierr.CodeRollbackFailed), ce.Code)
	assert.Empty(t, commandsContaining("compose.yaml up"), "nothing may be activated without a trustworthy pointer")
}

func TestRollback_ListFails(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	respondingFakeExec(t, []fakeRule{{Match: "ls -1", Exit: 1}})
	assert.Error(t, Rollback(cfg))
}

func TestRollback_MissingReleaseImage(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	rules := rollbackRules()
	rules[2] = fakeRule{Match: "RELEASE_IMAGE", Exit: 1}
	respondingFakeExec(t, rules)

	err := Rollback(cfg)
	require.Error(t, err, "a docker release without RELEASE_IMAGE cannot be activated")
	assert.Empty(t, commandsContaining("compose.yaml up"))
}

func TestRollback_NoPreviousFound(t *testing.T) {
	// current IS the oldest release — nothing strictly older exists.
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	respondingFakeExec(t, []fakeRule{
		{Match: "ls -1 /opt/test/releases", Stdout: "20260101-000000\n20260102-000000\n"},
		{Match: "readlink /opt/test/current", Stdout: "/opt/test/releases/20260101-000000"},
	})
	err := Rollback(cfg)
	assert.Error(t, err)
}

func TestRollback_ComposeUpFails(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	rules := append(rollbackRules(), fakeRule{Match: "compose.yaml up -d", Exit: 1})
	respondingFakeExec(t, rules)
	assert.Error(t, Rollback(cfg))
}

func TestRollback_BinaryInstallFails(t *testing.T) {
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	rules := append(rollbackRules(), fakeRule{Match: "systemctl restart", Exit: 1})
	respondingFakeExec(t, rules)
	assert.Error(t, Rollback(cfg))
}

func TestRollback_SymlinkFails(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	rules := append(rollbackRules(), fakeRule{Match: "ln -sfn", Exit: 1})
	respondingFakeExec(t, rules)
	assert.Error(t, Rollback(cfg))
}

// TestRollback_UnhealthyTargetRestoresCurrent — if the rollback target fails
// its health gate, the release that was live before must be re-activated.
func TestRollback_UnhealthyTargetRestoresCurrent(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	rules := append(rollbackRules(),
		fakeRule{Match: "wget -qO /dev/null", Exit: 1},
		fakeRule{Match: "cat /opt/test/releases/20260102-000000/RELEASE_IMAGE", Stdout: "testapp:20260102-000000"},
	)
	respondingFakeExec(t, rules)

	err := Rollback(cfg)
	require.Error(t, err)
	restore := commandsContaining("APP_IMAGE=testapp:20260102-000000")
	assert.NotEmpty(t, restore, "the previously-live release must be restored after an unhealthy rollback target")
}

// TestRollback_NoPreviousRelease — empty listing → error.
func TestRollback_NoPreviousRelease(t *testing.T) {
	cfg := newTestCfg("binary")
	cfg.DryRun = false
	withFakeExecStdout(t, 0, "")
	err := Rollback(cfg)
	require.Error(t, err)
}
