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
