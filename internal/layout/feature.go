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

func (featureLayout) ModelFile(snake string) string {
	return fmt.Sprintf("app/%s/model.go", snake)
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
func (featureLayout) ValidatorsFile(snake string) string {
	return fmt.Sprintf("app/%s/validators.go", snake)
}
func (featureLayout) WireProviderFile(snake string) string {
	return fmt.Sprintf("app/%s/wire.go", snake)
}

func (featureLayout) ContainerFile() string  { return "app/di/container.go" }
func (featureLayout) WireFile() string       { return "app/di/wire.go" }
func (featureLayout) RouteIndexFile() string { return "app/rest/routes/index.routes.go" }
func (featureLayout) ServeFile() string      { return "cmd/serve.go" }
func (featureLayout) ResolverFile() string   { return "app/graphql/resolvers/resolver.go" }

// InterfaceDirs returns the per-feature directories `g mock --all`
// should walk. Excludes shared concern directories (di, jobs, tasks,
// graphql, main, devtools, shared, validators, rest).
//
// Implementation in Phase A is a placeholder — the feature layout isn't
// reachable from configutil.ReadLayout yet because the `--layout=feature`
// flag is wired in Phase B. The full directory-walking implementation
// lands when Phase C extends gen_mock.go.
func (featureLayout) InterfaceDirs() []string {
	// Phase-C TODO: walk app/*/ filtered by IsFeatureResourceDir.
	// For Phase A this is unreachable (Detect always returns Layered).
	return nil
}

func (featureLayout) RoutesDir() string     { return filepath.Join("app", "rest", "routes") }
func (featureLayout) MigrationsDir() string { return filepath.Join("db", "migrations") }
