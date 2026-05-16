package ai

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleData is the standard InstallData used by tests — every agent's
// templates render against it.
func sampleData() InstallData {
	return InstallData{
		ProjectName:      "Myapp",
		ProjectNameLower: "myapp",
		ProjectNameUpper: "MYAPP",
		ModulePath:       "github.com/acme/myapp",
		CLIVersion:       "v0.0.0-test",
	}
}

func TestAgentByKey_ReturnsKnownAgent(t *testing.T) {
	a := AgentByKey("claude")
	require.NotNil(t, a)
	assert.Equal(t, "Claude Code", a.Name)
}

func TestAgentByKey_NilForUnknown(t *testing.T) {
	assert.Nil(t, AgentByKey("nonexistent-agent"))
}

func TestListKeys_Sorted(t *testing.T) {
	keys := ListKeys()
	require.NotEmpty(t, keys)
	for i := 1; i < len(keys); i++ {
		assert.LessOrEqual(t, keys[i-1], keys[i], "ListKeys output must be sorted")
	}
}

// TestInstall_Claude_CreatesExpectedFiles exercises a full end-to-end
// install of the claude templates into a temp directory.
func TestInstall_Claude_CreatesExpectedFiles(t *testing.T) {
	dir := t.TempDir()
	agent := AgentByKey("claude")
	require.NotNil(t, agent)

	result, err := Install(agent, dir, sampleData(), InstallOptions{})
	require.NoError(t, err)
	require.NotNil(t, result)

	// At minimum, claude installs settings.json + the pre-commit hook +
	// the three slash commands, all under .claude/. Verify each one
	// ended up on disk.
	expected := []string{
		".claude/settings.json",
		".claude/hooks/pre-commit.sh",
		".claude/commands/verify.md",
		".claude/commands/scaffold.md",
		".claude/commands/inspect.md",
	}
	for _, rel := range expected {
		path := filepath.Join(dir, rel)
		info, err := os.Stat(path)
		require.NoError(t, err, "expected %s to exist", rel)
		assert.False(t, info.IsDir())
	}

	// Hook must be executable.
	info, err := os.Stat(filepath.Join(dir, ".claude", "hooks", "pre-commit.sh"))
	require.NoError(t, err)
	assert.NotEqual(t, 0, int(info.Mode()&0o111),
		"pre-commit.sh should be executable")

	// Every file should be recorded as Created on a fresh install.
	assert.Len(t, result.Created, len(expected))
	assert.Empty(t, result.Skipped)
	assert.Empty(t, result.Replaced)
}

// TestInstall_Idempotent — running the installer twice should mark every
// file as Skipped the second time (byte-identical content).
func TestInstall_Idempotent(t *testing.T) {
	dir := t.TempDir()
	agent := AgentByKey("claude")

	_, err := Install(agent, dir, sampleData(), InstallOptions{})
	require.NoError(t, err)

	result2, err := Install(agent, dir, sampleData(), InstallOptions{})
	require.NoError(t, err)
	assert.Empty(t, result2.Created, "no new files on second run")
	assert.NotEmpty(t, result2.Skipped, "every file should be skipped")
}

// TestInstall_ExistingDifferentFileBlocks — if the user has edited a
// template-generated file, re-running without --force must halt with a
// clierr.Error rather than silently overwrite.
func TestInstall_ExistingDifferentFileBlocks(t *testing.T) {
	dir := t.TempDir()
	agent := AgentByKey("claude")

	_, err := Install(agent, dir, sampleData(), InstallOptions{})
	require.NoError(t, err)

	// User-edited file — different content from the template.
	settings := filepath.Join(dir, ".claude", "settings.json")
	require.NoError(t, os.WriteFile(settings, []byte(`{"custom":true}`), 0o644))

	_, err = Install(agent, dir, sampleData(), InstallOptions{})
	require.Error(t, err, "second install without --force must refuse to overwrite")
	structured, ok := clierr.As(err)
	require.True(t, ok, "error should be a clierr.Error")
	assert.Equal(t, string(clierr.CodeAIInstallFailed), structured.Code)
}

