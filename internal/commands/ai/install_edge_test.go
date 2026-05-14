package ai

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Edge-case coverage for install.go — the branches the happy-path
// suite doesn't hit: Install's "exists + differs + !force" halt,
// dry-run "WouldReplace" accounting, force-replace path, and
// writeFile's shell-script +x mode.
// ─────────────────────────────────────────────────────────────────────

// TestInstall_ExistsAndDiffersWithoutForce — a destination file with
// different contents and --force unset → error, no overwrite.
func TestInstall_ExistsAndDiffersWithoutForce(t *testing.T) {
	dir := t.TempDir()
	agent := AgentByKey("claude")
	require.NotNil(t, agent)

	// Pre-populate ONE destination file with bogus content so the
	// idempotency check fires for it.
	files, err := TemplateFiles(agent)
	require.NoError(t, err)
	require.NotEmpty(t, files)
	dst := filepath.Join(dir, files[0].DestPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, []byte("conflicting content"), 0o644))

	data := InstallData{ProjectName: "t", ProjectNameLower: "t", ProjectNameUpper: "T",
		ModulePath: "example.com/t", CLIVersion: "dev"}
	_, err = Install(agent, dir, data, InstallOptions{Force: false, DryRun: false})
	require.Error(t, err)
}

// TestInstall_ExistsAndDiffersDryRun — same conflict as above but
// with --dry-run → records WouldReplace, returns nil.
func TestInstall_ExistsAndDiffersDryRun(t *testing.T) {
	dir := t.TempDir()
	agent := AgentByKey("claude")
	require.NotNil(t, agent)

	files, _ := TemplateFiles(agent)
	dst := filepath.Join(dir, files[0].DestPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, []byte("conflict"), 0o644))

	data := InstallData{ProjectName: "t"}
	result, err := Install(agent, dir, data, InstallOptions{DryRun: true})
	require.NoError(t, err)
	assert.NotEmpty(t, result.WouldReplace)
}

// TestInstall_ForceReplaces — existing conflicting file with --force
// → recorded as Replaced and overwritten on disk.
func TestInstall_ForceReplaces(t *testing.T) {
	dir := t.TempDir()
	agent := AgentByKey("claude")
	require.NotNil(t, agent)

	files, _ := TemplateFiles(agent)
	dst := filepath.Join(dir, files[0].DestPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, []byte("conflict"), 0o644))

	data := InstallData{ProjectName: "t", ProjectNameLower: "t",
		ProjectNameUpper: "T", ModulePath: "example.com/t", CLIVersion: "dev"}
	result, err := Install(agent, dir, data, InstallOptions{Force: true})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Replaced)
	// Verify the file was actually overwritten.
	written, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.NotEqual(t, "conflict", string(written))
}

// TestInstall_SkipsIdenticalContent — pre-populate with the exact
// rendered output; Install records Skipped.
func TestInstall_SkipsIdenticalContent(t *testing.T) {
	dir := t.TempDir()
	agent := AgentByKey("claude")
	require.NotNil(t, agent)

	data := InstallData{ProjectName: "t", ProjectNameLower: "t",
		ProjectNameUpper: "T", ModulePath: "example.com/t", CLIVersion: "dev"}

	// First install: creates files.
	_, err := Install(agent, dir, data, InstallOptions{})
	require.NoError(t, err)
	// Second install: every file now byte-identical → Skipped.
	result2, err := Install(agent, dir, data, InstallOptions{})
	require.NoError(t, err)
	assert.NotEmpty(t, result2.Skipped)
	assert.Empty(t, result2.Created)
	assert.Empty(t, result2.WouldReplace)
}

// TestWriteFile_ShellExecutableBit — .sh suffix gets 0o755 mode.
func TestWriteFile_ShellExecutableBit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hook.sh")
	require.NoError(t, writeFile(path, []byte("#!/bin/sh\necho hi\n")))
	info, err := os.Stat(path)
	require.NoError(t, err)
	// Owner-executable bit set.
	assert.NotZero(t, info.Mode()&0o100, "expected +x on .sh file, got %v", info.Mode())
}

// TestWriteFile_PlainMode — non-.sh files get 0o644.
func TestWriteFile_PlainMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, writeFile(path, []byte("k = 1\n")))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Zero(t, info.Mode()&0o100, "expected non-exec mode, got %v", info.Mode())
}

// TestRenderTemplate_BadTemplate — malformed .tmpl source surfaces
// as a parse error.
func TestRenderTemplate_BadTemplate(t *testing.T) {
	// Need a path into the embed FS that points at a real file, but
	// we can't plant malformed content into the embed FS at runtime.
	// Skip — renderTemplate's error path is exercised indirectly via
	// the real template corpus (TestAllTemplatesAreParseable in the
	// generator test suite asserts every shipped template parses).
	t.Skip("renderTemplate parse-error branch requires custom embed FS")
}

