package ai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// agentConflictError — every diff branch
// ─────────────────────────────────────────────────────────────────────

// TestAgentConflictError_PrevRenamedTargetHasDoc — prev agent renamed
// AGENTS.md (aider→CONVENTIONS.md), target also has a DocFilename
// (claude→CLAUDE.md). The diff should describe "rename CONVENTIONS.md → CLAUDE.md".
func TestAgentConflictError_PrevRenamedTargetHasDoc(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	// Pre-seed AGENTS.md so aider's rename has something to act on.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"),
		[]byte("# briefing\n"), 0o644))

	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("aider", false, false))
	})
	t.Cleanup(func() { installSwitch = false })

	err := runInstall("claude", false, false)
	require.Error(t, err)
	b, _ := json.Marshal(err)
	assert.Contains(t, string(b), "AI_AGENT_CONFLICT")
	assert.Contains(t, err.Error(), "rename CONVENTIONS.md → CLAUDE.md")
}

// TestAgentConflictError_PrevRenamedTargetNoDoc — prev agent renamed
// AGENTS.md (claude→CLAUDE.md), target has NO DocFilename (cursor).
// The diff should reverse the rename: "CLAUDE.md → AGENTS.md".
func TestAgentConflictError_PrevRenamedTargetNoDoc(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"),
		[]byte("# briefing\n"), 0o644))

	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	t.Cleanup(func() { installSwitch = false })

	err := runInstall("cursor", false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename CLAUDE.md → AGENTS.md")
}

// TestAgentConflictError_PrevUnknownAgent — manifest references an
// agent the running CLI doesn't know about (e.g. older or experimental
// agent). agentConflictError should still produce a useful error using
// the recorded key as the name. Reuses the manifest-load path: install
// nothing, hand-write a manifest with ActiveAgent="legacyx".
func TestAgentConflictError_PrevUnknownAgent(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	m.ActiveAgent = "legacyx"
	require.NoError(t, m.Save(dir))
	t.Cleanup(func() { installSwitch = false })

	err = runInstall("claude", false, false)
	require.Error(t, err)
	// "legacyx" → no entries in Installed, target has DocFilename,
	// so diff is: "rename AGENTS.md → CLAUDE.md" + "add ...".
	assert.Contains(t, err.Error(), "legacyx is currently installed")
}

// ─────────────────────────────────────────────────────────────────────
// switchUninstall — every early-return branch
// ─────────────────────────────────────────────────────────────────────

// TestRunInstall_SwitchPrevAgentUnknown — manifest references an
// unknown agent under --switch. switchUninstall clears ActiveAgent
// and returns nil so the new install proceeds.
func TestRunInstall_SwitchPrevAgentUnknown(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	m.ActiveAgent = "legacyx"
	require.NoError(t, m.Save(dir))

	installSwitch = true
	t.Cleanup(func() { installSwitch = false })

	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("codex", false, false))
	})
	m2, err := LoadManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, "codex", m2.ActiveAgent)
}

// TestRunInstall_SwitchPrevAgentNoRecord — ActiveAgent points to a
// known agent that has NO Install record (manifest got out of sync).
// switchUninstall clears ActiveAgent and returns nil.
func TestRunInstall_SwitchPrevAgentNoRecord(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	// Active is "aider" but no record exists in Installed.
	m.ActiveAgent = "aider"
	require.NoError(t, m.Save(dir))

	installSwitch = true
	t.Cleanup(func() { installSwitch = false })

	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("codex", false, false))
	})
}

// TestRunInstall_SwitchDryRunDoesNotUpdateManifest — --switch +
// --dry-run runs the uninstall in dry-run mode AND skips
// RecordUninstall. Verify the original agent is still recorded after
// the dry-run switch attempt.
func TestRunInstall_SwitchDryRunDoesNotUpdateManifest(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("codex", false, false))
	})
	mBefore, err := LoadManifest(dir)
	require.NoError(t, err)
	require.Contains(t, mBefore.Installed, "codex")

	installSwitch = true
	t.Cleanup(func() { installSwitch = false })
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", true, false))
	})

	mAfter, err := LoadManifest(dir)
	require.NoError(t, err)
	// Dry-run path: codex install record stays put, ActiveAgent unchanged.
	assert.Contains(t, mAfter.Installed, "codex")
	assert.Equal(t, "codex", mAfter.ActiveAgent)
}

