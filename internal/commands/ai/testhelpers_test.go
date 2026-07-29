package ai

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

// scaffoldFakeProject creates a temporary directory that looks like a
// gofasta project to the ai package's helpers — just a go.mod with a
// module declaration is enough. Chdirs into it for the duration of
// the test so the install path is predictable.
func scaffoldFakeProject(t *testing.T, modulePath string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module "+modulePath+"\n\ngo 1.25.0\n"),
		0o644,
	))
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	return dir
}

// captureStdout redirects os.Stdout for the duration of fn and
// returns whatever was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()
	fn()
	_ = w.Close()
	os.Stdout = orig
	return strings.TrimSpace(<-done)
}

// assertError is a tiny string error used by the seam-based error
// tests that inject custom failures into template parsers and marshal
// calls.
type assertError string

func (e assertError) Error() string { return string(e) }
