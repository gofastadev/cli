// patcher_feature_test.go — the feature-layout arms of the three patchers.
//
// In the layered layout every generated symbol lands in a shared package
// (controllers, repoInterfaces, providers), so the patchers only ever
// reference imports the scaffold already has. In the feature layout each
// resource owns a package at app/<snake>/, so the patchers must additionally
// INSERT that import before referencing it through the `<snake>pkg` alias.
// That insertion — and its failure mode when the import block cannot be
// located — is what these tests pin.

package generate

import (
	"testing"

	"github.com/gofastadev/cli/internal/layout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// featureScaffoldData is sampleScaffoldData switched to the feature layout.
func featureScaffoldData() ScaffoldData {
	d := sampleScaffoldData()
	d.Layout = layout.For(layout.Feature)
	return d
}

// --- PatchContainer ---

const featureContainer = `package di

import (
	"github.com/testorg/testapp/app/graphql/resolvers"
)

type Container struct {
	// gofasta:scaffold:container-fields
	Resolver *resolvers.Resolver
}
`

func TestPatchContainer_FeatureAddsAliasedImportAndFields(t *testing.T) {
	setupTempProject(t)
	d := featureScaffoldData()
	d.IncludeController = true
	writeTestFile(t, "app/di/container.go", featureContainer)

	require.NoError(t, PatchContainer(d))

	content := readTestFile(t, "app/di/container.go")
	assert.Contains(t, content, `productpkg "github.com/testorg/testapp/app/product"`,
		"the feature package must be imported before it is referenced")
	assert.Contains(t, content, "productpkg.ProductRepositoryInterface")
	assert.Contains(t, content, "productpkg.ProductServiceInterface")
	assert.Contains(t, content, "*productpkg.ProductController")
	// The layered qualifiers must not leak into a feature project.
	assert.NotContains(t, content, "repoInterfaces.")
	assert.NotContains(t, content, "controllers.ProductController")
}

func TestPatchContainer_FeatureWithoutController(t *testing.T) {
	setupTempProject(t)
	d := featureScaffoldData()
	d.IncludeController = false
	writeTestFile(t, "app/di/container.go", featureContainer)

	require.NoError(t, PatchContainer(d))

	content := readTestFile(t, "app/di/container.go")
	assert.Contains(t, content, "productpkg.ProductServiceInterface")
	assert.NotContains(t, content, "ProductController")
}

// TestPatchContainer_FeatureDoesNotDuplicateImport covers the branch where the
// alias is already imported — patching a second resource into a container that
// has been patched before must not add the line twice.
func TestPatchContainer_FeatureDoesNotDuplicateImport(t *testing.T) {
	setupTempProject(t)
	d := featureScaffoldData()
	writeTestFile(t, "app/di/container.go", `package di

import (
	productpkg "github.com/testorg/testapp/app/product"
	"github.com/testorg/testapp/app/graphql/resolvers"
)

type Container struct {
	// gofasta:scaffold:container-fields
	Resolver *resolvers.Resolver
}
`)

	require.NoError(t, PatchContainer(d))

	content := readTestFile(t, "app/di/container.go")
	assert.Equal(t, 1, countSubstring(content, `productpkg "github.com/testorg/testapp/app/product"`),
		"the feature import must be inserted at most once")
}

// TestPatchContainer_FeatureNoImportBlock covers the error return. Without a
// closing ")" there is nowhere to put the import, and emitting the field
// referencing an unimported alias would produce a file that cannot compile —
// so the patcher refuses rather than writing broken code.
func TestPatchContainer_FeatureNoImportBlock(t *testing.T) {
	setupTempProject(t)
	d := featureScaffoldData()
	writeTestFile(t, "app/di/container.go",
		"package di\n\ntype Container struct {\n\t// gofasta:scaffold:container-fields\n}\n")

	err := PatchContainer(d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not locate import block close")
}

// --- PatchWireFile ---

const featureWire = `//go:build wireinject

package di

import (
	"github.com/google/wire"
)

func InitializeContainer() (*Container, error) {
	wire.Build(
		// gofasta:scaffold:wire-providers
	)
	return nil, nil
}
`

func TestPatchWireFile_FeatureUsesAliasedProviderSet(t *testing.T) {
	setupTempProject(t)
	d := featureScaffoldData()
	writeTestFile(t, "app/di/wire.go", featureWire)

	require.NoError(t, PatchWireFile(d))

	content := readTestFile(t, "app/di/wire.go")
	assert.Contains(t, content, `productpkg "github.com/testorg/testapp/app/product"`)
	assert.Contains(t, content, "productpkg.ProductSet")
	assert.NotContains(t, content, "providers.ProductSet",
		"the layered provider package must not be referenced in a feature project")
}

func TestPatchWireFile_FeatureSkipsWhenAlreadyWired(t *testing.T) {
	setupTempProject(t)
	d := featureScaffoldData()
	writeTestFile(t, "app/di/wire.go", `//go:build wireinject

package di

import (
	productpkg "github.com/testorg/testapp/app/product"
	"github.com/google/wire"
)

func InitializeContainer() (*Container, error) {
	wire.Build(
		productpkg.ProductSet,
		// gofasta:scaffold:wire-providers
	)
	return nil, nil
}
`)
	before := readTestFile(t, "app/di/wire.go")

	require.NoError(t, PatchWireFile(d))

	assert.Equal(t, before, readTestFile(t, "app/di/wire.go"),
		"re-wiring an already-wired resource must leave the file untouched")
}

func TestPatchWireFile_FeatureNoImportBlock(t *testing.T) {
	setupTempProject(t)
	d := featureScaffoldData()
	writeTestFile(t, "app/di/wire.go",
		"package di\n\nfunc InitializeContainer() {\n\t\t// gofasta:scaffold:wire-providers\n}\n")

	err := PatchWireFile(d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not locate import block close")
}

// --- PatchRouteConfig ---

const featureRouteIndex = `package routes

import (
	"github.com/gofastadev/gofasta/pkg/health"
)

type RouteConfig struct {
	// gofasta:scaffold:route-config-fields
	HealthController *health.Controller
}

func InitAPIRoutes(config *RouteConfig) *chi.Mux {
	r := chi.NewRouter()
	api := chi.NewRouter()
	// gofasta:scaffold:route-registrations
	r.Mount("/api/v1", api)
	return r
}
`

// TestPatchRouteConfig_FeatureDelegatesToFeaturePackage pins the routing
// difference: layered calls a package-level <Name>Routes function, while
// feature calls RegisterRoutes on the resource's own package.
func TestPatchRouteConfig_FeatureDelegatesToFeaturePackage(t *testing.T) {
	setupTempProject(t)
	d := featureScaffoldData()
	writeTestFile(t, "app/rest/routes/index.routes.go", featureRouteIndex)

	require.NoError(t, PatchRouteConfig(d))

	content := readTestFile(t, "app/rest/routes/index.routes.go")
	assert.Contains(t, content, `productpkg "github.com/testorg/testapp/app/product"`)
	assert.Contains(t, content, "ProductController *productpkg.ProductController")
	assert.Contains(t, content, "productpkg.RegisterRoutes(api, config.ProductController)")
	assert.NotContains(t, content, "ProductRoutes(api",
		"the layered route-function form must not appear in a feature project")
}

func TestPatchRouteConfig_FeatureDoesNotDuplicateImport(t *testing.T) {
	setupTempProject(t)
	d := featureScaffoldData()
	writeTestFile(t, "app/rest/routes/index.routes.go", `package routes

import (
	productpkg "github.com/testorg/testapp/app/product"
	"github.com/gofastadev/gofasta/pkg/health"
)

type RouteConfig struct {
	// gofasta:scaffold:route-config-fields
	HealthController *health.Controller
}

func InitAPIRoutes(config *RouteConfig) *chi.Mux {
	api := chi.NewRouter()
	// gofasta:scaffold:route-registrations
	return nil
}
`)

	require.NoError(t, PatchRouteConfig(d))

	content := readTestFile(t, "app/rest/routes/index.routes.go")
	assert.Equal(t, 1, countSubstring(content, `productpkg "github.com/testorg/testapp/app/product"`))
}

func TestPatchRouteConfig_FeatureNoImportBlock(t *testing.T) {
	setupTempProject(t)
	d := featureScaffoldData()
	writeTestFile(t, "app/rest/routes/index.routes.go",
		"package routes\n\ntype RouteConfig struct {\n\t// gofasta:scaffold:route-config-fields\n}\n\n"+
			"func InitAPIRoutes() {\n\t// gofasta:scaffold:route-registrations\n}\n")

	err := PatchRouteConfig(d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not locate import block close")
}

// countSubstring reports how many times sub occurs in s.
func countSubstring(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}
