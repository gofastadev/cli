package ai

import (
	"bytes"
	"os"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
)

// docadapt.go owns the post-rename content transformations applied to
// the briefing file (AGENTS.md → CLAUDE.md, AGENTS.md → CONVENTIONS.md).
//
// Why content transforms at all: a plain os.Rename leaves CLAUDE.md
// titled "# AGENTS.md — Guidance for AI coding agents" and pitched at a
// generic agent audience. That's incongruent with the new filename and
// with the user who chose a specific agent. The transforms here adapt:
//
//   1. The H1 title line so it matches the renamed file.
//   2. The opening paragraph so it speaks to the installed agent
//      specifically rather than the generic "every modern agent" list.
//
// Both transforms are EXACT-STRING substitutions, not regex rewrites.
// That's deliberate — if the user has edited the original phrasing,
// the substitution simply no-ops and their edits survive. The reverse
// transform (used on uninstall) uses the same exact-match approach, so
// install → uninstall round-trips losslessly even when applied to a
// previously-adapted file.
//
// We never touch the body table on lines 13-26 ("Setting up your
// agent") even though it's somewhat redundant once an agent IS
// installed. Reasoning: the table is also a useful reminder of how
// other agents could be installed if the user wants to add a second
// one later (e.g. install Claude, then also install Aider — neither
// install's adaptation should hide the other agent's bootstrapping
// instructions). Keeping the table avoids that footgun.

// The exact strings we expect in the scaffolded AGENTS.md. If a future
// AGENTS.md.tmpl edit changes these phrases, the substitution silently
// no-ops — surface that via the docadapt test, not by panicking at
// install time.
const (
	docOriginalTitle = "# AGENTS.md — Guidance for AI coding agents"
	docOriginalIntro = "This file tells AI coding agents (Claude Code, OpenAI Codex, Cursor, Aider,\n" +
		"Devin, and other MCP-compatible agents) everything they need to work\n" +
		"productively in this codebase. Agents read it automatically at startup.\n" +
		"Humans onboarding to the project should read it too."
)

// AdaptDocFileContent rewrites the renamed briefing file at path so its
// title + intro paragraph reflect the installed agent. Idempotent: a
// second call with the same agent is a no-op. Safe on hand-edited
// files: any phrase that no longer matches the original is left alone.
//
// Returns CodeFileIO on read/write failures. Never errors when the
// adaptation simply finds nothing to change.
func AdaptDocFileContent(path string, agent *Agent) error {
	if agent == nil || agent.DocFilename == "" {
		return nil // nothing to do — agent reads AGENTS.md natively
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return clierr.Wrap(clierr.CodeFileIO, err, "read "+path)
	}
	out := applyTitle(body, docOriginalTitle, adaptedTitle(agent))
	out = applyIntro(out, docOriginalIntro, adaptedIntro(agent))
	if bytes.Equal(out, body) {
		return nil
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return clierr.Wrap(clierr.CodeFileIO, err, "write "+path)
	}
	return nil
}

// RestoreDocFileContent is the inverse for uninstall. Takes the same
// agent the install used, restores the original title and intro. Same
// safety guarantees as AdaptDocFileContent (idempotent, no-op on
// hand-edited content that no longer matches).
func RestoreDocFileContent(path string, agent *Agent) error {
	if agent == nil || agent.DocFilename == "" {
		return nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return clierr.Wrap(clierr.CodeFileIO, err, "read "+path)
	}
	out := applyTitle(body, adaptedTitle(agent), docOriginalTitle)
	out = applyIntro(out, adaptedIntro(agent), docOriginalIntro)
	if bytes.Equal(out, body) {
		return nil
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return clierr.Wrap(clierr.CodeFileIO, err, "write "+path)
	}
	return nil
}

// adaptedTitle returns the H1 line for the installed agent. Example:
// "# CLAUDE.md — Guidance for Claude Code".
func adaptedTitle(agent *Agent) string {
	return "# " + agent.DocFilename + " — Guidance for " + agent.Name
}

// adaptedIntro returns the opening paragraph rewritten for the installed
// agent. Single-agent wording replaces the multi-agent list, and the
// "Agents read it" sentence becomes specific. Mentioning the original
// AGENTS.md name preserves the "this used to be AGENTS.md" context so a
// user who follows external docs (e.g. agent vendor docs that say "drop
// AGENTS.md at your repo root") can still find the right file.
func adaptedIntro(agent *Agent) string {
	return "This file tells " + agent.Name + " everything it needs to work\n" +
		"productively in this codebase. " + agent.Name + " reads it automatically\n" +
		"at session start. Humans onboarding to the project should read it too.\n" +
		"\n" +
		"This file was renamed from AGENTS.md by `gofasta ai " + agent.Key + "`. Run\n" +
		"`gofasta ai uninstall " + agent.Key + "` to rename it back."
}

// applyTitle swaps the H1 line if (and only if) it matches the expected
// `from` exactly. Returns the input unchanged otherwise.
func applyTitle(body []byte, from, to string) []byte {
	// Only act on the first line — H1 must be at the top of the file
	// per markdown convention. Restricting the scope means we don't
	// accidentally rewrite "# AGENTS.md" if it appears in a code block
	// later in the file.
	idx := bytes.IndexByte(body, '\n')
	if idx == -1 {
		idx = len(body)
	}
	first := string(body[:idx])
	if first != from {
		return body
	}
	return append([]byte(to), body[idx:]...)
}

// applyIntro swaps the opening paragraph if it matches `from` exactly.
// We use strings.Replace with N=1 so only the first occurrence is
// touched, leaving any later mentions of the original phrasing
// (unlikely but defensible) alone.
func applyIntro(body []byte, from, to string) []byte {
	if !strings.Contains(string(body), from) {
		return body
	}
	return []byte(strings.Replace(string(body), from, to, 1))
}
