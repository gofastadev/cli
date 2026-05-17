package ai

import (
	"encoding/json"
	"io/fs"
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

// claudeExpectedFiles is the canonical list of project-relative paths
// `gofasta ai claude` writes. Centralized so both
// TestInstall_Claude_CreatesExpectedFiles and the claude row of
// TestInstall_PerAgentTreeShape stay in sync — adding a new template
// file under templates/claude/ requires a single edit here.
func claudeExpectedFiles() []string {
	return []string{
		"CLAUDE.md",
		".claude/settings.json",
		".claude/hooks/pre-commit.sh",
		".claude/hooks/wire-reminder.sh",
		".claude/hooks/migration-reminder.sh",
		".claude/hooks/swagger-reminder.sh",
		".claude/hooks/session-start.sh",
		".claude/commands/verify.md",
		".claude/commands/scaffold.md",
		".claude/commands/inspect.md",
		".claude/commands/status.md",
		".claude/commands/health-check.md",
		".claude/commands/routes.md",
		".claude/commands/rebuild.md",
		".claude/commands/migrate-explain.md",
		".claude/commands/inspect-jobs.md",
		".claude/commands/inspect-tasks.md",
		".claude/commands/xrefs.md",
		".claude/commands/impact.md",
		".claude/commands/debug-slow.md",
		".claude/commands/debug-error.md",
		".claude/commands/n-plus-one.md",
		".claude/commands/g-method.md",
		".claude/commands/g-field.md",
		".claude/commands/g-endpoint.md",
		".claude/commands/g-middleware.md",
		".claude/commands/g-repo-method.md",
		".claude/commands/g-relation.md",
		".claude/commands/g-rename.md",
		".claude/commands/g-mock.md",
		".claude/commands/seed-memory.md",
		".claude/rules/conventions.md",
		".claude/rules/overview.md",
		".claude/rules/workflow.md",
		".claude/rules/commands.md",
		".claude/rules/debugging.md",
		".claude/rules/docs-index.md",
	}
}

// cursorExpectedFiles — see claudeExpectedFiles for the rationale.
func cursorExpectedFiles() []string {
	return []string{
		".cursor/rules/conventions.mdc",
		".cursor/rules/overview.mdc",
		".cursor/rules/workflow.mdc",
		".cursor/rules/commands.mdc",
		".cursor/rules/debugging.mdc",
		".cursor/rules/docs-index.mdc",
		".cursor/commands/status.md",
		".cursor/commands/health-check.md",
		".cursor/commands/routes.md",
		".cursor/commands/rebuild.md",
		".cursor/commands/migrate-explain.md",
		".cursor/commands/inspect-jobs.md",
		".cursor/commands/inspect-tasks.md",
		".cursor/commands/xrefs.md",
		".cursor/commands/impact.md",
		".cursor/commands/debug-slow.md",
		".cursor/commands/debug-error.md",
		".cursor/commands/n-plus-one.md",
		".cursor/commands/g-method.md",
		".cursor/commands/g-field.md",
		".cursor/commands/g-endpoint.md",
		".cursor/commands/g-middleware.md",
		".cursor/commands/g-repo-method.md",
		".cursor/commands/g-relation.md",
		".cursor/commands/g-rename.md",
		".cursor/commands/g-mock.md",
		".cursor/commands/seed-memory.md",
		".cursor/hooks.json",
		".cursor/hooks/wire-reminder.sh",
		".cursor/hooks/migration-reminder.sh",
		".cursor/hooks/swagger-reminder.sh",
		".cursor/hooks/session-start.sh",
	}
}

// codexExpectedFiles — see claudeExpectedFiles for the rationale.
func codexExpectedFiles() []string {
	return []string{
		"AGENTS.md",
		".codex/config.toml",
		".codex/hooks/wire-reminder.sh",
		".codex/hooks/migration-reminder.sh",
		".codex/hooks/swagger-reminder.sh",
		".codex/hooks/session-start.sh",
		".codex/docs/conventions.md",
		".codex/docs/overview.md",
		".codex/docs/workflow.md",
		".codex/docs/commands.md",
		".codex/docs/debugging.md",
		".codex/docs/docs-index.md",
		".codex/prompts/status.md",
		".codex/prompts/health-check.md",
		".codex/prompts/routes.md",
		".codex/prompts/rebuild.md",
		".codex/prompts/migrate-explain.md",
		".codex/prompts/inspect-jobs.md",
		".codex/prompts/inspect-tasks.md",
		".codex/prompts/xrefs.md",
		".codex/prompts/impact.md",
		".codex/prompts/debug-slow.md",
		".codex/prompts/debug-error.md",
		".codex/prompts/n-plus-one.md",
		".codex/prompts/g-method.md",
		".codex/prompts/g-field.md",
		".codex/prompts/g-endpoint.md",
		".codex/prompts/g-middleware.md",
		".codex/prompts/g-repo-method.md",
		".codex/prompts/g-relation.md",
		".codex/prompts/g-rename.md",
		".codex/prompts/g-mock.md",
		".codex/prompts/seed-memory.md",
	}
}

// aiderExpectedFiles — no new files vs prior CLI versions (Aider
// doesn't support custom slash commands or hooks; only content edits
// applied). Listed here for symmetry.
func aiderExpectedFiles() []string {
	return []string{
		"CONVENTIONS.md",
		".aider.conf.yml",
		".aider/docs/conventions.md",
		".aider/docs/overview.md",
		".aider/docs/workflow.md",
		".aider/docs/commands.md",
		".aider/docs/debugging.md",
		".aider/docs/docs-index.md",
	}
}

// windsurfExpectedFiles — see claudeExpectedFiles for the rationale.
func windsurfExpectedFiles() []string {
	return []string{
		".windsurf/rules/conventions.md",
		".windsurf/rules/overview.md",
		".windsurf/rules/workflow.md",
		".windsurf/rules/commands.md",
		".windsurf/rules/debugging.md",
		".windsurf/rules/docs-index.md",
		".windsurf/workflows/status.md",
		".windsurf/workflows/health-check.md",
		".windsurf/workflows/routes.md",
		".windsurf/workflows/rebuild.md",
		".windsurf/workflows/migrate-explain.md",
		".windsurf/workflows/inspect-jobs.md",
		".windsurf/workflows/inspect-tasks.md",
		".windsurf/workflows/xrefs.md",
		".windsurf/workflows/impact.md",
		".windsurf/workflows/debug-slow.md",
		".windsurf/workflows/debug-error.md",
		".windsurf/workflows/n-plus-one.md",
		".windsurf/workflows/g-method.md",
		".windsurf/workflows/g-field.md",
		".windsurf/workflows/g-endpoint.md",
		".windsurf/workflows/g-middleware.md",
		".windsurf/workflows/g-repo-method.md",
		".windsurf/workflows/g-relation.md",
		".windsurf/workflows/g-rename.md",
		".windsurf/workflows/g-mock.md",
		".windsurf/workflows/seed-memory.md",
		".windsurf/hooks.json",
		".windsurf/hooks/wire-reminder.sh",
		".windsurf/hooks/migration-reminder.sh",
		".windsurf/hooks/swagger-reminder.sh",
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

	// Claude installs:
	//   - CLAUDE.md root briefing
	//   - .claude/settings.json + 5 hook scripts
	//   - 24 slash commands (3 originals + 21 new diagnostic/analysis/
	//     debug/generator/memory commands)
	//   - six topic rules under .claude/rules/
	expected := claudeExpectedFiles()
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

// TestInstall_PerAgentTreeShape is a table-driven check that every
// agent's template tree lands at the right paths and that each
// install is fully isolated (no shared chunk directory, no rename of
// pre-existing files). One row per supported agent; if a future agent
// is added to the registry, add a row here.
func TestInstall_PerAgentTreeShape(t *testing.T) {
	cases := []struct {
		key  string
		want []string
	}{
		{key: "claude", want: claudeExpectedFiles()},
		{key: "cursor", want: cursorExpectedFiles()},
		{key: "codex", want: codexExpectedFiles()},
		{key: "aider", want: aiderExpectedFiles()},
		{key: "windsurf", want: windsurfExpectedFiles()},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			dir := t.TempDir()
			agent := AgentByKey(tc.key)
			require.NotNil(t, agent, "registry must include %s", tc.key)
			result, err := Install(agent, dir, sampleData(), InstallOptions{})
			require.NoError(t, err)
			require.NotNil(t, result)
			for _, rel := range tc.want {
				info, err := os.Stat(filepath.Join(dir, rel))
				require.NoError(t, err, "%s install must produce %s", tc.key, rel)
				assert.False(t, info.IsDir(), "%s should be a file, not a directory", rel)
			}
			assert.Len(t, result.Created, len(tc.want),
				"every template file should land on disk on a fresh install")
			// Windsurf has a hard 12 KB per-rule cap.
			if tc.key == "windsurf" {
				entries, _ := filepath.Glob(filepath.Join(dir, ".windsurf", "rules", "*.md"))
				for _, f := range entries {
					info, _ := os.Stat(f)
					assert.LessOrEqual(t, info.Size(), int64(12000),
						"windsurf rule %s must stay under 12 KB", filepath.Base(f))
				}
			}
			// Codex has a 32 KiB hard cap on AGENTS.md.
			if tc.key == "codex" {
				info, _ := os.Stat(filepath.Join(dir, "AGENTS.md"))
				assert.LessOrEqual(t, info.Size(), int64(32*1024),
					"codex AGENTS.md must stay under 32 KiB")
			}
		})
	}
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
// disk and InstallRecord data survives intact.
func TestManifest_LoadSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	m, err := LoadManifest(dir)
	require.NoError(t, err)
	assert.Empty(t, m.Installed, "fresh manifest should be empty")
	assert.Equal(t, manifestSchemaVersion, m.Version)

	m.RecordInstall("claude", "v0.5.0-test",
		[]string{".claude/settings.json", ".claude/commands/verify.md"})
	require.NoError(t, m.Save(dir))

	m2, err := LoadManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, "claude", m2.ActiveAgent)
	rec, ok := m2.Installed["claude"]
	require.True(t, ok)
	assert.Equal(t, "v0.5.0-test", rec.CLIVersion)
	assert.Equal(t, []string{".claude/settings.json", ".claude/commands/verify.md"}, rec.CreatedFiles)
}

