package ai

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for the `gofasta ai uninstall <agent>` flow:
// runUninstall + Uninstall + UninstallResult.
// ─────────────────────────────────────────────────────────────────────

// TestRunUninstall_RemovesCreatedFiles — install claude, uninstall it,
// assert all the dotfiles are gone and the manifest no longer records
// an active agent.
func TestRunUninstall_RemovesCreatedFiles(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	_, err := os.Stat(filepath.Join(dir, ".claude", "settings.json"))
	require.NoError(t, err)

	_ = captureStdout(t, func() {
		require.NoError(t, runUninstall("claude", false))
	})

	// .claude/ should be gone — including the empty parent dirs.
	_, err = os.Stat(filepath.Join(dir, ".claude"))
	assert.True(t, os.IsNotExist(err), ".claude/ should be removed")
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	assert.Empty(t, m.ActiveAgent)
	_, present := m.Installed["claude"]
	assert.False(t, present)
}

// TestRunUninstall_RestoresOriginalDocFile — install claude with a
// pre-seeded AGENTS.md, uninstall, assert CLAUDE.md is renamed back to
// AGENTS.md with original content preserved.
func TestRunUninstall_RestoresOriginalDocFile(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	body := []byte("# original briefing\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), body, 0o644))

	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	_ = captureStdout(t, func() {
		require.NoError(t, runUninstall("claude", false))
	})

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	assert.Equal(t, body, got)
	_, err = os.Stat(filepath.Join(dir, "CLAUDE.md"))
	assert.True(t, os.IsNotExist(err))
}

// TestRunUninstall_PreservesUserModifiedFiles — user edits one of the
// installed files; uninstall keeps it and reports it as preserved.
func TestRunUninstall_PreservesUserModifiedFiles(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})

	settings := filepath.Join(dir, ".claude", "settings.json")
	require.NoError(t, os.WriteFile(settings, []byte(`{"my":"edits"}`), 0o644))

	out := captureStdout(t, func() {
		require.NoError(t, runUninstall("claude", false))
	})
	// File should still exist (preserved).
	got, err := os.ReadFile(settings)
	require.NoError(t, err)
	assert.Equal(t, `{"my":"edits"}`, string(got))
	assert.Contains(t, out, "preserved")
	assert.Contains(t, out, "settings.json")
}

// TestRunUninstall_UnknownAgent — bad key surfaces UNKNOWN_AGENT.
func TestRunUninstall_UnknownAgent(t *testing.T) {
	scaffoldFakeProject(t, "example.com/app")
	err := runUninstall("nonexistent", false)
	require.Error(t, err)
	b, _ := json.Marshal(err)
	assert.Contains(t, string(b), "UNKNOWN_AGENT")
}

// TestRunUninstall_NotInstalled — calling uninstall for an agent that
// was never installed returns a friendly error, not a panic.
func TestRunUninstall_NotInstalled(t *testing.T) {
	scaffoldFakeProject(t, "example.com/app")
	err := runUninstall("claude", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not currently installed")
}

// TestRunUninstall_DryRun — nothing is removed but the result lists
// what would have been.
func TestRunUninstall_DryRun(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})

	_ = captureStdout(t, func() {
		require.NoError(t, runUninstall("claude", true))
	})
	// .claude/settings.json should still exist after dry-run.
	_, err := os.Stat(filepath.Join(dir, ".claude", "settings.json"))
	require.NoError(t, err)
	// Manifest still records claude as active.
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, "claude", m.ActiveAgent)
}

// TestRunUninstall_FindProjectRootError — outside any Go module.
func TestRunUninstall_FindProjectRootError(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	err := runUninstall("claude", false)
	require.Error(t, err)
}

// TestUninstall_NotFoundFiles — manifest references a file that's
// already gone (user deleted it manually). Uninstall reports it under
// NotFound, doesn't error.
func TestUninstall_NotFoundFiles(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})

	// Delete one tracked file behind the installer's back.
	require.NoError(t, os.Remove(filepath.Join(dir, ".claude", "commands", "verify.md")))

	out := captureStdout(t, func() {
		require.NoError(t, runUninstall("claude", false))
	})
	assert.Contains(t, out, "already gone")
}

// TestUninstallResult_PrintText_AllSections — the human renderer
// covers each section. Per-path decorated lines, not aggregate counts.
func TestUninstallResult_PrintText_AllSections(t *testing.T) {
	r := &UninstallResult{
		Agent:     "claude",
		Renamed:   []string{"CLAUDE.md → AGENTS.md"},
		Removed:   []string{".claude/settings.json", ".claude/commands/verify.md"},
		Preserved: []string{".claude/commands/scaffold.md"},
		NotFound:  []string{".claude/hooks/pre-commit.sh"},
	}
	var buf bytes.Buffer
	r.PrintText(&buf)
	out := buf.String()
	assert.Contains(t, out, "renamed: CLAUDE.md → AGENTS.md")
	assert.Contains(t, out, "removed: .claude/settings.json")
	assert.Contains(t, out, "removed: .claude/commands/verify.md")
	assert.Contains(t, out, "preserved (locally modified): .claude/commands/scaffold.md")
	assert.Contains(t, out, "already gone: .claude/hooks/pre-commit.sh")
}

