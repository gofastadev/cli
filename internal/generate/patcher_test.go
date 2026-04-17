package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatchContainer_AddsFields(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	d.IncludeController = true

	containerContent := `package di

import (
	"github.com/testorg/testapp/app/rest/controllers"
	svcInterfaces "github.com/testorg/testapp/app/services/interfaces"
	"github.com/testorg/testapp/app/graphql/resolvers"
)

type Container struct {
	Resolver       *resolvers.Resolver
	UserController *controllers.UserController
}
`
	writeTestFile(t, "app/di/container.go", containerContent)

	err := PatchContainer(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/di/container.go")
	assert.Contains(t, content, "ProductRepo")
	assert.Contains(t, content, "ProductService")
	assert.Contains(t, content, "ProductController")
}

func TestPatchContainer_WithExistingRepoImport(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	d.IncludeController = false

	containerContent := `package di

import (
	repoInterfaces "github.com/testorg/testapp/app/repositories/interfaces"
	"github.com/testorg/testapp/app/rest/controllers"
	svcInterfaces "github.com/testorg/testapp/app/services/interfaces"
	"github.com/testorg/testapp/app/graphql/resolvers"
)

type Container struct {
	Resolver       *resolvers.Resolver
}
`
	writeTestFile(t, "app/di/container.go", containerContent)

	err := PatchContainer(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/di/container.go")
	assert.Contains(t, content, "ProductRepo")
	assert.Contains(t, content, "ProductService")
	// Should NOT have ProductController since IncludeController=false
	assert.NotContains(t, content, "ProductController")
}

func TestPatchContainer_SkipsIfAlreadyWired(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	containerContent := `package di

type Container struct {
	ProductService svcInterfaces.ProductServiceInterface
	Resolver       *resolvers.Resolver
}
`
	writeTestFile(t, "app/di/container.go", containerContent)

	err := PatchContainer(d)
	require.NoError(t, err)
	// Should not duplicate
}

func TestPatchWireFile_AddsProviderSet(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	wireContent := `package di

func InitializeContainer() *Container {
	wire.Build(
		providers.CoreSet,
		providers.GraphQLSet,
	)
	return nil
}
`
	writeTestFile(t, "app/di/wire.go", wireContent)

	err := PatchWireFile(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/di/wire.go")
	assert.Contains(t, content, "providers.ProductSet")
}

func TestPatchWireFile_SkipsIfExists(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	wireContent := `package di
	providers.ProductSet,
	providers.GraphQLSet,
`
	writeTestFile(t, "app/di/wire.go", wireContent)

	err := PatchWireFile(d)
	require.NoError(t, err)
}

func TestPatchResolver_AddsServiceField(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	resolverContent := `package resolvers

import (
	svcInterfaces "github.com/testorg/testapp/app/services/interfaces"
)

type Resolver struct {
	UserService svcInterfaces.UserServiceInterface
}

// NewResolver creates a new resolver.
func NewResolver(userService svcInterfaces.UserServiceInterface) *Resolver {
	return &Resolver{UserService: userService}
}
`
	writeTestFile(t, "app/graphql/resolvers/resolver.go", resolverContent)

	err := PatchResolver(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/graphql/resolvers/resolver.go")
	assert.Contains(t, content, "ProductService")
	assert.Contains(t, content, "productService svcInterfaces.ProductServiceInterface")
}

func TestPatchResolver_SkipsIfExists(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	resolverContent := `package resolvers

type Resolver struct {
	ProductService svcInterfaces.ProductServiceInterface
}

func NewResolver(productService svcInterfaces.ProductServiceInterface) *Resolver {
	return &Resolver{ProductService: productService}
}
`
	writeTestFile(t, "app/graphql/resolvers/resolver.go", resolverContent)

	err := PatchResolver(d)
	require.NoError(t, err)
}

func TestPatchResolver_ErrorNoSignature(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	resolverContent := `package resolvers

type Resolver struct {
}

func CreateResolver() *Resolver {
	return &Resolver{}
}
`
	writeTestFile(t, "app/graphql/resolvers/resolver.go", resolverContent)

	err := PatchResolver(d)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NewResolver")
}

func TestPatchRouteConfig_AddsController(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	routeContent := `package routes

import (
	"github.com/testorg/testapp/app/rest/controllers"
	"github.com/gofastadev/gofasta/pkg/health"
)

type RouteConfig struct {
	HealthController *health.Controller
}

func InitApiRoutes(config *RouteConfig) *chi.Mux {
	r := chi.NewRouter()
	api := chi.NewRouter()
	r.Mount("/api/v1", api)
	return r
}
`
	writeTestFile(t, "app/rest/routes/index.routes.go", routeContent)

	err := PatchRouteConfig(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/rest/routes/index.routes.go")
	assert.Contains(t, content, "ProductController")
	assert.Contains(t, content, "ProductRoutes")
}

func TestPatchRouteConfig_SkipsIfExists(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	routeContent := `package routes
	ProductController *controllers.ProductController
`
	writeTestFile(t, "app/rest/routes/index.routes.go", routeContent)

	err := PatchRouteConfig(d)
	require.NoError(t, err)
}

func TestPatchServeFile_AddsController(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	serveContent := `package cmd

	apiRouter := routes.InitApiRoutes(&routes.RouteConfig{
		HealthController: healthController,
	})
`
	writeTestFile(t, "cmd/serve.go", serveContent)

	err := PatchServeFile(d)
	require.NoError(t, err)

	content := readTestFile(t, "cmd/serve.go")
	assert.Contains(t, content, "ProductController")
}

func TestPatchServeFile_SkipsIfExists(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	serveContent := `package cmd
	ProductController: container.ProductController,
	HealthController: healthController,
`
	writeTestFile(t, "cmd/serve.go", serveContent)

	err := PatchServeFile(d)
	require.NoError(t, err)
}
