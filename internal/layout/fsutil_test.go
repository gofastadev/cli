// fsutil_test.go — coverage for the filesystem-dependent half of the layout
// package. Unlike the pure path builders in layout_test.go, these functions
// answer questions about a project on disk, so each test builds the smallest
// tree that makes the answer meaningful and runs from inside it.

package layout

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
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

// --- featureResourceDirs ---

// TestFeatureResourceDirs_ExcludesSharedConcerns is the crux of the feature
// layout's discovery: app/ holds both resource directories and cross-cutting
// ones, and only the former describe a resource. Treating app/di or app/models
// as a resource would make `g mock --all` generate mocks for infrastructure.
func TestFeatureResourceDirs_ExcludesSharedConcerns(t *testing.T) {
	inProjectTree(t, map[string]string{
		"app/user/user.go":   "package user",
		"app/order/order.go": "package order",
		// Every shared concern must be filtered out.
		"app/di/container.go":        "package di",
		"app/jobs/jobs.go":           "package jobs",
		"app/tasks/tasks.go":         "package tasks",
		"app/graphql/schema.go":      "package graphql",
		"app/shared/shared.go":       "package shared",
		"app/validators/validate.go": "package validators",
		"app/rest/routes/index.go":   "package routes",
		"app/models/user.model.go":   "package models",
		"app/devtools/tool.go":       "package devtools",
		// A stray file directly under app/ is not a directory and is skipped.
		"app/README.md": "notes",
	})

	assert.ElementsMatch(t, []string{
		filepath.Join("app", "order"),
		filepath.Join("app", "user"),
	}, featureResourceDirs())
}

func TestFeatureResourceDirs_NoAppDirectory(t *testing.T) {
	inProjectTree(t, map[string]string{"go.mod": "module example.com/app\n"})
	assert.Nil(t, featureResourceDirs(), "a missing app/ is an empty result, not a failure")
}

// --- dirHasSuffixFile ---

func TestDirHasSuffixFile(t *testing.T) {
	inProjectTree(t, map[string]string{
		"withiface/repository_iface.go": "package withiface",
		"withiface/other.go":            "package withiface",
		"noiface/service.go":            "package noiface",
		// A test file must not count: a directory whose only _iface.go is a
		// test fixture has no interface to mock.
		"onlytest/repository_iface_test.go": "package onlytest",
		"nested/sub/":                       "",
	})

	assert.True(t, dirHasSuffixFile("withiface", "_iface.go"))
	assert.False(t, dirHasSuffixFile("noiface", "_iface.go"))
	assert.False(t, dirHasSuffixFile("onlytest", "_iface.go"),
		"a _test.go file must not be mistaken for a real interface file")
	assert.False(t, dirHasSuffixFile("nested", "_iface.go"),
		"subdirectories are not scanned")
	assert.False(t, dirHasSuffixFile("does-not-exist", "_iface.go"))

	// The suffix is a real parameter, not a constant baked into the helper:
	// production only asks for "_iface.go" today, but the matching logic has
	// to work for any suffix a future layout query needs.
	assert.True(t, dirHasSuffixFile("withiface", ".go"))
	assert.True(t, dirHasSuffixFile("noiface", "service.go"))
	assert.False(t, dirHasSuffixFile("noiface", ".routes.go"))
}

// --- globRouteFiles ---

func TestGlobRouteFiles(t *testing.T) {
	inProjectTree(t, map[string]string{
		"routes/user.routes.go":        "package routes",
		"routes/order.routes.go":       "package routes",
		"routes/index.go":              "package routes",
		"routes/nested/deep.routes.go": "package nested",
	})

	assert.ElementsMatch(t, []string{
		filepath.Join("routes", "order.routes.go"),
		filepath.Join("routes", "user.routes.go"),
	}, globRouteFiles("routes"), "only *.routes.go directly under dir")

	assert.Nil(t, globRouteFiles("missing"))
}

// --- fileExists ---

func TestFileExists(t *testing.T) {
	inProjectTree(t, map[string]string{
		"real.go": "package main",
		"a/dir/":  "",
	})

	assert.True(t, fileExists("real.go"))
	assert.False(t, fileExists("a/dir"), "a directory is not a file")
	assert.False(t, fileExists("nope.go"))
}

// --- feature layout, filesystem-backed ---

func TestFeature_SingleFiles(t *testing.T) {
	lo := For(Feature)
	cases := []struct{ name, got, want string }{
		{"ContainerFile", lo.ContainerFile(), "app/di/container.go"},
		{"WireFile", lo.WireFile(), "app/di/wire.go"},
		{"RouteIndexFile", lo.RouteIndexFile(), "app/rest/routes/index.routes.go"},
		{"ServeFile", lo.ServeFile(), "cmd/serve.go"},
		{"ResolverFile", lo.ResolverFile(), "app/graphql/resolvers/resolver.go"},
		{"RoutesDir", lo.RoutesDir(), filepath.Join("app", "rest", "routes")},
		{"MigrationsDir", lo.MigrationsDir(), filepath.Join("db", "migrations")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, c.got)
		})
	}
}

