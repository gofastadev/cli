package ai

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentByKey_ReturnsKnownAgent(t *testing.T) {
	a := AgentByKey("claude")
	require.NotNil(t, a)
	assert.Equal(t, "Claude Code", a.Name)
}

func TestAgentByKey_NilForUnknown(t *testing.T) {
	assert.Nil(t, AgentByKey("nonexistent-agent"))
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

// TestTemplateFiles_EmptyAgent — an agent pointing at a nonexistent
// directory returns an empty slice with no error.
func TestTemplateFiles_EmptyAgent(t *testing.T) {
	files, err := TemplateFiles(&Agent{TemplateDir: "templates/claude"})
	require.NoError(t, err)
	assert.NotEmpty(t, files)
}

// TestTemplateFiles_NoTemplatesIsNotAnError — an agent that ships no
// template files (cursor, windsurf — they read AGENTS.md natively)
// returns an empty slice and a nil error. This is the contract the
// Install path relies on for those agents.
func TestTemplateFiles_NoTemplatesIsNotAnError(t *testing.T) {
	a := &Agent{Key: "x", TemplateDir: "templates/nonexistent"}
	files, err := TemplateFiles(a)
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestUndotPrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"dot-claude/settings.json", ".claude/settings.json"},
		{"dot-claude/hooks/pre-commit.sh", ".claude/hooks/pre-commit.sh"},
		{"dot-cursor/rules/gofasta.mdc", ".cursor/rules/gofasta.mdc"},
		{"dot-windsurfrules", ".windsurfrules"},
		{"dot-aider.conf.yml", ".aider.conf.yml"},
		{"dot-aider/CONVENTIONS.md", ".aider/CONVENTIONS.md"},
		// Any segment starting with "dot-" is transformed regardless of
		// depth — the convention is symmetric at every directory level.
		{"configs/dot-this/file", "configs/.this/file"},
		// "dot-" appearing mid-segment is NOT a prefix, so it stays.
		{"this-is-not-dot-", "this-is-not-dot-"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := undotPrefix(tc.in)
			if got != tc.want {
				t.Errorf("undotPrefix(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestUndotPrefix_EdgeCases — additional edge cases collected while
// reviewing the transform.
func TestUndotPrefix_EdgeCases(t *testing.T) {
	assert.Equal(t, "", undotPrefix(""))
	assert.Equal(t, ".config", undotPrefix("dot-config"))
	assert.Equal(t, ".config/x", undotPrefix("dot-config/x"))
	assert.Equal(t, "normal/x", undotPrefix("normal/x"))
}
