package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenMiddleware_FindRouteFileError — RoutesDir doesn't exist.
func TestGenMiddleware_FindRouteFileError(t *testing.T) {
	chdirTest(t, t.TempDir())
	err := GenMiddleware(MiddlewareData{
		HTTPMethod: "POST", Path: "/x", Middleware: "auth",
		RoutesDir: "nope",
	})
	require.Error(t, err)
}

// TestGenMiddleware_ReadFileError — RoutesFile exists in find but
// disappears between find and read. We can simulate by overriding
// RoutesFile to point at the target then removing it after find.
//
// Simpler: setup happy path, then chmod the routes file to deny read
// after find succeeds — though find also reads. So just point at a
// non-existent file via RoutesFile.
func TestGenMiddleware_RoutesFileExplicitMissing(t *testing.T) {
	chdirTest(t, t.TempDir())
	err := GenMiddleware(MiddlewareData{
		HTTPMethod: "POST", Path: "/x", Middleware: "auth",
		RoutesFile: "/nope/missing.routes.go",
	})
	require.Error(t, err)
}

// TestFindRouteFile_NonRoutesFileSkipped — a non-`.routes.go` file in
// the dir exercises the suffix-filter continue.
func TestFindRouteFile_NonRoutesFileSkipped(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "app", "rest", "routes", "order.routes.go"),
		`package routes
func OrderRoutes(r interface{}) { r.Get("/orders", nil) }`)
	mustWriteFile(t, filepath.Join(tmp, "app", "rest", "routes", "README.md"), "notes")
	chdirTest(t, tmp)
	_, hit, err := findRouteFile(MiddlewareData{
		HTTPMethod: "GET", Path: "/orders",
		RoutesDir: filepath.Join("app", "rest", "routes"),
	})
	require.NoError(t, err)
	require.True(t, hit)
}

// TestFindRouteFile_UnreadableFileIsContinued — chmod the routes file
// to deny read, exercise the continue-on-read-error branch.
func TestFindRouteFile_UnreadableFileIsContinued(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod")
	}
	tmp := t.TempDir()
	routesDir := filepath.Join(tmp, "app", "rest", "routes")
	require.NoError(t, os.MkdirAll(routesDir, 0o755))
	bad := filepath.Join(routesDir, "bad.routes.go")
	good := filepath.Join(routesDir, "order.routes.go")
	require.NoError(t, os.WriteFile(bad, []byte("readme"), 0o644))
	require.NoError(t, os.Chmod(bad, 0o000))
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })
	require.NoError(t, os.WriteFile(good,
		[]byte(`package routes
func OrderRoutes(r interface{}) { r.Get("/orders", nil) }`), 0o644))
	chdirTest(t, tmp)
	_, _, err := findRouteFile(MiddlewareData{
		HTTPMethod: "GET", Path: "/orders", RoutesDir: routesDir,
	})
	require.NoError(t, err)
}

// TestRegexpMatchEquals_LengthMismatch — exercises the early-return
// when lengths differ.
func TestRegexpMatchEquals_LengthMismatch(t *testing.T) {
	require.False(t, regexpMatchEquals([]byte("ab"), []byte("abc")))
}

// TestRegexpMatchEquals_ContentMismatch — same length, different byte.
func TestRegexpMatchEquals_ContentMismatch(t *testing.T) {
	require.False(t, regexpMatchEquals([]byte("abc"), []byte("abd")))
}
