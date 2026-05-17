package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureAgentsMD is the exact title + intro the scaffold's AGENTS.md.tmpl
// ships with — kept verbatim so the docadapt transforms have a known
// input to round-trip against. If the .tmpl changes these phrases, this
// fixture (and the docOriginalTitle / docOriginalIntro constants) must
// change in lockstep — that's the safety net.
const fixtureAgentsMD = `# AGENTS.md — Guidance for AI coding agents

This file tells AI coding agents (Claude Code, OpenAI Codex, Cursor, Aider,
Devin, and other MCP-compatible agents) everything they need to work
productively in this codebase. Agents read it automatically at startup.
Humans onboarding to the project should read it too.

## Project overview

- Name: example
`

func writeFixture(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func TestAdaptDocFileContent_RewritesTitleAndIntroForClaude(t *testing.T) {
	dir := t.TempDir()
	// Simulate the post-rename state: file is named CLAUDE.md but still
	// has the AGENTS.md-shaped body.
	path := writeFixture(t, dir, "CLAUDE.md", fixtureAgentsMD)

	require.NoError(t, AdaptDocFileContent(path, AgentByKey("claude")))

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	s := string(body)

	// Title was swapped to the new filename + agent name.
	require.True(t, strings.HasPrefix(s, "# CLAUDE.md — Guidance for Claude Code"),
		"title not adapted; got prefix %q", firstLine(s))

	// Intro narrowed to Claude Code specifically (no more multi-agent list).
	require.Contains(t, s, "This file tells Claude Code everything it needs")
	require.NotContains(t, s, "OpenAI Codex, Cursor, Aider")

	// Reverse-instructions paragraph mentions the original name and the
	// uninstall command — both so the user knows how to undo.
	require.Contains(t, s, "renamed from AGENTS.md by `gofasta ai claude`")
	require.Contains(t, s, "`gofasta ai uninstall claude`")

	// Body below the intro (the "## Project overview" section) is
	// untouched.
	require.Contains(t, s, "## Project overview")
	require.Contains(t, s, "- Name: example")
}

func TestAdaptDocFileContent_RewritesForAider(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "CONVENTIONS.md", fixtureAgentsMD)

	require.NoError(t, AdaptDocFileContent(path, AgentByKey("aider")))

	body, _ := os.ReadFile(path)
	s := string(body)
	require.True(t, strings.HasPrefix(s, "# CONVENTIONS.md — Guidance for Aider"),
		"aider title not adapted; got prefix %q", firstLine(s))
	require.Contains(t, s, "This file tells Aider")
	require.Contains(t, s, "renamed from AGENTS.md by `gofasta ai aider`")
}

func TestAdaptDocFileContent_NoOpForNativeAgents(t *testing.T) {
	// Agents whose DocFilename is empty (codex, cursor, windsurf) read
	// AGENTS.md natively — no rename happens, so no adaptation should
	// happen either. The function must be a clean no-op.
	for _, key := range []string{"codex", "cursor", "windsurf"} {
		t.Run(key, func(t *testing.T) {
			dir := t.TempDir()
			path := writeFixture(t, dir, "AGENTS.md", fixtureAgentsMD)

			require.NoError(t, AdaptDocFileContent(path, AgentByKey(key)))

			body, _ := os.ReadFile(path)
			require.Equal(t, fixtureAgentsMD, string(body),
				"native-read agent %s must not touch AGENTS.md", key)
		})
	}
}

func TestAdaptDocFileContent_NilAgentNoOp(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "AGENTS.md", fixtureAgentsMD)

	require.NoError(t, AdaptDocFileContent(path, nil))
	body, _ := os.ReadFile(path)
	require.Equal(t, fixtureAgentsMD, string(body))
}

func TestAdaptDocFileContent_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "CLAUDE.md", fixtureAgentsMD)

	require.NoError(t, AdaptDocFileContent(path, AgentByKey("claude")))
	first, _ := os.ReadFile(path)

	require.NoError(t, AdaptDocFileContent(path, AgentByKey("claude")))
	second, _ := os.ReadFile(path)

	require.Equal(t, string(first), string(second),
		"second AdaptDocFileContent call must be a no-op")
}

func TestAdaptDocFileContent_LeavesHandEditedTitleAlone(t *testing.T) {
	// User changed the title to something custom. The adapter must NOT
	// rewrite it (no exact match → no-op), so user edits survive.
	dir := t.TempDir()
	custom := "# my own custom title\n\n## Project overview\n"
	path := writeFixture(t, dir, "CLAUDE.md", custom)

	require.NoError(t, AdaptDocFileContent(path, AgentByKey("claude")))
	body, _ := os.ReadFile(path)
	require.Equal(t, custom, string(body))
}

