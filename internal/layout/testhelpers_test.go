// fsutil_test.go — coverage for the filesystem-dependent half of the layout
// package. Unlike the pure path builders in layout_test.go, these functions
// answer questions about a project on disk, so each test builds the smallest
// tree that makes the answer meaningful and runs from inside it.

package layout

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// inProjectTree chdirs into a temp directory populated with the given files.
// A file's parent directories are created automatically; an entry ending in
// "/" creates an empty directory.
func inProjectTree(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })

	for path, content := range files {
		full := filepath.Join(dir, path)
		if path[len(path)-1] == '/' {
			require.NoError(t, os.MkdirAll(full, 0o755))
			continue
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
}
