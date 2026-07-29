package commands

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoutesCmd_RunE(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.MkdirAll("app/rest/routes", 0755))
	assert.NoError(t, routesCmd.RunE(routesCmd, nil))
}

func TestRunRoutes_SampleProject(t *testing.T) {
	chdirTemp(t)
	routesDir := "app/rest/routes"
	require.NoError(t, os.MkdirAll(routesDir, 0755))

	// Index file includes a chi r.Mount("...", ...) call so the
	// prefix extraction regex matches and apiPrefix gets set to "/api/v1".
	index := `package routes
func InitApi(r *chi.Mux) {
	api := chi.NewRouter()
	r.Get("/health", httputil.Handle(c.Ok))
	r.Handle("/swagger/*", httpSwagger.WrapHandler)
	r.Mount("/api/v1", api)
}`
	require.NoError(t, os.WriteFile(routesDir+"/index.routes.go", []byte(index), 0644))

	user := `package routes
func UserRoutes(r chi.Router) {
	r.Get("/users", httputil.Handle(c.List))
	r.Get("/users/{id}", httputil.Handle(c.Get))
}`
	require.NoError(t, os.WriteFile(routesDir+"/user.routes.go", []byte(user), 0644))

	assert.NoError(t, runRoutes())
}

func TestRunRoutes_Empty(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.MkdirAll("app/rest/routes", 0755))
	assert.NoError(t, runRoutes())
}

func TestRunRoutes_NoIndexFile(t *testing.T) {
	chdirTemp(t)
	routesDir := "app/rest/routes"
	require.NoError(t, os.MkdirAll(routesDir, 0755))
	// Only a non-index file — apiPrefix will stay empty
	user := `r.Get("/a", x)`
	require.NoError(t, os.WriteFile(routesDir+"/a.routes.go", []byte(user), 0644))
	assert.NoError(t, runRoutes())
}

func TestRunRoutes_ReadDirError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod-based access denial")
	}
	chdirTemp(t)
	routesDir := "app/rest/routes"
	require.NoError(t, os.MkdirAll(routesDir, 0755))
	// os.Stat passes (dir exists), but drop read permission so ReadDir fails.
	require.NoError(t, os.Chmod(routesDir, 0o111))
	t.Cleanup(func() { _ = os.Chmod(routesDir, 0o755) })

	err := runRoutes()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read routes directory")
}

func TestRunRoutes_UnreadableRouteFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod-based access denial")
	}
	chdirTemp(t)
	routesDir := "app/rest/routes"
	require.NoError(t, os.MkdirAll(routesDir, 0755))
	// Write a valid route file, then revoke read permission. ReadDir can
	// list the file (only needs execute on the parent dir), but ReadFile
	// fails with EACCES. The file should be silently skipped.
	require.NoError(t, os.WriteFile(routesDir+"/blocked.routes.go",
		[]byte(`r.Get("/x", h)`), 0o000))
	t.Cleanup(func() { _ = os.Chmod(routesDir+"/blocked.routes.go", 0o644) })

	// Should not error — unreadable files are skipped (continue branch).
	assert.NoError(t, runRoutes())
}

func TestRunRoutes_SkipsNonRouteFiles(t *testing.T) {
	chdirTemp(t)
	routesDir := "app/rest/routes"
	require.NoError(t, os.MkdirAll(routesDir, 0755))
	// A subdirectory + a non .routes.go file + an unreadable name
	require.NoError(t, os.MkdirAll(routesDir+"/subdir", 0755))
	require.NoError(t, os.WriteFile(routesDir+"/notaroute.go", []byte("package routes"), 0644))
	assert.NoError(t, runRoutes())
}

func TestRoutesCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "routes" {
			found = true
			break
		}
	}
	assert.True(t, found, "routesCmd should be registered on rootCmd")
}

func TestRoutesCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, routesCmd.Short)
	assert.NotEmpty(t, routesCmd.Long)
}

