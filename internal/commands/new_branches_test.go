package commands

import (
	"encoding/json"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// new.go — JSON mode deferred result emit
// ─────────────────────────────────────────────────────────────────────

// TestRunNew_JSON_EmitsResultOnEarlyReturn — JSON mode emits a single
// newResult document even on the early-return path (directory already
// exists). Verifies that the deferred restore-stdout + Print runs
// regardless of where runNew exits.
func TestRunNew_JSON_EmitsResultOnEarlyReturn(t *testing.T) {
	chdirTemp(t)
	withJSONMode(t)

	projectFSOverride = fstest.MapFS{
		"project":       {Mode: fs.ModeDir},
		"project/a.txt": {Data: []byte("x")},
	}
	t.Cleanup(func() { projectFSOverride = nil })

	// Pre-create the target dir so runNew bails at the existence check.
	require.NoError(t, os.MkdirAll("collision-app", 0o755))

	out := captureStdout(t, func() {
		err := runNew("collision-app", false)
		require.Error(t, err)
	})

	var got newResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "new", got.Action)
	assert.Equal(t, "collision-app", got.Project)
	assert.False(t, got.Success)
	assert.Contains(t, got.Error, "already exists")
}