// TestJoinShort covers both inline and "(+N more)" overflow branches.
func TestJoinShort(t *testing.T) {
	assert.Equal(t, "", joinShort(nil))
	assert.Equal(t, "a", joinShort([]string{"a"}))
	assert.Equal(t, "a, b, c, d", joinShort([]string{"a", "b", "c", "d"}))
	assert.Equal(t, "a, b, c, d (+2 more)",
		joinShort([]string{"a", "b", "c", "d", "e", "f"}))
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

// TestAgentConflictError_DiffListsRemoveAndAdd — install a previous
// agent, attempt to install another without --switch. The conflict
// error must list the files the previous agent will remove and the
// files the new agent will add.
func TestAgentConflictError_DiffListsRemoveAndAdd(t *testing.T) {
	scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	t.Cleanup(func() { installSwitch = false })

	err := runInstall("codex", false, false)
	require.Error(t, err)
	b, _ := json.Marshal(err)
	assert.Contains(t, string(b), "AI_AGENT_CONFLICT")
	assert.Contains(t, err.Error(), "remove")
	assert.Contains(t, err.Error(), "add")
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

// TestAgentConflictError_NoDiff — prev is an unknown agent (no remove
// diff), target has no templates (synthetic Agent pointing at a
// nonexistent template dir). Diff stays empty and we hit the fallback
// message that includes the `--switch` hint.
func TestAgentConflictError_NoDiff(t *testing.T) {
	m := &Manifest{ActiveAgent: "legacyx", Installed: map[string]InstallRecord{}}
	target := &Agent{Key: "synthetic", Name: "Synthetic", TemplateDir: "templates/nonexistent"}
	err := agentConflictError(m, target, "/nowhere", InstallData{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Re-run with `--switch`")
	assert.Contains(t, err.Error(), "Synthetic")
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

// TestSwitchUninstall_UninstallError — make Uninstall fail inside
// switchUninstall by chmod'ing a parent dir of a recorded file so
// os.Remove returns EACCES. Hits the `err != nil` branch of the
// Uninstall call.
func TestSwitchUninstall_UninstallError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})
	m, err := LoadManifest(dir)
	require.NoError(t, err)

	// Make .claude/commands read-only so os.Remove on a file in it fails.
	cmds := filepath.Join(dir, ".claude", "commands")
	require.NoError(t, os.Chmod(cmds, 0o555))
	t.Cleanup(func() { _ = os.Chmod(cmds, 0o755) })

	err = switchUninstall(m, dir, false)
	require.Error(t, err)
}

// TestRunInstall_SwitchUninstallError — runInstall with --switch
// where switchUninstall fails: chmod a parent dir read-only so the
// inner Uninstall errors. Surfaces the line `if err := switchUninstall(...)
// ... return err` branch inside runInstall.
func TestRunInstall_SwitchUninstallError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	dir := scaffoldFakeProject(t, "example.com/app")
	_ = captureStdout(t, func() {
		require.NoError(t, runInstall("claude", false, false))
	})

	cmds := filepath.Join(dir, ".claude", "commands")
	require.NoError(t, os.Chmod(cmds, 0o555))
	t.Cleanup(func() { _ = os.Chmod(cmds, 0o755) })

	installSwitch = true
	t.Cleanup(func() { installSwitch = false })

	err := runInstall("aider", false, false)
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

// TestAgentOwnedFiles_EmptyForAgentWithoutTemplates — a synthetic
// agent pointing at a nonexistent template dir returns an empty slice
// and no error. Every shipping agent now has templates; this branch
// stays exercised for safety because future agents may register before
// their template tree is authored.
func TestAgentOwnedFiles_EmptyForAgentWithoutTemplates(t *testing.T) {
	agent := &Agent{Key: "synthetic", TemplateDir: "templates/nonexistent"}
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

// TestTemplateFiles_WalkErrorPropagates — inject a non-IsNotExist
// walk error via the fsWalkDir seam. TemplateFiles must surface it
// so the callers (agentOwnedFiles, Install, expectedRenderings) hit
// their error-return branches.
func TestTemplateFiles_WalkErrorPropagates(t *testing.T) {
	orig := fsWalkDir
	fsWalkDir = func(_ fs.FS, _ string, _ fs.WalkDirFunc) error {
		return assertError("synthetic walk failure")
	}
	t.Cleanup(func() { fsWalkDir = orig })

	_, err := TemplateFiles(AgentByKey("claude"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "synthetic walk failure")
}

// TestAgentOwnedFiles_WalkErrorPropagates — fsWalkDir failure
// surfaces through agentOwnedFiles as a CodeAIInstallFailed clierr.
func TestAgentOwnedFiles_WalkErrorPropagates(t *testing.T) {
	orig := fsWalkDir
	fsWalkDir = func(_ fs.FS, _ string, _ fs.WalkDirFunc) error {
		return assertError("synthetic walk failure")
	}
	t.Cleanup(func() { fsWalkDir = orig })

	_, err := agentOwnedFiles(AgentByKey("claude"))
	require.Error(t, err)
}

// TestRunInstall_AgentOwnedFilesError — fsWalkDir failure: the
// agentOwnedFiles call inside runInstall returns an error after the
// install succeeded, so runInstall propagates it before saving the
// manifest.
func TestRunInstall_AgentOwnedFilesError(t *testing.T) {
	scaffoldFakeProject(t, "example.com/app")

	// Install succeeds, but the post-install ownedFiles lookup fails.
	// To trigger this ordering: let TemplateFiles succeed once (for
	// Install's own use) and fail on the second call (agentOwnedFiles).
	orig := fsWalkDir
	calls := 0
	fsWalkDir = func(fsys fs.FS, root string, fn fs.WalkDirFunc) error {
		calls++
		if calls >= 2 {
			return assertError("synthetic walk failure on second call")
		}
		return fs.WalkDir(fsys, root, fn)
	}
	t.Cleanup(func() { fsWalkDir = orig })

	_ = captureStdout(t, func() {
		err := runInstall("claude", false, false)
		require.Error(t, err)
	})
}

// TestInstall_TemplateFilesError — Install's first call site for
// TemplateFiles. fsWalkDir failure on the very first call surfaces
// as a CodeAIInstallFailed wrap.
func TestInstall_TemplateFilesError(t *testing.T) {
	orig := fsWalkDir
	fsWalkDir = func(_ fs.FS, _ string, _ fs.WalkDirFunc) error {
		return assertError("synthetic walk failure")
	}
	t.Cleanup(func() { fsWalkDir = orig })

	dir := t.TempDir()
	_, err := Install(AgentByKey("claude"), dir, sampleData(), InstallOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not enumerate templates")
}

// TestExpectedRenderings_TemplateFilesError — expectedRenderings
// swallows TemplateFiles errors (returns an empty map) per design;
// this exercises that branch via the fsWalkDir seam.
func TestExpectedRenderings_TemplateFilesError(t *testing.T) {
	orig := fsWalkDir
	fsWalkDir = func(_ fs.FS, _ string, _ fs.WalkDirFunc) error {
		return assertError("synthetic walk failure")
	}
	t.Cleanup(func() { fsWalkDir = orig })

	got := expectedRenderings(AgentByKey("claude"), sampleData())
	assert.Empty(t, got)
}
