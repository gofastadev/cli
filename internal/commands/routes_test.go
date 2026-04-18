package commands

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