// TestRunInstall_SwitchBuildInstallDataError — buildInstallData fails
// inside switchUninstall when go.mod becomes unreadable between the
// outer buildInstallData (in runInstall) and the inner one. Triggered
// by having a chmod 000 go.mod — but runInstall does buildInstallData
// before reaching switchUninstall, so the OUTER call fails first.
// Skipped in favor of TestRunInstall_BuildInstallDataError above —
// the inner call is only reachable via tests that simulate the failure
// happening exclusively in switchUninstall, which our code structure
// doesn't easily allow without a second seam.

// ─────────────────────────────────────────────────────────────────────
// agentOwnedFiles — TemplateFiles error
// ─────────────────────────────────────────────────────────────────────

// TestAgentOwnedFiles_HappyPath — sanity that the function returns a
// non-empty slice for an agent with templates.
func TestAgentOwnedFiles_HappyPath(t *testing.T) {
	agent := AgentByKey("claude")
	require.NotNil(t, agent)
	files, err := agentOwnedFiles(agent)
	require.NoError(t, err)
	assert.NotEmpty(t, files)
}

// TestAgentOwnedFiles_EmptyForAgentWithoutTemplates — cursor has no
// embedded template dir; agentOwnedFiles returns an empty slice and
// no error (the "agent installs nothing on disk" representation).
func TestAgentOwnedFiles_EmptyForAgentWithoutTemplates(t *testing.T) {
	agent := AgentByKey("cursor")
	require.NotNil(t, agent)
	files, err := agentOwnedFiles(agent)
	require.NoError(t, err)
	assert.Empty(t, files)
}

// ─────────────────────────────────────────────────────────────────────
// runInstall — explicit Help path via Cmd.RunE with no args
// ─────────────────────────────────────────────────────────────────────

// TestAiCmd_NoArgsShowsHelp — `gofasta ai` with no agent argument
// prints help and exits 0 instead of erroring.
func TestAiCmd_NoArgsShowsHelp(t *testing.T) {
	_ = captureStdout(t, func() {
		require.NoError(t, Cmd.RunE(Cmd, nil))
	})
}

// TestAgentConflictError_NoDiff — prev is an unknown agent (so no
// rename diff, no remove diff) and target also has no templates and
// no DocFilename (cursor) — diff stays empty and we hit the fallback
// "Re-run with `--switch`" message.
func TestAgentConflictError_NoDiff(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	m.ActiveAgent = "legacyx"
	require.NoError(t, m.Save(dir))
	t.Cleanup(func() { installSwitch = false })

	err = runInstall("cursor", false, false)
	require.Error(t, err)
	// Empty-diff branch ends with the literal fallback hint.
	assert.Contains(t, err.Error(), "Re-run with `--switch`")
}

// TestSwitchUninstall_BuildInstallDataError — call switchUninstall
// directly so we can fail buildInstallData specifically inside it
// (runInstall does its own outer buildInstallData call first and
// would otherwise short-circuit before reaching switchUninstall).
func TestSwitchUninstall_BuildInstallDataError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod read denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	// Install codex so there's an active record to uninstall.
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("codex", false, false))
	})
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	// Break go.mod read permission.
	require.NoError(t, os.Chmod(filepath.Join(dir, "go.mod"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "go.mod"), 0o644) })

	err = switchUninstall(m, dir, true)
	require.Error(t, err)
}

// TestSwitchUninstall_UninstallError — same direct-call seam, but this
// time fail osRename inside Uninstall (so the previous agent's rename
// can't be reversed) to hit the Uninstall-error branch of switchUninstall.
func TestSwitchUninstall_UninstallError(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"),
		[]byte("# briefing\n"), 0o644))
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	m, err := LoadManifest(dir)
	require.NoError(t, err)

	orig := osRename
	osRename = func(_, _ string) error { return assertError("rename boom") }
	t.Cleanup(func() { osRename = orig })

	err = switchUninstall(m, dir, false)
	require.Error(t, err)
}
