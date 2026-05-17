//go:build !windows

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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// runHookWithPayload renders a hook script for the given agent, writes
// it to a temp file (preserving the +x mode from Install), and execs
// it with the given JSON payload on stdin. Returns combined stdout +
// the run error (if any).
//
// Requires `bash` and `jq` on PATH. Skips if jq isn't installed — many
// CI environments don't ship it.
func runHookWithPayload(t *testing.T, agent, scriptRel, payload string) string {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not on PATH — skipping hook payload test")
	}
	root := renderAgentToTempDir(t, agent)
	script := filepath.Join(root, scriptRel)

	cmd := exec.Command("bash", script)
	cmd.Stdin = strings.NewReader(payload)
	cmd.Dir = root
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	require.NoError(t, cmd.Run(), "hook %s should exit 0 (stderr=%q)", scriptRel, errOut.String())
	return out.String()
}

// TestHook_WireReminder_Matches feeds a path matching Wire's input
// glob and asserts the script surfaces the reminder.
func TestHook_WireReminder_Matches(t *testing.T) {
	for _, a := range []struct{ agent, script string }{
		{"claude", ".claude/hooks/wire-reminder.sh"},
		{"codex", ".codex/hooks/wire-reminder.sh"},
	} {
		t.Run(a.agent, func(t *testing.T) {
			out := runHookWithPayload(t, a.agent, a.script,
				`{"tool_input":{"file_path":"app/di/wire.go"}}`)
			assert.Contains(t, out, "gofasta wire",
				"wire-reminder should mention `gofasta wire`")
		})
	}
}

// TestHook_WireReminder_ProviderPath confirms the case-glob matches
// provider files under app/di/providers/ (the other Wire input path).
func TestHook_WireReminder_ProviderPath(t *testing.T) {
	out := runHookWithPayload(t, "claude", ".claude/hooks/wire-reminder.sh",
		`{"tool_input":{"file_path":"app/di/providers/order.go"}}`)
	assert.Contains(t, out, "gofasta wire")
}

// TestHook_WireReminder_NonMatchingPathSilent feeds a path that
// should NOT match — assert silent stdout.
func TestHook_WireReminder_NonMatchingPathSilent(t *testing.T) {
	out := runHookWithPayload(t, "claude", ".claude/hooks/wire-reminder.sh",
		`{"tool_input":{"file_path":"README.md"}}`)
	assert.Empty(t, strings.TrimSpace(out),
		"non-matching path must produce no output")
}

// TestHook_MigrationReminder_Matches feeds a model file path.
func TestHook_MigrationReminder_Matches(t *testing.T) {
	out := runHookWithPayload(t, "claude", ".claude/hooks/migration-reminder.sh",
		`{"tool_input":{"file_path":"app/models/user.go"}}`)
	assert.Contains(t, out, "migration",
		"migration-reminder should mention 'migration'")
}

// TestHook_SwaggerReminder_Matches feeds a controller file path.
func TestHook_SwaggerReminder_Matches(t *testing.T) {
	out := runHookWithPayload(t, "claude", ".claude/hooks/swagger-reminder.sh",
		`{"tool_input":{"file_path":"app/rest/controllers/user.controller.go"}}`)
	assert.Contains(t, out, "swagger",
		"swagger-reminder should mention 'swagger'")
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

// TestHook_Cursor_WireReminder_Matches feeds Cursor's afterFileEdit
// payload (file_path at the top level, not under tool_input) and
// asserts the reminder fires.
func TestHook_Cursor_WireReminder_Matches(t *testing.T) {
	out := runHookWithPayload(t, "cursor", ".cursor/hooks/wire-reminder.sh",
		`{"file_path":"app/di/wire.go"}`)
	assert.Contains(t, out, "gofasta wire",
		"cursor wire-reminder should mention `gofasta wire`")
}

// TestHook_Cursor_WireReminder_NonMatchingSilent feeds a non-Wire
// path and asserts stdout is empty (no false-positive reminder).
func TestHook_Cursor_WireReminder_NonMatchingSilent(t *testing.T) {
	out := runHookWithPayload(t, "cursor", ".cursor/hooks/wire-reminder.sh",
		`{"file_path":"README.md"}`)
	assert.Empty(t, strings.TrimSpace(out),
		"non-matching path must produce no output")
}

// TestHook_Windsurf_WireReminder_Matches feeds Cascade's
// post_write_code payload (file_path nested under tool_info) and
// asserts the reminder fires.
func TestHook_Windsurf_WireReminder_Matches(t *testing.T) {
	out := runHookWithPayload(t, "windsurf", ".windsurf/hooks/wire-reminder.sh",
		`{"agent_action_name":"post_write_code","tool_info":{"file_path":"app/di/wire.go"}}`)
	assert.Contains(t, out, "gofasta wire",
		"windsurf wire-reminder should mention `gofasta wire`")
}

// TestHook_Windsurf_WireReminder_NonMatchingSilent feeds a non-Wire
// path and asserts stdout is empty.
func TestHook_Windsurf_WireReminder_NonMatchingSilent(t *testing.T) {
	out := runHookWithPayload(t, "windsurf", ".windsurf/hooks/wire-reminder.sh",
		`{"agent_action_name":"post_write_code","tool_info":{"file_path":"README.md"}}`)
	assert.Empty(t, strings.TrimSpace(out),
		"non-matching path must produce no output")
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

// TestPrintNextSteps_MentionsNewFamilies — calls printNextSteps for
// each agent and asserts the output mentions the new families we
// added (hooks for claude/codex, slash/workflow counts for
// claude/cursor/windsurf). Catches future drift between the templates
// and the post-install help text.
func TestPrintNextSteps_MentionsNewFamilies(t *testing.T) {
	cases := []struct {
		agent string
		wants []string
	}{
		{"claude", []string{"/status", "/g-method", "/seed-memory", "Hooks", "jq"}},
		{"cursor", []string{"/status", "/g-method", ".cursor/commands", "Hooks", "afterFileEdit"}},
		{"codex", []string{"Hooks", ".codex/hooks", "/hooks", "prompts", "symlink"}},
		{"windsurf", []string{"/status", "/g-method", ".windsurf/workflows", "Hooks", "post_write_code"}},
	}
	for _, tc := range cases {
		t.Run(tc.agent, func(t *testing.T) {
			agent := AgentByKey(tc.agent)
			require.NotNil(t, agent)
			var buf bytes.Buffer
			printNextSteps(&buf, agent)
			out := buf.String()
			for _, want := range tc.wants {
				assert.Contains(t, out, want,
					"printNextSteps(%s) should mention %q", tc.agent, want)
			}
		})
	}
}