// TestInstall_ForceOverwrites — same scenario as above but with --force
// succeeds and the new content is on disk.
func TestInstall_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	agent := AgentByKey("claude")

	_, err := Install(agent, dir, sampleData(), InstallOptions{})
	require.NoError(t, err)

	settings := filepath.Join(dir, ".claude", "settings.json")
	require.NoError(t, os.WriteFile(settings, []byte(`{"custom":true}`), 0o644))

	result, err := Install(agent, dir, sampleData(), InstallOptions{Force: true})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Replaced, "Replaced list should include the modified file")

	current, err := os.ReadFile(settings)
	require.NoError(t, err)
	assert.NotContains(t, string(current), `"custom":true`,
		"force install should have overwritten the user edit")
}

// TestInstall_DryRunWritesNothing — in dry-run mode, no files touch disk
// and WouldReplace captures what would have changed.
func TestInstall_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	agent := AgentByKey("claude")

	result, err := Install(agent, dir, sampleData(), InstallOptions{DryRun: true})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Created, "dry-run should report what would be created")

	// Disk should still be empty.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "dry-run must not write files")
}

// TestManifest_LoadSaveRoundtrip — manifest round-trips cleanly through
// disk and InstallRecord data (including v2 fields) survives intact.
func TestManifest_LoadSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	assert.Empty(t, m.Installed, "fresh manifest should be empty")
	assert.Equal(t, manifestSchemaVersion, m.Version)

	m.RecordInstall("claude", "v0.5.0-test",
		[]string{".claude/settings.json", ".claude/commands/verify.md"},
		"AGENTS.md", "CLAUDE.md")
	require.NoError(t, m.Save(dir))

	m2, err := LoadManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, "claude", m2.ActiveAgent)
	rec, ok := m2.Installed["claude"]
	require.True(t, ok)
	assert.Equal(t, "v0.5.0-test", rec.CLIVersion)
	assert.Equal(t, []string{".claude/settings.json", ".claude/commands/verify.md"}, rec.CreatedFiles)
	assert.Equal(t, "AGENTS.md", rec.RenamedFrom)
	assert.Equal(t, "CLAUDE.md", rec.RenamedTo)
}

// TestExtractModulePath — parses `module ...` lines out of go.mod text.
func TestExtractModulePath(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"simple", "module myapp\n\ngo 1.25.0\n", "myapp"},
		{"namespaced", "module github.com/acme/myapp\n\ngo 1.25.0\n", "github.com/acme/myapp"},
		{"leading whitespace", "\nmodule  example.com/x\ngo 1.25\n", "example.com/x"},
		{"missing", "go 1.25.0\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, extractModulePath(tc.in))
		})
	}
}

func TestModuleName(t *testing.T) {
	assert.Equal(t, "myapp", moduleName("myapp"))
	assert.Equal(t, "myapp", moduleName("github.com/acme/myapp"))
	assert.Equal(t, "", moduleName(""))
}

// TestCmdRunE_NoArgs — Cmd.RunE with zero args delegates to cmd.Help().
func TestCmdRunE_NoArgs(t *testing.T) {
	Cmd.SetOut(os.Stderr)
	Cmd.SetErr(os.Stderr)
	require.NoError(t, Cmd.RunE(Cmd, nil))
}

// TestCmdRunE_WithArg — Cmd.RunE with an unknown agent name returns
// the UNKNOWN_AGENT clierr via runInstall.
func TestCmdRunE_WithArg(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	err := Cmd.RunE(Cmd, []string{"nonexistent-agent"})
	require.Error(t, err)
}

// TestListCmdRunE — listCmd.RunE delegates to runList.
func TestListCmdRunE(t *testing.T) {
	_ = captureStdout(t, func() {
		require.NoError(t, listCmd.RunE(listCmd, nil))
	})
}

// TestStatusCmdRunE — statusCmd.RunE delegates to runStatus; outside
// a Go module it returns an error.
func TestStatusCmdRunE(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	err := statusCmd.RunE(statusCmd, nil)
	require.Error(t, err)
}

