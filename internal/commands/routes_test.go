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

func UserRoutes(r *mux.Router, c *controllers.UserController) {
	r.HandleFunc("/users", httputil.Handle(c.List)).Methods("GET")
	r.HandleFunc("/users", httputil.Handle(c.Create)).Methods("POST")
	r.HandleFunc("/users/{id}", httputil.Handle(c.GetByID)).Methods("GET")
	r.HandleFunc("/users/{id}", httputil.Handle(c.Update)).Methods("PUT")
	r.HandleFunc("/users/{id}", httputil.Handle(c.Archive)).Methods("DELETE")
}`

	routes := extractRoutes(content, "/api/v1", "user.routes.go")

	assert.Len(t, routes, 5)
	assert.Equal(t, "GET", routes[0].method)
	assert.Equal(t, "/api/v1/users", routes[0].path)
	assert.Equal(t, "user.routes.go", routes[0].filename)

	assert.Equal(t, "POST", routes[1].method)
	assert.Equal(t, "/api/v1/users", routes[1].path)

	assert.Equal(t, "DELETE", routes[4].method)
	assert.Equal(t, "/api/v1/users/{id}", routes[4].path)
}

func TestExtractRoutes_IndexFile(t *testing.T) {
	content := `package routes

func InitApiRoutes(config *RouteConfig) *mux.Router {
	r.HandleFunc("/health", httputil.Handle(config.HealthController.Check)).Methods("GET")
	r.HandleFunc("/health/live", httputil.Handle(config.HealthController.Live)).Methods("GET")
	r.HandleFunc("/health/ready", httputil.Handle(config.HealthController.Ready)).Methods("GET")
}`

	routes := extractRoutes(content, "", "index.routes.go")

	assert.Len(t, routes, 3)
	assert.Equal(t, "GET", routes[0].method)
	assert.Equal(t, "/health", routes[0].path)
	assert.Equal(t, "/health/live", routes[1].path)
	assert.Equal(t, "/health/ready", routes[2].path)
}

func TestExtractRoutes_EmptyContent(t *testing.T) {
	routes := extractRoutes("package routes", "/api/v1", "empty.routes.go")
	assert.Empty(t, routes)
}

func TestExtractRoutes_NoPrefix(t *testing.T) {
	content := `r.HandleFunc("/test", httputil.Handle(c.Test)).Methods("GET")`
	routes := extractRoutes(content, "", "test.routes.go")

	assert.Len(t, routes, 1)
	assert.Equal(t, "/test", routes[0].path)
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