func TestExtractRoutes_BasicRouteFile(t *testing.T) {
	content := `package routes

func UserRoutes(r chi.Router, c *controllers.UserController) {
	r.Get("/users", httputil.Handle(c.List))
	r.Post("/users", httputil.Handle(c.Create))
	r.Get("/users/{id}", httputil.Handle(c.GetByID))
	r.Put("/users/{id}", httputil.Handle(c.Update))
	r.Delete("/users/{id}", httputil.Handle(c.Archive))
}`

	routes := extractRoutes(content, "/api/v1", "user.routes.go")

	assert.Len(t, routes, 5)
	assert.Equal(t, "GET", routes[0].Method)
	assert.Equal(t, "/api/v1/users", routes[0].Path)
	assert.Equal(t, "user.routes.go", routes[0].Filename)

	assert.Equal(t, "POST", routes[1].Method)
	assert.Equal(t, "/api/v1/users", routes[1].Path)

	assert.Equal(t, "DELETE", routes[4].Method)
	assert.Equal(t, "/api/v1/users/{id}", routes[4].Path)
}

func TestExtractRoutes_IndexFile(t *testing.T) {
	content := `package routes

func InitApiRoutes(config *RouteConfig) *chi.Mux {
	r.Get("/health", httputil.Handle(config.HealthController.Check))
	r.Get("/health/live", httputil.Handle(config.HealthController.Live))
	r.Get("/health/ready", httputil.Handle(config.HealthController.Ready))
}`

	routes := extractRoutes(content, "", "index.routes.go")

	assert.Len(t, routes, 3)
	assert.Equal(t, "GET", routes[0].Method)
	assert.Equal(t, "/health", routes[0].Path)
	assert.Equal(t, "/health/live", routes[1].Path)
	assert.Equal(t, "/health/ready", routes[2].Path)
}

func TestExtractRoutes_WildcardHandler(t *testing.T) {
	content := `package routes

func InitApiRoutes(config *RouteConfig) *chi.Mux {
	r.Get("/health", httputil.Handle(config.HealthController.Check))
	r.Handle("/swagger/*", httpSwagger.WrapHandler)
}`

	routes := extractRoutes(content, "", "index.routes.go")

	assert.Len(t, routes, 2)
	assert.Equal(t, "GET", routes[0].Method)
	assert.Equal(t, "/health", routes[0].Path)
	// Wildcard-mounted handlers show as GET with the pattern as-is.
	assert.Equal(t, "GET", routes[1].Method)
	assert.Equal(t, "/swagger/*", routes[1].Path)
}

func TestExtractRoutes_EmptyContent(t *testing.T) {
	routes := extractRoutes("package routes", "/api/v1", "empty.routes.go")
	assert.Empty(t, routes)
}

func TestExtractRoutes_NoPrefix(t *testing.T) {
	content := `r.Get("/test", httputil.Handle(c.Test))`
	routes := extractRoutes(content, "", "test.routes.go")

	assert.Len(t, routes, 1)
	assert.Equal(t, "/test", routes[0].Path)
}

func TestRunRoutes_NoRoutesDir(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	err := runRoutes()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "routes directory not found")
}

// TestRunRoutes_FeatureLayoutUsesLayoutRouteFiles covers the feature arm of
// runRoutes' layout switch.
//
// Layered keeps every registration under app/rest/routes/, so runRoutes scans
// that directory. Feature scatters them to app/<resource>/routes.go, and the
// only thing that knows where they went is layout.RouteFiles(). Taking the
// wrong arm makes `gofasta routes` silently report an empty route table for a
// feature project.
func TestRunRoutes_FeatureLayoutUsesLayoutRouteFiles(t *testing.T) {
	inRenderedProject(t)

	// Move the user routes into the feature package and declare the layout,
	// which is what layout.Detect() reads.
	_, _, err := migrateResource(userResourceFixture(), fixtureModulePath)
	require.NoError(t, err)
	require.NoError(t, flipLayoutInConfig())

	out := captureStdout(t, func() {
		require.NoError(t, runRoutes())
	})

	assert.Contains(t, out, "/users",
		"a feature project's per-resource routes must still be discovered")
}

// TestRunRoutes_FeatureLayoutWithNoRouteFiles covers the same arm when the
// layout resolves to nothing — an empty listing rather than an error.
func TestRunRoutes_FeatureLayoutWithNoRouteFiles(t *testing.T) {
	inRenderedProject(t)
	require.NoError(t, flipLayoutInConfig())
	require.NoError(t, os.RemoveAll("app/rest/routes"))

	assert.NoError(t, runRoutes(),
		"a project with no route files is an empty table, not a failure")
}