// TestUninstallCmd_RunE — the cobra-bound RunE delegates to runUninstall.
func TestUninstallCmd_RunE(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	// No go.mod → findProjectRoot fails.
	err := uninstallCmd.RunE(uninstallCmd, []string{"claude"})
	require.Error(t, err)
}

// TestRunUninstall_ManifestSaveError — install then chmod the .gofasta
// dir read-only so m.Save() in runUninstall fails after removal
// succeeded. The error must propagate out of runUninstall.
func TestRunUninstall_ManifestSaveError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	gofastaDir := filepath.Join(dir, ".gofasta")
	require.NoError(t, os.Chmod(gofastaDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(gofastaDir, 0o755) })

	_ = captureStdout(t, func() {
		err := runUninstall("claude", false)
		require.Error(t, err)
	})
}

// TestReverseDocRename_DryRun — install claude with a pre-seeded
// AGENTS.md, then run uninstall in dry-run. The Renamed entry should be
// reported but the file should not actually be renamed back.
func TestReverseDocRename_DryRun(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"),
		[]byte("# briefing\n"), 0o644))

	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	out := captureStdout(t, func() {
		require.NoError(t, runUninstall("claude", true))
	})
	assert.Contains(t, out, "CLAUDE.md → AGENTS.md")
	_, err := os.Stat(filepath.Join(dir, "CLAUDE.md"))
	assert.NoError(t, err, "CLAUDE.md should still exist after dry-run uninstall")
}

// TestReverseDocRename_RenameFails — force osRename to fail so the
// reverse-rename error path is exercised.
func TestReverseDocRename_RenameFails(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"),
		[]byte("# briefing\n"), 0o644))
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})

	orig := osRename
	osRename = func(_, _ string) error { return assertError("rename boom") }
	t.Cleanup(func() { osRename = orig })

	_ = captureStdout(t, func() {
		err := runUninstall("claude", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rename")
	})
	_ = dir
}

// TestReverseDocRename_NoOpWhenAgentReadsAgentsMD — codex has no
// DocFilename, so reverseDocRename should be a no-op (no rename
// recorded → early return).
func TestReverseDocRename_NoOpWhenAgentReadsAgentsMD(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("codex", false, false))
	})
	_ = captureStdout(t, func() {
		require.NoError(t, runUninstall("codex", false))
	})
	_ = dir
}

// TestRemoveOneFile_ReadError — chmod a recorded file to 000 so
// os.ReadFile returns a non-IsNotExist error and removeOneFile
// returns the wrapped error.
func TestRemoveOneFile_ReadError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	settings := filepath.Join(dir, ".claude", "settings.json")
	require.NoError(t, os.Chmod(settings, 0o000))
	t.Cleanup(func() { _ = os.Chmod(settings, 0o644) })

	_ = captureStdout(t, func() {
		err := runUninstall("claude", false)
		require.Error(t, err)
	})
}

// TestRemoveOneFile_RemoveFails — install, then chmod the parent dir
// read-only so os.Remove on the file inside fails.
func TestRemoveOneFile_RemoveFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	parentDir := filepath.Join(dir, ".claude", "commands")
	require.NoError(t, os.Chmod(parentDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(parentDir, 0o755) })

	_ = captureStdout(t, func() {
		err := runUninstall("claude", false)
		require.Error(t, err)
	})
}

// TestRemoveEmptyParents_StopsAtRoot — calling with dir == root is a
// no-op; root is not removed.
func TestRemoveEmptyParents_StopsAtRoot(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	removeEmptyParents(dir, dir)
	_, err := os.Stat(dir)
	assert.NoError(t, err)
}

// TestRemoveEmptyParents_NonEmptyDir — non-empty dir is left alone.
func TestRemoveEmptyParents_NonEmptyDir(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	sub := filepath.Join(dir, "nest", "deep")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "keep.txt"),
		[]byte("x"), 0o644))
	removeEmptyParents(dir, sub)
	_, err := os.Stat(sub)
	assert.NoError(t, err)
}

// TestRunUninstall_LoadManifestError — corrupt manifest causes
// LoadManifest to fail; runUninstall surfaces the error.
func TestRunUninstall_LoadManifestError(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".gofasta"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestPath),
		[]byte("not-json"), 0o644))
	err := runUninstall("claude", false)
	require.Error(t, err)
}

// TestRunUninstall_BuildInstallDataError — go.mod unreadable so the
// inner buildInstallData call inside runUninstall fails.
func TestRunUninstall_BuildInstallDataError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod read denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	require.NoError(t, os.Chmod(filepath.Join(dir, "go.mod"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "go.mod"), 0o644) })

	err := runUninstall("claude", false)
	require.Error(t, err)
}

// TestReverseDocRename_DestMissing — install claude (which records a
// rename), then delete CLAUDE.md so the dest doesn't exist. The early-
// return `!fileExists(dstAbs)` branch fires and uninstall succeeds
// without renaming.
func TestReverseDocRename_DestMissing(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"),
		[]byte("# briefing\n"), 0o644))
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	// Remove CLAUDE.md so the rename target is gone.
	require.NoError(t, os.Remove(filepath.Join(dir, "CLAUDE.md")))

	_ = captureStdout(t, func() {
		require.NoError(t, runUninstall("claude", false))
	})
}