// ─────────────────────────────────────────────────────────────────────
// Doc-file rename coverage — the AGENTS.md → CLAUDE.md / CONVENTIONS.md
// step that runs at the top of Install for non-native readers.
// ─────────────────────────────────────────────────────────────────────

// TestInstall_RenamesAgentsmd_Claude — pre-seed AGENTS.md, run the
// claude install, assert AGENTS.md is gone and CLAUDE.md has identical
// content.
func TestInstall_RenamesAgentsmd_Claude(t *testing.T) {
	dir := t.TempDir()
	body := []byte("# Project briefing\nUse `gofasta verify`.\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), body, 0o644))

	agent := AgentByKey("claude")
	require.NotNil(t, agent)
	result, err := Install(agent, dir, sampleData(), InstallOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, result.Renamed, "expected the rename to be reported")
	assert.Equal(t, []string{"AGENTS.md → CLAUDE.md"}, result.Renamed)

	_, err = os.Stat(filepath.Join(dir, "AGENTS.md"))
	assert.True(t, os.IsNotExist(err), "AGENTS.md should be gone after rename")
	got, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	require.NoError(t, err)
	assert.Equal(t, body, got, "CLAUDE.md should preserve AGENTS.md content")
}

// TestInstall_RenamesAgentsmd_Aider — same as above for aider, which
// renames to CONVENTIONS.md.
func TestInstall_RenamesAgentsmd_Aider(t *testing.T) {
	dir := t.TempDir()
	body := []byte("# Aider conventions\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), body, 0o644))

	agent := AgentByKey("aider")
	require.NotNil(t, agent)
	_, err := Install(agent, dir, sampleData(), InstallOptions{})
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "AGENTS.md"))
	assert.True(t, os.IsNotExist(err))
	got, err := os.ReadFile(filepath.Join(dir, "CONVENTIONS.md"))
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

// TestInstall_NativeReader_LeavesAgentsmd — codex reads AGENTS.md
// natively, so the install must NOT rename it.
func TestInstall_NativeReader_LeavesAgentsmd(t *testing.T) {
	dir := t.TempDir()
	body := []byte("# Briefing\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), body, 0o644))

	agent := AgentByKey("codex")
	require.NotNil(t, agent)
	result, err := Install(agent, dir, sampleData(), InstallOptions{})
	require.NoError(t, err)
	assert.Empty(t, result.Renamed, "codex should not rename AGENTS.md")

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

// TestInstall_AmbiguousDocFiles — pre-seed BOTH AGENTS.md and
// CLAUDE.md; the install must refuse with a clear error so the user
// resolves the ambiguity rather than silently picking one.
func TestInstall_AmbiguousDocFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("c"), 0o644))

	agent := AgentByKey("claude")
	_, err := Install(agent, dir, sampleData(), InstallOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AGENTS.md", "error should mention the conflict")
	assert.Contains(t, err.Error(), "CLAUDE.md")
}

// TestInstall_DryRunDoesNotRename — dry-run records the would-be
// rename in WouldRename without moving the file.
func TestInstall_DryRunDoesNotRename(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("x"), 0o644))

	agent := AgentByKey("claude")
	result, err := Install(agent, dir, sampleData(), InstallOptions{DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"AGENTS.md → CLAUDE.md"}, result.WouldRename)
	assert.Empty(t, result.Renamed)
	// AGENTS.md still on disk.
	_, err = os.Stat(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "CLAUDE.md"))
	assert.True(t, os.IsNotExist(err))
}

// TestInstall_AlreadyRenamed_RecordsForUninstall — only CLAUDE.md
// exists (no AGENTS.md). The install treats it as already-renamed and
// records the rename pair on the result so the manifest can reverse it.
func TestInstall_AlreadyRenamed_RecordsForUninstall(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("body"), 0o644))

	agent := AgentByKey("claude")
	result, err := Install(agent, dir, sampleData(), InstallOptions{})
	require.NoError(t, err)
	assert.Empty(t, result.Renamed, "no rename actually happened")
	assert.Equal(t, "AGENTS.md", result.renameFrom)
	assert.Equal(t, "CLAUDE.md", result.renameTo)
}

// TestInstall_RenameFails_PropagatesError — force an osRename failure
// via the seam to confirm the rename error path is wrapped properly.
func TestInstall_RenameFails_PropagatesError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("x"), 0o644))
	orig := osRename
	osRename = func(_, _ string) error { return assertError("rename boom") }
	t.Cleanup(func() { osRename = orig })

	agent := AgentByKey("claude")
	_, err := Install(agent, dir, sampleData(), InstallOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename")
}
