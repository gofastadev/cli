package generate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/require"
)

func setupMiddlewareProject(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))

	mustWriteFile(t, filepath.Join(tmp, "app", "rest", "routes", "order.routes.go"), `package routes

import "github.com/go-chi/chi/v5"

func OrderRoutes(r chi.Router, c interface{}) {
	r.Get("/orders", nil)
	r.Post("/orders/{id}/archive", nil)
}
`)
	return tmp
}

func TestGenMiddleware_WrapsBareRoute(t *testing.T) {
	tmp := setupMiddlewareProject(t)
	chdirTest(t, tmp)

	require.NoError(t, GenMiddleware(MiddlewareData{
		HTTPMethod: "POST",
		Path:       "/orders/{id}/archive",
		Middleware: `auth.RequireRole("admin")`,
	}))

	body, _ := os.ReadFile(filepath.Join(tmp, "app", "rest", "routes", "order.routes.go"))
	s := string(body)
	require.Contains(t, s, `r.With(auth.RequireRole("admin")).Post("/orders/{id}/archive"`)
	// Bare route line should be gone.
	require.NotContains(t, s, `r.Post("/orders/{id}/archive", nil)`)
}

func TestGenMiddleware_ChainsAdditionalMiddleware(t *testing.T) {
	tmp := setupMiddlewareProject(t)
	chdirTest(t, tmp)

	// Add first middleware.
	require.NoError(t, GenMiddleware(MiddlewareData{
		HTTPMethod: "POST",
		Path:       "/orders/{id}/archive",
		Middleware: "middleware.Logger",
	}))
	// Add second — must append into the same With(...) call.
	require.NoError(t, GenMiddleware(MiddlewareData{
		HTTPMethod: "POST",
		Path:       "/orders/{id}/archive",
		Middleware: `auth.RequireRole("admin")`,
	}))

	body, _ := os.ReadFile(filepath.Join(tmp, "app", "rest", "routes", "order.routes.go"))
	require.Contains(t, string(body),
		`r.With(middleware.Logger, auth.RequireRole("admin")).Post("/orders/{id}/archive"`)
}

func TestGenMiddleware_IdempotentSameMiddleware(t *testing.T) {
	tmp := setupMiddlewareProject(t)
	chdirTest(t, tmp)

	require.NoError(t, GenMiddleware(MiddlewareData{
		HTTPMethod: "POST",
		Path:       "/orders/{id}/archive",
		Middleware: "middleware.Logger",
	}))
	beforeBody, _ := os.ReadFile(filepath.Join(tmp, "app", "rest", "routes", "order.routes.go"))

	// Re-running with the same middleware should not duplicate it.
	require.NoError(t, GenMiddleware(MiddlewareData{
		HTTPMethod: "POST",
		Path:       "/orders/{id}/archive",
		Middleware: "middleware.Logger",
	}))
	afterBody, _ := os.ReadFile(filepath.Join(tmp, "app", "rest", "routes", "order.routes.go"))

	require.Equal(t, string(beforeBody), string(afterBody),
		"second add of same middleware must be a no-op")
}

func TestGenMiddleware_ValidationErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    MiddlewareData
	}{
		{"empty-method", MiddlewareData{Path: "/x", Middleware: "m"}},
		{"empty-path", MiddlewareData{HTTPMethod: "GET", Middleware: "m"}},
		{"empty-mw", MiddlewareData{HTTPMethod: "GET", Path: "/x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := GenMiddleware(tc.d)
			require.Error(t, err)
			var ce *clierr.Error
			require.True(t, errors.As(err, &ce))
			require.Equal(t, string(clierr.CodeInvalidName), ce.Code)
		})
	}
}

func TestGenMiddleware_NoMatchingRouteReturnsClierr(t *testing.T) {
	tmp := setupMiddlewareProject(t)
	chdirTest(t, tmp)

	err := GenMiddleware(MiddlewareData{
		HTTPMethod: "DELETE",
		Path:       "/nope",
		Middleware: "m",
	})
	require.Error(t, err)
}

func TestContainsMiddleware(t *testing.T) {
	require.True(t, containsMiddleware("a, b, c", "b"))
	require.True(t, containsMiddleware(" auth.X(), middleware.Y ", "middleware.Y"))
	require.False(t, containsMiddleware("a, b", "c"))
	require.False(t, containsMiddleware("", "b"))
}

func TestWrapRouteWithMiddleware_NoOpWhenAlreadyPresent(t *testing.T) {
	body := []byte(`r.With(mw).Post("/x", nil)`)
	patched, applied := wrapRouteWithMiddleware(body, "POST", "/x", "mw")
	require.False(t, applied)
	require.Equal(t, string(body), string(patched))
}

func TestRegexpReplaceEscape(t *testing.T) {
	require.Equal(t, "a$$1b", regexpReplaceEscape("a$1b"))
	require.Equal(t, "plain", regexpReplaceEscape("plain"))
}

// findRouteFile-via-explicit-RoutesFile path coverage.
func TestFindRouteFile_ExplicitFile(t *testing.T) {
	tmp := setupMiddlewareProject(t)
	chdirTest(t, tmp)

	path, hit, err := findRouteFile(MiddlewareData{
		HTTPMethod: "POST",
		Path:       "/orders/{id}/archive",
		RoutesFile: filepath.Join("app", "rest", "routes", "order.routes.go"),
	})
	require.NoError(t, err)
	require.True(t, hit)
	require.True(t, strings.HasSuffix(path, "order.routes.go"))
}

func TestFindRouteFile_MissingExplicitFile(t *testing.T) {
	chdirTest(t, t.TempDir())
	_, _, err := findRouteFile(MiddlewareData{
		HTTPMethod: "GET", Path: "/x",
		RoutesFile: "missing.go",
	})
	require.Error(t, err)
}