// ─────────────────────────────────────────────────────────────────────
// agentConflictError — every diff branch
// ─────────────────────────────────────────────────────────────────────

// TestAgentConflictError_PrevRenamedTargetHasDoc — prev agent renamed
// AGENTS.md (aider→CONVENTIONS.md), target also has a DocFilename
// (claude→CLAUDE.md). The diff should describe "rename CONVENTIONS.md → CLAUDE.md".
func TestAgentConflictError_PrevRenamedTargetHasDoc(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
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
// agent the running CLI doesn't know about. agentConflictError should
// still produce a useful error using the recorded key as the name.
func TestAgentConflictError_PrevUnknownAgent(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	m.ActiveAgent = "legacyx"
	require.NoError(t, m.Save(dir))
	t.Cleanup(func() { installSwitch = false })

	err = runInstall("claude", false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "legacyx is currently installed")
}

// TestAgentConflictError_NoDiff — prev is an unknown agent (no rename
// diff, no remove diff), target also has no templates and no DocFilename
// (cursor). Diff stays empty and we hit the fallback message.
func TestAgentConflictError_NoDiff(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	m.ActiveAgent = "legacyx"
	require.NoError(t, m.Save(dir))
	t.Cleanup(func() { installSwitch = false })

	err = runInstall("cursor", false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Re-run with `--switch`")
}

// ─────────────────────────────────────────────────────────────────────
// switchUninstall — early-return + error branches
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
// known agent with NO install record (manifest got out of sync).
// switchUninstall clears ActiveAgent and returns nil.
func TestRunInstall_SwitchPrevAgentNoRecord(t *testing.T) {
	dir := scaffoldFakeProject(t, "example.com/app")
	m, err := LoadManifest(dir)
	require.NoError(t, err)
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
// RecordUninstall. The original agent is still recorded after the
// dry-run switch attempt.
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
	assert.Contains(t, mAfter.Installed, "codex")
	assert.Equal(t, "codex", mAfter.ActiveAgent)
}

// TestSwitchUninstall_BuildInstallDataError — call switchUninstall
// directly so buildInstallData fails specifically inside it.
func TestSwitchUninstall_BuildInstallDataError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod read denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("codex", false, false))
	})
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(filepath.Join(dir, "go.mod"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "go.mod"), 0o644) })

	err = switchUninstall(m, dir, true)
	require.Error(t, err)
}

// TestSwitchUninstall_UninstallError — fail osRename inside Uninstall
// to hit the Uninstall-error branch of switchUninstall.
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

// ─────────────────────────────────────────────────────────────────────
// agentOwnedFiles
// ─────────────────────────────────────────────────────────────────────

// TestAgentOwnedFiles_HappyPath — non-empty slice for an agent with
// templates.
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
// runInstall — Cmd.RunE help path
// ─────────────────────────────────────────────────────────────────────

// TestAiCmd_NoArgsShowsHelp — `gofasta ai` with no agent argument
// prints help and exits 0 instead of erroring.
func TestAiCmd_NoArgsShowsHelp(t *testing.T) {
	_ = captureStdout(t, func() {
		require.NoError(t, Cmd.RunE(Cmd, nil))
	})
}

// TestInstallResult_PrintText_RenameSections — covers the Renamed
// and WouldRename branches that the aggregate sections test in
// runners_test.go doesn't exercise.
func TestInstallResult_PrintText_RenameSections(t *testing.T) {
	r := &InstallResult{
		Agent:       "claude",
		Renamed:     []string{"AGENTS.md → CLAUDE.md"},
		WouldRename: []string{"AGENTS.md → CLAUDE.md"},
	}
	var buf bytes.Buffer
	r.PrintText(&buf)
	out := buf.String()
	assert.Contains(t, out, "renamed: AGENTS.md → CLAUDE.md")
	assert.Contains(t, out, "would rename (dry-run): AGENTS.md → CLAUDE.md")
}
