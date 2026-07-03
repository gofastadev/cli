// layered.go — the historical "one directory per layer" layout.
//
// Every literal string this file returns is what the generators
// previously hardcoded via `fmt.Sprintf("app/models/%s.model.go", snake)`
// and friends. The byte-for-byte regression test in layout_test.go pins
// these so a refactor of the call sites can't drift the on-disk shape.

package layout

import (
	"fmt"
	"path/filepath"
)

// layeredLayout is the layered-architecture implementation.
//
// Resource files spread across layer directories:
//
//	app/models/<snake>.model.go
//	app/dtos/<snake>.dtos.go
//	app/repositories/<snake>.repository.go        + interfaces/<snake>_repository.go
//	app/services/<snake>.service.go               + interfaces/<snake>_service.go
//	app/services/<snake>_errors.go                + <snake>_inputs.go
//	app/rest/controllers/<snake>.controller.go
//	app/rest/routes/<snake>.routes.go
//	app/validators/<snake>.validators.go
//	app/di/providers/<snake>.go
//
//revive:disable:exported // method docs live on the Layout interface
type layeredLayout struct{}

func (layeredLayout) Kind() Kind      { return Layered }
func (layeredLayout) IsFeature() bool { return false }

func (layeredLayout) ModelFile(snake string) string {
	return fmt.Sprintf("app/models/%s.model.go", snake)
}

func (layeredLayout) RepoIfaceFile(snake string) string {
	return fmt.Sprintf("app/repositories/interfaces/%s_repository.go", snake)
}

func (layeredLayout) RepoImplFile(snake string) string {
	return fmt.Sprintf("app/repositories/%s.repository.go", snake)
}

func (layeredLayout) RepoTestFile(snake string) string {
	return fmt.Sprintf("app/repositories/%s.repository_test.go", snake)
}

func (layeredLayout) SvcIfaceFile(snake string) string {
	return fmt.Sprintf("app/services/interfaces/%s_service.go", snake)
}

func (layeredLayout) SvcImplFile(snake string) string {
	return fmt.Sprintf("app/services/%s.service.go", snake)
}

func (layeredLayout) SvcTestFile(snake string) string {
	return fmt.Sprintf("app/services/%s.service_test.go", snake)
}

func (layeredLayout) ErrorsFile(snake string) string {
	return fmt.Sprintf("app/services/%s_errors.go", snake)
}

func (layeredLayout) InputsFile(snake string) string {
	return fmt.Sprintf("app/services/%s_inputs.go", snake)
}

func (layeredLayout) InputsTestFile(snake string) string {
	return fmt.Sprintf("app/services/%s_inputs_test.go", snake)
}

func (layeredLayout) DTOsFile(snake string) string {
	return fmt.Sprintf("app/dtos/%s.dtos.go", snake)
}

func (layeredLayout) DTOsTestFile(snake string) string {
	return fmt.Sprintf("app/dtos/%s.dtos_test.go", snake)
}

func (layeredLayout) ControllerFile(snake string) string {
	return fmt.Sprintf("app/rest/controllers/%s.controller.go", snake)
}

func (layeredLayout) ControllerTestFile(snake string) string {
	return fmt.Sprintf("app/rest/controllers/%s.controller_test.go", snake)
}

func (layeredLayout) RoutesFile(snake string) string {
	return fmt.Sprintf("app/rest/routes/%s.routes.go", snake)
}

func (layeredLayout) ValidatorsFile(snake string) string {
	return fmt.Sprintf("app/validators/%s.validators.go", snake)
}

func (layeredLayout) WireProviderFile(snake string) string {
	return fmt.Sprintf("app/di/providers/%s.go", snake)
}

func (layeredLayout) ContainerFile() string  { return "app/di/container.go" }
func (layeredLayout) WireFile() string       { return "app/di/wire.go" }
func (layeredLayout) RouteIndexFile() string { return "app/rest/routes/index.routes.go" }
func (layeredLayout) ServeFile() string      { return "cmd/serve.go" }
func (layeredLayout) ResolverFile() string   { return "app/graphql/resolvers/resolver.go" }

func (layeredLayout) InterfaceDirs() []string {
	return []string{
		filepath.Join("app", "services", "interfaces"),
		filepath.Join("app", "repositories", "interfaces"),
	}
}

func (layeredLayout) RoutesDir() string { return filepath.Join("app", "rest", "routes") }

// RouteFiles returns every *.routes.go file under app/rest/routes/, where the
// layered layout keeps all route registrations.
func (layeredLayout) RouteFiles() []string {
	return globRouteFiles(filepath.Join("app", "rest", "routes"))
}

func (layeredLayout) MigrationsDir() string { return filepath.Join("db", "migrations") }
