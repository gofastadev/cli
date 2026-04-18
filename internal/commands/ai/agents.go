// Package ai implements the `gofasta ai` installer command family. Each
// supported agent (Claude Code, Cursor, OpenAI Codex, Aider, Windsurf)
// has a bundle of configuration files templated under templates/<agent>/;
// running `gofasta ai <agent>` renders those templates into the project
// with the project's module path and name interpolated in.
//
// The installer is intentionally opt-in — shipping every agent's config
// in the scaffold would clutter projects for developers who don't use
// AI agents. Only AGENTS.md (the universal file read by every modern
// agent) is shipped by default; everything else lives behind this
// command.
package ai

import (
	"embed"
	"io/fs"
	"sort"
)

// templatesFS embeds every template file so they're shipped inside the
// gofasta binary. Adding a new agent = adding a directory under
// templates/ and an entry to Agents; no code changes elsewhere.
//
//go:embed all:templates
var templatesFS embed.FS

// Agent describes one supported AI agent and points at the template
// directory it ships. The Key is the command argument (e.g. "claude"
// in `gofasta ai claude`), Name is the human-readable label, and
// TemplateDir is the path inside templatesFS rooted at templates/.
//
// The JSON tags matter — `gofasta --json ai list` emits this struct
// directly, and downstream tooling reads lowercase keys.
type Agent struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TemplateDir string `json:"-"` // implementation detail, not part of the public shape
}

// Agents is the stable registry. Adding a new agent:
//
//  1. Add a new directory under internal/commands/ai/templates/<key>/
//     with .tmpl files rendering whatever configuration that agent reads
//     on startup.
//  2. Append an Agent entry below.
//  3. Done — `gofasta ai <key>` / `gofasta ai list` pick it up automatically.
var Agents = []Agent{
	{
		Key:         "claude",
		Name:        "Claude Code",
		Description: "Anthropic's official CLI coding agent",
		TemplateDir: "templates/claude",
	},
	{
		Key:         "cursor",
		Name:        "Cursor",
		Description: "AI-first IDE with project-level rules and MCP support",
		TemplateDir: "templates/cursor",
	},
	{
		Key:         "codex",
		Name:        "OpenAI Codex",
		Description: "OpenAI's coding agent — reads AGENTS.md by default",
		TemplateDir: "templates/codex",
	},
	{
		Key:         "aider",
		Name:        "Aider",
		Description: "Open-source pair-programming CLI agent",
		TemplateDir: "templates/aider",
	},
	{
		Key:         "windsurf",
		Name:        "Windsurf",
		Description: "Codeium's AI-native IDE",
		TemplateDir: "templates/windsurf",
	},
}

// AgentByKey returns the agent with the given key, or nil if not found.
func AgentByKey(key string) *Agent {
	for i := range Agents {
		if Agents[i].Key == key {
			return &Agents[i]
		}
	}
	return nil
}

// ListKeys returns every registered agent key in sorted order. Used by
// the `gofasta ai list` subcommand.
func ListKeys() []string {
	keys := make([]string, 0, len(Agents))
	for _, a := range Agents {
		keys = append(keys, a.Key)
	}
	sort.Strings(keys)
	return keys
}

// TemplateFiles walks the agent's template directory and returns every
// .tmpl file it contains, each mapped to the destination path it should
// be written to (relative to the project root).
//
// Two path transforms are applied to every entry:
//
//  1. The `.tmpl` suffix is stripped.
//  2. Every path segment that starts with `dot-` is rewritten to start
//     with `.` — the same convention the top-level scaffold uses for
//     `dot-env` → `.env`. Lets us store `.claude/`, `.cursor/`, etc.
//     as on-disk directories that Go's embed FS can see (it otherwise
//     excludes files under dot-prefixed directories) while still
//     producing the right dotfile tree in the project.
func TemplateFiles(a *Agent) ([]TemplateFile, error) {
	var out []TemplateFile
	err := fs.WalkDir(templatesFS, a.TemplateDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := path[len(a.TemplateDir)+1:]
		dst := rel
		if filepathHasSuffix(dst, ".tmpl") {
			dst = dst[:len(dst)-len(".tmpl")]
		}
		dst = undotPrefix(dst)
		out = append(out, TemplateFile{
			SourcePath: path,
			DestPath:   dst,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// undotPrefix rewrites every path segment that starts with "dot-" so it
// starts with "." instead. Used to stage dotfiles in the embedded FS
// without tripping Go's embed rules (embed excludes entries whose name
// begins with `.` unless `all:` is used; even with `all:` some CI
// tooling is unhappy with literal dotdirs in source trees).
//
// Example: "dot-claude/commands/verify.md" → ".claude/commands/verify.md"
func undotPrefix(p string) string {
	segments := []rune{}
	segStart := 0
	result := []byte{}
	// Walk the string byte-by-byte, emitting each segment with the
	// transform applied. Using []byte avoids allocating a []string
	// for every path processed.
	_ = segments // silence "declared and not used" if we don't need the slice
	segStart = 0
	for i := 0; i <= len(p); i++ {
		if i == len(p) || p[i] == '/' {
			seg := p[segStart:i]
			if len(seg) >= 4 && seg[:4] == "dot-" {
				result = append(result, '.')
				result = append(result, seg[4:]...)
			} else {
				result = append(result, seg...)
			}
			if i < len(p) {
				result = append(result, '/')
			}
			segStart = i + 1
		}
	}
	return string(result)
}

// TemplateFile is one rendered artifact — mapping from an embedded
// template source to the on-disk destination path (relative to project
// root).
type TemplateFile struct {
	SourcePath string
	DestPath   string
}

// ReadTemplate returns the raw bytes of an embedded template file.
func ReadTemplate(path string) ([]byte, error) {
	return templatesFS.ReadFile(path)
}

// filepathHasSuffix is a zero-import helper to avoid pulling in path/filepath
// just for one suffix check.
func filepathHasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