func TestFeature_InterfaceDirs(t *testing.T) {
	inProjectTree(t, map[string]string{
		"app/user/repository_iface.go": "package user",
		"app/order/service_iface.go":   "package order",
		// No _iface.go: nothing to mock, so it must not be returned.
		"app/report/report.go": "package report",
		// Shared concerns are excluded even when they declare an iface file.
		"app/shared/thing_iface.go": "package shared",
	})

	assert.ElementsMatch(t, []string{
		filepath.Join("app", "order"),
		filepath.Join("app", "user"),
	}, For(Feature).InterfaceDirs())
}

func TestFeature_InterfaceDirs_EmptyProject(t *testing.T) {
	inProjectTree(t, map[string]string{"go.mod": "module example.com/app\n"})
	assert.Empty(t, For(Feature).InterfaceDirs())
}

// TestFeature_RouteFiles covers the two-source listing: the shared index plus
// each resource's own routes.go, which is where the per-resource registrations
// live once a project has been converted to the feature layout.
func TestFeature_RouteFiles(t *testing.T) {
	inProjectTree(t, map[string]string{
		"app/rest/routes/index.routes.go": "package routes",
		"app/user/routes.go":              "package user",
		"app/order/routes.go":             "package order",
		// A resource without routes contributes nothing.
		"app/report/report.go": "package report",
	})

	assert.ElementsMatch(t, []string{
		filepath.Join("app", "rest", "routes", "index.routes.go"),
		filepath.Join("app", "order", "routes.go"),
		filepath.Join("app", "user", "routes.go"),
	}, For(Feature).RouteFiles())
}

func TestFeature_RouteFiles_NoIndex(t *testing.T) {
	inProjectTree(t, map[string]string{"app/user/routes.go": "package user"})
	assert.Equal(t, []string{filepath.Join("app", "user", "routes.go")}, For(Feature).RouteFiles())
}

func TestFeature_RouteFiles_EmptyProject(t *testing.T) {
	inProjectTree(t, map[string]string{"go.mod": "module example.com/app\n"})
	assert.Empty(t, For(Feature).RouteFiles())
}

// --- layered layout, filesystem-backed ---

func TestLayered_RouteFiles(t *testing.T) {
	inProjectTree(t, map[string]string{
		"app/rest/routes/user.routes.go":  "package routes",
		"app/rest/routes/index.routes.go": "package routes",
		"app/rest/routes/helpers.go":      "package routes",
	})

	assert.ElementsMatch(t, []string{
		filepath.Join("app", "rest", "routes", "index.routes.go"),
		filepath.Join("app", "rest", "routes", "user.routes.go"),
	}, For(Layered).RouteFiles())
}

func TestLayered_RouteFiles_MissingDir(t *testing.T) {
	inProjectTree(t, map[string]string{"go.mod": "module example.com/app\n"})
	assert.Nil(t, For(Layered).RouteFiles())
}

// --- Detect ---

// TestDetect_ConfigIsAuthoritative pins the priority order: config.yaml wins
// over whatever the filesystem looks like, because `gofasta new` writes the
// key at scaffold time and that value is the project's declared intent.
func TestDetect_ConfigIsAuthoritative(t *testing.T) {
	cases := map[string]struct {
		config string
		want   Kind
	}{
		"feature": {"project:\n  layout: feature\n", Feature},
		"layered": {"project:\n  layout: layered\n", Layered},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			inProjectTree(t, map[string]string{
				"config.yaml": tc.config,
				// A layered-looking tree that must not override the config.
				"app/models/user.model.go": "package models",
			})
			assert.Equal(t, tc.want, Detect().Kind())
		})
	}
}

// TestDetect_FallsBackToLayered covers the no-config path. Both branches
// resolve to Layered on purpose — there is only a positive signal for layered
// (app/models/), and an unrecognized shape must not route to a layout that
// would not produce a working project.
func TestDetect_FallsBackToLayered(t *testing.T) {
	t.Run("layered signal present", func(t *testing.T) {
		inProjectTree(t, map[string]string{"app/models/user.model.go": "package models"})
		assert.Equal(t, Layered, Detect().Kind())
	})

	t.Run("no signal at all", func(t *testing.T) {
		inProjectTree(t, map[string]string{"go.mod": "module example.com/app\n"})
		assert.Equal(t, Layered, Detect().Kind())
	})
}
