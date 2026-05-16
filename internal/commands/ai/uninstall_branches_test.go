package ai

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// uninstall.go — branches not exercised by uninstall_test.go
// ─────────────────────────────────────────────────────────────────────

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
	// Dry-run reports the rename but file should still be CLAUDE.md.
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
	// AGENTS.md exists (codex shipped it) — uninstall should remove it
	// (it's in CreatedFiles) without trying to rename anything.
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

// TestRemoveEmptyParents_StopsAtRoot — happy path: removeEmptyParents
// walks up from a deep nested empty dir but stops at projectRoot.
// Covered indirectly by uninstall flows above, but the trivial
// edge case here exercises the "curAbs == rootAbs" early return when
// passed the root directly.
func TestRemoveEmptyParents_StopsAtRoot(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	// Calling with dir == root is a no-op — root is not removed.
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