func TestRestoreDocFileContent_RoundTripWithAdapt(t *testing.T) {
	// adapt → restore must produce a file byte-identical to the
	// original. That's the contract uninstall relies on.
	for _, key := range []string{"claude", "aider"} {
		t.Run(key, func(t *testing.T) {
			dir := t.TempDir()
			agent := AgentByKey(key)
			path := writeFixture(t, dir, agent.DocFilename, fixtureAgentsMD)

			require.NoError(t, AdaptDocFileContent(path, agent))
			require.NoError(t, RestoreDocFileContent(path, agent))

			body, _ := os.ReadFile(path)
			require.Equal(t, fixtureAgentsMD, string(body),
				"adapt → restore must be lossless")
		})
	}
}

func TestRestoreDocFileContent_NoOpForNativeAgents(t *testing.T) {
	for _, key := range []string{"codex", "cursor", "windsurf"} {
		t.Run(key, func(t *testing.T) {
			dir := t.TempDir()
			path := writeFixture(t, dir, "AGENTS.md", fixtureAgentsMD)

			require.NoError(t, RestoreDocFileContent(path, AgentByKey(key)))
			body, _ := os.ReadFile(path)
			require.Equal(t, fixtureAgentsMD, string(body))
		})
	}
}

func TestRestoreDocFileContent_LeavesUnadaptedFileAlone(t *testing.T) {
	// File was never adapted (no matching adapted title). Restore must
	// be a no-op rather than mangling unrelated content.
	dir := t.TempDir()
	custom := "# my own custom title\n\n## Project overview\n"
	path := writeFixture(t, dir, "CLAUDE.md", custom)

	require.NoError(t, RestoreDocFileContent(path, AgentByKey("claude")))
	body, _ := os.ReadFile(path)
	require.Equal(t, custom, string(body))
}

// firstLine returns the first line of s — used so failure messages show
// only the relevant snippet rather than the whole file body.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i != -1 {
		return s[:i]
	}
	return s
}

// TestAdaptDocFileContent_ReadFileError — non-existent file path makes
// os.ReadFile return an error; AdaptDocFileContent surfaces it wrapped
// as CodeFileIO.
func TestAdaptDocFileContent_ReadFileError(t *testing.T) {
	err := AdaptDocFileContent("/nonexistent/path/agents.md", AgentByKey("claude"))
	require.Error(t, err)
}

// TestAdaptDocFileContent_WriteFileError — chmod the FILE read-only
// so os.WriteFile fails after the in-memory transform succeeds.
// (Chmod on the parent dir doesn't help on macOS — existing files
// can still be overwritten if their own perms allow.)
func TestAdaptDocFileContent_WriteFileError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "CLAUDE.md")
	require.NoError(t, os.WriteFile(p, []byte(docOriginalTitle+"\n\n"+docOriginalIntro+"\n"), 0o644))
	require.NoError(t, os.Chmod(p, 0o444))
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })

	err := AdaptDocFileContent(p, AgentByKey("claude"))
	require.Error(t, err)
}

// TestRestoreDocFileContent_ReadFileError — same defensive path on
// the restore side.
func TestRestoreDocFileContent_ReadFileError(t *testing.T) {
	err := RestoreDocFileContent("/nonexistent/path/claude.md", AgentByKey("claude"))
	require.Error(t, err)
}

// TestRestoreDocFileContent_WriteFileError — chmod the file
// read-only after seeding it with adapted content; the in-memory
// restore transform produces a different body but os.WriteFile then
// fails.
func TestRestoreDocFileContent_WriteFileError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "CLAUDE.md")
	agent := AgentByKey("claude")
	adapted := adaptedTitle(agent) + "\n\n" + adaptedIntro(agent) + "\n"
	require.NoError(t, os.WriteFile(p, []byte(adapted), 0o644))
	require.NoError(t, os.Chmod(p, 0o444))
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })

	err := RestoreDocFileContent(p, agent)
	require.Error(t, err)
}

// TestApplyTitle_NoNewline — single-line input with no trailing
// newline: applyTitle's `idx == -1` branch sets idx = len(body) so
// the entire input is treated as the first line.
func TestApplyTitle_NoNewline(t *testing.T) {
	out := applyTitle([]byte(docOriginalTitle), docOriginalTitle, "# Replaced")
	assert.Equal(t, "# Replaced", string(out))
}

// TestApplyTitle_NoNewlineMismatch — single-line input that doesn't
// match `from`: returned unchanged.
func TestApplyTitle_NoNewlineMismatch(t *testing.T) {
	out := applyTitle([]byte("# Something Else"), docOriginalTitle, "# Replaced")
	assert.Equal(t, "# Something Else", string(out))
}
