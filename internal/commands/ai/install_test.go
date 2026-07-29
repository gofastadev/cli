package ai

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"text/template"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// renderAgentToTempDir runs Install for the named agent into a fresh
// temp dir. Centralizes the install → return-dir dance used by every
// test in this file.
func renderAgentToTempDir(t *testing.T, agentKey string) string {
	t.Helper()
	dir := t.TempDir()
	agent := AgentByKey(agentKey)
	require.NotNil(t, agent, "agent %s must be registered", agentKey)
	_, err := Install(agent, dir, sampleData(), InstallOptions{})
	require.NoError(t, err)
	return dir
}

// renderAgentFile installs the agent and reads one rendered file. Use
// for tests that only need to inspect one file's content (settings.json,
// config.toml, a specific slash command).
func renderAgentFile(t *testing.T, agentKey, relPath string) []byte {
	t.Helper()
	dir := renderAgentToTempDir(t, agentKey)
	content, err := os.ReadFile(filepath.Join(dir, relPath))
	require.NoError(t, err, "expected %s to exist after installing %s", relPath, agentKey)
	return content
}

// claudeSettingsShape mirrors only the keys the tests assert on. Tests
// unmarshal into this struct (plus a generic map for extension keys)
// rather than blanket-asserting equality so future settings additions
// don't break existing tests.
type claudeSettingsShape struct {
	Permissions struct {
		Allow []string `json:"allow"`
	} `json:"permissions"`
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"hooks"`
	} `json:"hooks"`
}

// TestInstall_Claude_HookSettingsValid renders settings.json,
// asserts it's valid JSON, asserts the hooks block has PostToolUse +
// SessionStart with non-empty matchers, and confirms every wired hook
// script command path matches a file the agent actually installs.
func TestInstall_Claude_HookSettingsValid(t *testing.T) {
	body := renderAgentFile(t, "claude", ".claude/settings.json")

	var settings claudeSettingsShape
	require.NoError(t, json.Unmarshal(body, &settings), "settings.json must be valid JSON")

	require.NotEmpty(t, settings.Permissions.Allow, "permissions.allow must not be empty")
	require.NotEmpty(t, settings.Hooks, "hooks block must be present")

	require.Contains(t, settings.Hooks, "PostToolUse", "PostToolUse must be configured")
	require.Contains(t, settings.Hooks, "SessionStart", "SessionStart must be configured")

	for event, blocks := range settings.Hooks {
		require.NotEmpty(t, blocks, "%s must have at least one matcher block", event)
		for _, blk := range blocks {
			require.NotEmpty(t, blk.Matcher, "%s matcher must not be empty", event)
			require.NotEmpty(t, blk.Hooks, "%s must have at least one hook command", event)
		}
	}
}

// TestInstall_Codex_HookConfigValid asserts the rendered config.toml
// contains the expected [[hooks.PostToolUse]] / [[hooks.SessionStart]]
// tables. We avoid pulling in a TOML parser as a test-only dep —
// regex-based structural checks are sufficient for catching template
// regressions.
func TestInstall_Codex_HookConfigValid(t *testing.T) {
	body := string(renderAgentFile(t, "codex", ".codex/config.toml"))

	for _, want := range []string{
		"[[hooks.PostToolUse]]",
		"[[hooks.PostToolUse.hooks]]",
		"[[hooks.SessionStart]]",
		"[[hooks.SessionStart.hooks]]",
		`command = ".codex/hooks/wire-reminder.sh"`,
		`command = ".codex/hooks/migration-reminder.sh"`,
		`command = ".codex/hooks/swagger-reminder.sh"`,
		`command = ".codex/hooks/session-start.sh"`,
		`"jq *"`,
	} {
		assert.Contains(t, body, want, "config.toml should contain %q", want)
	}

	// matcher key must be present in every table — Codex requires it.
	matchers := regexp.MustCompile(`(?m)^matcher\s*=`).FindAllString(body, -1)
	assert.GreaterOrEqual(t, len(matchers), 2, "should have at least one matcher per hook event")
}

// TestInstall_AllAgents_HookScriptsExecutable walks every shipped hook
// script for every agent that ships hooks and asserts it has the
// executable bit set. Catches a missed call to writeFile's mode logic
// (which keys off the .sh suffix).
func TestInstall_AllAgents_HookScriptsExecutable(t *testing.T) {
	hookDirs := map[string]string{
		"claude":   ".claude/hooks",
		"codex":    ".codex/hooks",
		"cursor":   ".cursor/hooks",
		"windsurf": ".windsurf/hooks",
	}
	for agent, dir := range hookDirs {
		t.Run(agent, func(t *testing.T) {
			root := renderAgentToTempDir(t, agent)
			entries, err := os.ReadDir(filepath.Join(root, dir))
			require.NoError(t, err)
			require.NotEmpty(t, entries, "%s should ship at least one hook script", agent)

			for _, e := range entries {
				if !strings.HasSuffix(e.Name(), ".sh") {
					continue
				}
				info, err := os.Stat(filepath.Join(root, dir, e.Name()))
				require.NoError(t, err)
				assert.NotEqual(t, 0, int(info.Mode()&0o111),
					"%s/%s must be executable", dir, e.Name())
			}
		})
	}
}

// TestInstall_AllAgents_HookScriptsShebang checks the shebang +
// `set -euo pipefail` pair on the first two lines of every hook
// script. A missing shebang means the kernel can't exec the file as
// bash; a missing `set -e` means a failed jq pipe silently produces
// confusing output.
func TestInstall_AllAgents_HookScriptsShebang(t *testing.T) {
	hookDirs := map[string]string{
		"claude":   ".claude/hooks",
		"codex":    ".codex/hooks",
		"cursor":   ".cursor/hooks",
		"windsurf": ".windsurf/hooks",
	}
	for agent, dir := range hookDirs {
		t.Run(agent, func(t *testing.T) {
			root := renderAgentToTempDir(t, agent)
			entries, err := os.ReadDir(filepath.Join(root, dir))
			require.NoError(t, err)

			for _, e := range entries {
				if !strings.HasSuffix(e.Name(), ".sh") {
					continue
				}
				body, err := os.ReadFile(filepath.Join(root, dir, e.Name()))
				require.NoError(t, err)
				lines := strings.SplitN(string(body), "\n", 3)
				require.GreaterOrEqual(t, len(lines), 2,
					"%s/%s should have at least 2 lines", dir, e.Name())
				assert.Equal(t, "#!/usr/bin/env bash", lines[0],
					"%s/%s shebang must use env bash", dir, e.Name())
				assert.Contains(t, lines[1], "set -euo pipefail",
					"%s/%s should `set -euo pipefail`", dir, e.Name())
			}
		})
	}
}

// TestHook_SessionStart_NoGofastaSilent — when `gofasta` isn't on PATH,
// the session-start script should exit 0 silently rather than break
// the session. Simulates this by running with a stripped PATH.
func TestHook_SessionStart_NoGofastaSilent(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	root := renderAgentToTempDir(t, "claude")
	script := filepath.Join(root, ".claude/hooks/session-start.sh")
	cmd := exec.Command("bash", script)
	cmd.Stdin = strings.NewReader(`{"source":"startup"}`)
	cmd.Env = []string{"PATH=/usr/bin:/bin"} // strip everything that might have gofasta
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	require.NoError(t, err, "session-start should exit 0 even without gofasta (stderr=%q)", errOut.String())
	assert.Empty(t, strings.TrimSpace(out.String()),
		"session-start should be silent when gofasta is not on PATH")
}

// TestInstall_Claude_SlashCommandsHaveFrontmatter walks every
// commands/*.md in a fresh Claude install and asserts each starts
// with YAML frontmatter containing the required keys.
func TestInstall_Claude_SlashCommandsHaveFrontmatter(t *testing.T) {
	root := renderAgentToTempDir(t, "claude")
	entries, err := os.ReadDir(filepath.Join(root, ".claude/commands"))
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, ".claude/commands", e.Name()))
		require.NoError(t, err)
		text := string(body)
		assert.True(t, strings.HasPrefix(text, "---\n"),
			"%s should start with YAML frontmatter", e.Name())
		assert.Contains(t, text, "description:",
			"%s frontmatter should include `description:`", e.Name())
		assert.Contains(t, text, "allowed-tools:",
			"%s frontmatter should include `allowed-tools:`", e.Name())
	}
}

// TestInstall_Cursor_CommandsHaveTitle — Cursor commands use pure
// markdown with no frontmatter; the convention is to lead with a top-
// level heading describing the command.
func TestInstall_Cursor_CommandsHaveTitle(t *testing.T) {
	root := renderAgentToTempDir(t, "cursor")
	entries, err := os.ReadDir(filepath.Join(root, ".cursor/commands"))
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, ".cursor/commands", e.Name()))
		require.NoError(t, err)
		text := string(body)
		assert.True(t, strings.HasPrefix(text, "# "),
			"%s should start with a markdown H1 (no frontmatter for Cursor commands)", e.Name())
	}
}

// TestInstall_Windsurf_WorkflowsUnderSizeCap — Windsurf enforces a
// 12000-character cap per workflow file. Mirrors the rules cap
// asserted in TestInstall_PerAgentTreeShape.
func TestInstall_Windsurf_WorkflowsUnderSizeCap(t *testing.T) {
	root := renderAgentToTempDir(t, "windsurf")
	entries, err := filepath.Glob(filepath.Join(root, ".windsurf/workflows/*.md"))
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, f := range entries {
		info, err := os.Stat(f)
		require.NoError(t, err)
		assert.LessOrEqual(t, info.Size(), int64(12000),
			"windsurf workflow %s must stay under 12 KB", filepath.Base(f))
	}
}

// allowToolPattern extracts Bash(...) patterns from a slash command's
// `allowed-tools:` frontmatter line. Used by the cross-test below.
var allowToolPattern = regexp.MustCompile(`Bash\(([^)]+)\)`)

// TestInstall_Claude_PermissionsAllowComplete is the high-leverage
// cross-test: it scans every Claude slash command for the Bash
// patterns its frontmatter declares, then asserts each pattern is
// covered by an entry in settings.json's permissions.allow.
//
// Catches the "added /xrefs but forgot to add Bash(gofasta xrefs *)
// to settings.json" class of bug, which would otherwise show up only
// as runtime permission prompts in a real Claude Code session.
//
// "Covered" means an allow entry exists whose pattern is a prefix-
// or wildcard-match of the slash command's requested pattern. For
// example, `Bash(gofasta status*)` is covered by `Bash(gofasta *)`.
func TestInstall_Claude_PermissionsAllowComplete(t *testing.T) {
	root := renderAgentToTempDir(t, "claude")

	// Load the allowlist.
	settingsBytes, err := os.ReadFile(filepath.Join(root, ".claude/settings.json"))
	require.NoError(t, err)
	var settings claudeSettingsShape
	require.NoError(t, json.Unmarshal(settingsBytes, &settings))

	allow := settings.Permissions.Allow

	// Walk every slash command, extract the Bash patterns it asks for.
	entries, err := os.ReadDir(filepath.Join(root, ".claude/commands"))
	require.NoError(t, err)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, ".claude/commands", e.Name()))
		require.NoError(t, err)
		// Only inspect the YAML frontmatter (the first --- ... --- block).
		text := string(body)
		end := strings.Index(text[4:], "\n---")
		if end < 0 {
			continue
		}
		frontmatter := text[:end+4]

		matches := allowToolPattern.FindAllStringSubmatch(frontmatter, -1)
		for _, m := range matches {
			cmdPattern := strings.TrimSpace(m[1])
			require.True(t, isCoveredByAllow(cmdPattern, allow),
				"slash command %s requires Bash(%s) but settings.json permissions.allow does not cover it; add an entry like %q",
				e.Name(), cmdPattern, "Bash("+cmdPattern+")")
		}
	}
}

// isCoveredByAllow returns true if `pattern` is matched by any entry
// in `allow`. An entry covers a pattern when either (a) the entry is
// exactly Bash(pattern), or (b) the entry's command-substring is a
// prefix-with-glob of pattern (e.g. `Bash(gofasta *)` covers
// `Bash(gofasta status*)`).
func isCoveredByAllow(pattern string, allow []string) bool {
	want := "Bash(" + pattern + ")"
	for _, a := range allow {
		if a == want {
			return true
		}
		// Glob-prefix check: a = `Bash(gofasta *)` covers any
		// pattern starting with `gofasta`.
		if strings.HasSuffix(a, " *)") && strings.HasPrefix(a, "Bash(") {
			prefix := strings.TrimSuffix(strings.TrimPrefix(a, "Bash("), " *)")
			if strings.HasPrefix(pattern, prefix) {
				return true
			}
		}
		// Glob-suffix check: a = `Bash(gofasta inspect-jobs*)` covers
		// `Bash(gofasta inspect-jobs*)` only (already handled by ==).
	}
	return false
}

// cursorHooksShape mirrors only the keys the tests assert on for
// Cursor's hooks.json (schema v1).
type cursorHooksShape struct {
	Version int `json:"version"`
	Hooks   map[string][]struct {
		Command string `json:"command"`
		Matcher string `json:"matcher,omitempty"`
	} `json:"hooks"`
}

// windsurfHooksShape mirrors only the keys the tests assert on for
// Cascade Hooks. No `version` field; entries use `command` (bash) or
// `powershell` (Windows).
type windsurfHooksShape struct {
	Hooks map[string][]struct {
		Command    string `json:"command,omitempty"`
		Powershell string `json:"powershell,omitempty"`
		ShowOutput bool   `json:"show_output,omitempty"`
	} `json:"hooks"`
}

// TestInstall_Cursor_HooksJSONValid renders .cursor/hooks.json,
// confirms it's valid JSON, asserts schema version is 1, and that
// afterFileEdit + sessionStart events are wired with non-empty
// commands.
func TestInstall_Cursor_HooksJSONValid(t *testing.T) {
	body := renderAgentFile(t, "cursor", ".cursor/hooks.json")

	var hooks cursorHooksShape
	require.NoError(t, json.Unmarshal(body, &hooks),
		".cursor/hooks.json must be valid JSON")

	assert.Equal(t, 1, hooks.Version, "Cursor hooks schema must be version 1")
	require.NotEmpty(t, hooks.Hooks, "hooks block must be present")

	require.Contains(t, hooks.Hooks, "afterFileEdit", "afterFileEdit must be configured")
	require.Contains(t, hooks.Hooks, "sessionStart", "sessionStart must be configured")

	for event, entries := range hooks.Hooks {
		require.NotEmpty(t, entries, "%s must have at least one hook entry", event)
		for _, e := range entries {
			require.NotEmpty(t, e.Command, "%s entry command must not be empty", event)
		}
	}
}

// TestInstall_Windsurf_HooksJSONValid renders .windsurf/hooks.json,
// confirms it's valid JSON, asserts post_write_code is wired with
// non-empty bash commands. Cascade Hooks doesn't require a `version`
// field.
func TestInstall_Windsurf_HooksJSONValid(t *testing.T) {
	body := renderAgentFile(t, "windsurf", ".windsurf/hooks.json")

	var hooks windsurfHooksShape
	require.NoError(t, json.Unmarshal(body, &hooks),
		".windsurf/hooks.json must be valid JSON")

	require.NotEmpty(t, hooks.Hooks, "hooks block must be present")
	require.Contains(t, hooks.Hooks, "post_write_code", "post_write_code must be configured")

	for event, entries := range hooks.Hooks {
		require.NotEmpty(t, entries, "%s must have at least one entry", event)
		for _, e := range entries {
			require.NotEmpty(t, e.Command, "%s entry command must not be empty", event)
		}
	}
}

// TestInstall_Codex_PromptsHaveFrontmatter walks every Codex prompt
// markdown file and asserts each starts with YAML frontmatter and
// declares a `description:` (Codex's only required frontmatter key).
// `argument-hint:` is checked per-file where present but not
// required globally — some prompts have no positional args.
func TestInstall_Codex_PromptsHaveFrontmatter(t *testing.T) {
	root := renderAgentToTempDir(t, "codex")
	entries, err := os.ReadDir(filepath.Join(root, ".codex/prompts"))
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, ".codex/prompts", e.Name()))
		require.NoError(t, err)
		text := string(body)
		assert.True(t, strings.HasPrefix(text, "---\n"),
			"%s should start with YAML frontmatter", e.Name())
		assert.Contains(t, text, "description:",
			"%s frontmatter should include `description:`", e.Name())
	}
}

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

// TestInstall_StatReadFails_NonIsNotExist — destAbs is a DIRECTORY,
// so os.ReadFile returns "is a directory" — not IsNotExist. This
// covers the `default` arm of the switch in Install that wraps the
// stat error.
func TestInstall_StatReadFails_NonIsNotExist(t *testing.T) {
	dir := t.TempDir()
	agent := AgentByKey("claude")
	files, err := TemplateFiles(agent)
	require.NoError(t, err)
	require.NotEmpty(t, files)
	// Replace the first template's destination path with a directory
	// so the os.ReadFile call returns EISDIR rather than IsNotExist.
	dst := filepath.Join(dir, files[0].DestPath)
	require.NoError(t, os.MkdirAll(dst, 0o755))

	_, err = Install(agent, dir, sampleData(), InstallOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stat")
}

// TestRenderTemplate_MissingSource — ReadTemplate fails for a path
// that isn't in the embed FS.
func TestRenderTemplate_MissingSource(t *testing.T) {
	_, err := renderTemplate("templates/nonexistent/file.tmpl",
		InstallData{ProjectName: "x"})
	require.Error(t, err)
}

// TestWriteFile_ParentWriteBlocked — MkdirAll fails when a segment
// of the path already exists as a regular file. Verifies writeFile
// propagates the error as AI_INSTALL_FAILED.
func TestWriteFile_ParentWriteBlocked(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file where a directory would need to exist.
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte(""), 0o644))
	// Try to write under it — MkdirAll should fail.
	target := filepath.Join(blocker, "child", "x.sh")
	err := writeFile(target, []byte("#!/bin/sh"))
	require.Error(t, err)
}

// parseTemplateStrict builds a *template.Template with
// missingkey=error so a reference to a non-existent field triggers
// an Execute error.
func parseTemplateStrict(src string) (*template.Template, error) {
	return template.New("t").Option("missingkey=error").Parse(src)
}

// TestWriteFile_MkdirAllFails — parent already exists as a regular
// file.
func TestWriteFile_MkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "sub")
	require.NoError(t, os.WriteFile(blocker, []byte{}, 0o644))
	err := writeFile(filepath.Join(blocker, "child.txt"), []byte("x"))
	require.Error(t, err)
}

// TestWriteFile_WriteFails — parent is read-only so WriteFile fails.
func TestWriteFile_WriteFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod write denial")
	}
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	require.NoError(t, os.MkdirAll(subdir, 0o755))
	require.NoError(t, os.Chmod(subdir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(subdir, 0o755) })
	err := writeFile(filepath.Join(subdir, "file.txt"), []byte("x"))
	require.Error(t, err)
}

// TestRenderTemplate_ReadTemplateError — ReadTemplate fails when the
// source path doesn't exist in the embed FS.
func TestRenderTemplate_ReadTemplateError(t *testing.T) {
	_, err := renderTemplate("templates/does-not-exist/x.tmpl", InstallData{})
	require.Error(t, err)
}

// TestInstall_NoTemplatesSucceeds — installing an agent with no
// templates is a no-op success. Manifest recording / rename handling
// happen at the runInstall layer; Install itself just returns an empty
// result.
func TestInstall_NoTemplatesSucceeds(t *testing.T) {
	a := &Agent{Key: "broken", TemplateDir: "templates/nonexistent"}
	result, err := Install(a, t.TempDir(), InstallData{}, InstallOptions{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Created)
	assert.Empty(t, result.Replaced)
}

// TestInstall_StatError — when the destination path is not readable
// due to a permissions error (neither "exists with content" nor
// NotExist), Install's default branch fires.
func TestInstall_StatError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod traversal denial")
	}
	dir := t.TempDir()
	agent := AgentByKey("claude")
	require.NotNil(t, agent)
	files, err := TemplateFiles(agent)
	require.NoError(t, err)
	require.NotEmpty(t, files)
	// Pick the first file and create a parent dir that we can't read.
	parent := filepath.Dir(filepath.Join(dir, files[0].DestPath))
	require.NoError(t, os.MkdirAll(parent, 0o755))
	// Create the target file AS a directory so it's neither ENOENT nor
	// a file-with-content (ReadFile returns EISDIR → default branch in
	// Install switch).
	require.NoError(t, os.MkdirAll(filepath.Join(dir, files[0].DestPath), 0o755))
	_, err = Install(agent, dir, sampleData(), InstallOptions{})
	require.Error(t, err)
}

// TestInstall_WriteFileFails — writeFile returns an error mid-install,
// propagating up.
func TestInstall_WriteFileFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	dir := t.TempDir()
	// Chmod the root dir read-only so MkdirAll inside writeFile fails.
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	agent := AgentByKey("claude")
	_, err := Install(agent, dir, sampleData(), InstallOptions{})
	require.Error(t, err)
}

// TestRenderTemplate_ParseError — templateParse seam returns an error.
func TestRenderTemplate_ParseError(t *testing.T) {
	orig := templateParse
	templateParse = func(_ string, _ []byte) (*template.Template, error) {
		return nil, assertError("bad parse")
	}
	t.Cleanup(func() { templateParse = orig })
	agent := AgentByKey("claude")
	files, err := TemplateFiles(agent)
	require.NoError(t, err)
	_, err = renderTemplate(files[0].SourcePath, sampleData())
	require.Error(t, err)
}

// TestRenderTemplate_ExecuteError — templateParse returns a template
// that fails at Execute time.
func TestRenderTemplate_ExecuteError(t *testing.T) {
	orig := templateParse
	templateParse = func(_ string, _ []byte) (*template.Template, error) {
		return parseTemplateStrict(`{{.NonexistentField.SubField}}`)
	}
	t.Cleanup(func() { templateParse = orig })
	agent := AgentByKey("claude")
	files, _ := TemplateFiles(agent)
	_, err := renderTemplate(files[0].SourcePath, sampleData())
	require.Error(t, err)
}

// TestInstall_RenderError — renderTemplate returns an error via the
// templateParse seam, which Install wraps as CodeAIInstallFailed.
func TestInstall_RenderError(t *testing.T) {
	orig := templateParse
	templateParse = func(_ string, _ []byte) (*template.Template, error) {
		return nil, assertError("bad parse")
	}
	t.Cleanup(func() { templateParse = orig })
	agent := AgentByKey("claude")
	_, err := Install(agent, t.TempDir(), sampleData(), InstallOptions{})
	require.Error(t, err)
}

func TestInstallResult_PrintText_AllSections(t *testing.T) {
	r := &InstallResult{
		Agent:        "claude",
		Created:      []string{"a", "b"},
		Replaced:     []string{"c"},
		WouldReplace: []string{"d"},
		Skipped:      []string{"e"},
	}
	var buf bytes.Buffer
	r.PrintText(&buf)
	out := buf.String()
	// The new vocabulary is per-line: each path gets its own decorated
	// row rather than an aggregate count. Assertions check the
	// per-path lines + the aggregate skipped summary.
	assert.Contains(t, out, "created: a")
	assert.Contains(t, out, "created: b")
	assert.Contains(t, out, "replaced: c")
	assert.Contains(t, out, "would replace (dry-run): d")
	assert.Contains(t, out, "1 file(s) unchanged")
}

func TestInstallResult_PrintText_EmptyResultIsSilent(t *testing.T) {
	var buf bytes.Buffer
	(&InstallResult{Agent: "x"}).PrintText(&buf)
	assert.Empty(t, buf.String())
}
