// feature.go — the feature-package layout.
//
// Resource files collapse into a single per-resource directory:
//
//	app/<snake>/model.go
//	app/<snake>/dtos.go
//	app/<snake>/repository.go            + repository_iface.go
//	app/<snake>/service.go               + service_iface.go
//	app/<snake>/errors.go                + inputs.go
//	app/<snake>/controller.go
//	app/<snake>/routes.go
//	app/<snake>/validators.go
//	app/<snake>/wire.go                  (per-feature Wire set)
//
// Shared concerns stay layered: app/di/, app/jobs/, app/tasks/,
// app/shared/, app/graphql/, app/validators/ (infra only), cmd/.
// app/rest/routes/index.routes.go also remains and is per-layout aware
// (it calls userpkg.RegisterRoutes(api, config.UserController) under
// feature, UserRoutes(api, config.UserController) under layered).

package layout

import (
	"fmt"
	"path/filepath"
)

// featureLayout is the feature-package implementation.
//
//revive:disable:exported // method docs live on the Layout interface
type featureLayout struct{}

func (featureLayout) Kind() Kind      { return Feature }
func (featureLayout) IsFeature() bool { return true }

// Under Option B, models stay in app/models/ because DTO mappers in
// the feature need to reference *models.X without creating a cycle.
func (featureLayout) ModelFile(snake string) string {
	return fmt.Sprintf("app/models/%s.model.go", snake)
}
func (featureLayout) RepoIfaceFile(snake string) string {
	return fmt.Sprintf("app/%s/repository_iface.go", snake)
}
func (featureLayout) RepoImplFile(snake string) string {
	return fmt.Sprintf("app/%s/repository.go", snake)
}
func (featureLayout) RepoTestFile(snake string) string {
	return fmt.Sprintf("app/%s/repository_test.go", snake)
}
func (featureLayout) SvcIfaceFile(snake string) string {
	return fmt.Sprintf("app/%s/service_iface.go", snake)
}
func (featureLayout) SvcImplFile(snake string) string {
	return fmt.Sprintf("app/%s/service.go", snake)
}
func (featureLayout) SvcTestFile(snake string) string {
	return fmt.Sprintf("app/%s/service_test.go", snake)
}
func (featureLayout) ErrorsFile(snake string) string {
	return fmt.Sprintf("app/%s/errors.go", snake)
}
func (featureLayout) InputsFile(snake string) string {
	return fmt.Sprintf("app/%s/inputs.go", snake)
}
func (featureLayout) InputsTestFile(snake string) string {
	return fmt.Sprintf("app/%s/inputs_test.go", snake)
}
func (featureLayout) DTOsFile(snake string) string {
	return fmt.Sprintf("app/%s/dtos.go", snake)
}
func (featureLayout) DTOsTestFile(snake string) string {
	return fmt.Sprintf("app/%s/dtos_test.go", snake)
}
func (featureLayout) ControllerFile(snake string) string {
	return fmt.Sprintf("app/%s/controller.go", snake)
}
func (featureLayout) ControllerTestFile(snake string) string {
	return fmt.Sprintf("app/%s/controller_test.go", snake)
}
func (featureLayout) RoutesFile(snake string) string {
	return fmt.Sprintf("app/%s/routes.go", snake)
}

// Under Option B, per-resource validators stay in app/validators/
// because register.go calls package-private helpers (isRecordExist*,
// isValidPhoneNumber) that would break if scattered into features.
func (featureLayout) ValidatorsFile(snake string) string {
	return fmt.Sprintf("app/validators/%s.validators.go", snake)
}
func (featureLayout) WireProviderFile(snake string) string {
	return fmt.Sprintf("app/%s/wire.go", snake)
}

func (featureLayout) ContainerFile() string  { return "app/di/container.go" }
func (featureLayout) WireFile() string       { return "app/di/wire.go" }
func (featureLayout) RouteIndexFile() string { return "app/rest/routes/index.routes.go" }
func (featureLayout) ServeFile() string      { return "cmd/serve.go" }
func (featureLayout) ResolverFile() string   { return "app/graphql/resolvers/resolver.go" }
func (featureLayout) ResolverResourceFile(snake string) string {
	return "app/graphql/resolvers/" + snake + ".resolvers.go"
}

// InterfaceDirs returns the per-feature directories `g mock --all` should
// walk. In the feature layout each resource keeps its interfaces alongside
// its implementation (app/<resource>/repository_iface.go, service_iface.go),
// so this walks app/*/ and returns every resource directory that declares at
// least one *_iface.go file, excluding shared-concern directories.
func (featureLayout) InterfaceDirs() []string {
	var dirs []string
	for _, dir := range featureResourceDirs() {
		if dirHasSuffixFile(dir, "_iface.go") {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func (featureLayout) RoutesDir() string { return filepath.Join("app", "rest", "routes") }

// RouteFiles returns the feature-layout route files: the shared route index
// plus each resource's own routes.go (app/<resource>/routes.go), which is
// where renameRoutesFunc moves the per-resource r.Get/r.Post registrations.
func (featureLayout) RouteFiles() []string {
	var files []string
	if index := filepath.Join("app", "rest", "routes", "index.routes.go"); fileExists(index) {
		files = append(files, index)
	}
	for _, dir := range featureResourceDirs() {
		if routes := filepath.Join(dir, "routes.go"); fileExists(routes) {
			files = append(files, routes)
		}
	}
	return files
}

func (featureLayout) MigrationsDir() string { return filepath.Join("db", "migrations") }
