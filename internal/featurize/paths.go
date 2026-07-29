// Package featurize — path mapping tables.
//
// Source-of-truth for "where does each layered file go in feature
// layout" (and the inverse). The transformer engines in forward.go,
// reverse.go, and crosscutting.go consume these tables; the refactor
// command and `new --layout=feature` walker do too.

package featurize

import "fmt"

// PathPair maps a layered path to its feature equivalent.
type PathPair struct {
	Layered string
	Feature string
}

// SharedRelocations returns the (layered, feature) path pairs for files
// that simply move location in feature mode without any source-level
// rewriting. Per Option B:
//
//   - app/dtos/aliases.go → app/shared/dtos/aliases.go (package stays
//     `dtos`, but the import path callers use becomes
//     `<mod>/app/shared/dtos` — featurize handles that rewrite on the
//     caller side via rewriteDtosImportPath).
//   - app/dtos/generated-types.dtos.go → app/shared/dtos/ — gqlgen's
//     generated models file (GraphQL projects only; absent in REST-only
//     projects, and relocation callers stat-gate each pair). Package
//     stays `dtos`; its only intra-project references are same-package,
//     so it moves without rewriting. gqlgen.yml's model.filename is
//     rewritten in step with this move (see gqlgenconfig.go).
func SharedRelocations() []PathPair {
	return []PathPair{
		{Layered: "app/dtos/aliases.go", Feature: "app/shared/dtos/aliases.go"},
		{Layered: "app/dtos/generated-types.dtos.go", Feature: "app/shared/dtos/generated-types.dtos.go"},
	}
}

// PerResourceMapping returns the (sourcePath, destPath) pairs for a
// given resource — i.e. which layered files become which feature files.
// Used by both `gofasta new --layout=feature` (to know which layered
// templates to transform) and Phase D's refactor command.
func PerResourceMapping(snake string) []PathPair {
	// Models and validators files intentionally NOT moved — see
	// `collapsablePaths` for the rationale. Per-resource dtos files
	// DO move into the feature (Option B). Shared aliases relocate
	// to app/shared/dtos/ via SharedRelocations() instead.
	return []PathPair{
		{Layered: fmt.Sprintf("app/dtos/%s.dtos.go", snake), Feature: fmt.Sprintf("app/%s/dtos.go", snake)},
		{Layered: fmt.Sprintf("app/dtos/%s.dtos_test.go", snake), Feature: fmt.Sprintf("app/%s/dtos_test.go", snake)},
		{Layered: fmt.Sprintf("app/repositories/%s.repository.go", snake), Feature: fmt.Sprintf("app/%s/repository.go", snake)},
		{Layered: fmt.Sprintf("app/repositories/interfaces/%s_repository.go", snake), Feature: fmt.Sprintf("app/%s/repository_iface.go", snake)},
		{Layered: fmt.Sprintf("app/repositories/%s.repository_test.go", snake), Feature: fmt.Sprintf("app/%s/repository_test.go", snake)},
		{Layered: fmt.Sprintf("app/services/%s.service.go", snake), Feature: fmt.Sprintf("app/%s/service.go", snake)},
		{Layered: fmt.Sprintf("app/services/interfaces/%s_service.go", snake), Feature: fmt.Sprintf("app/%s/service_iface.go", snake)},
		{Layered: fmt.Sprintf("app/services/%s.service_test.go", snake), Feature: fmt.Sprintf("app/%s/service_test.go", snake)},
		{Layered: fmt.Sprintf("app/services/%s_errors.go", snake), Feature: fmt.Sprintf("app/%s/errors.go", snake)},
		{Layered: fmt.Sprintf("app/services/%s_inputs.go", snake), Feature: fmt.Sprintf("app/%s/inputs.go", snake)},
		{Layered: fmt.Sprintf("app/services/%s_inputs_test.go", snake), Feature: fmt.Sprintf("app/%s/inputs_test.go", snake)},
		{Layered: fmt.Sprintf("app/rest/controllers/%s.controller.go", snake), Feature: fmt.Sprintf("app/%s/controller.go", snake)},
		{Layered: fmt.Sprintf("app/rest/controllers/%s.controller_test.go", snake), Feature: fmt.Sprintf("app/%s/controller_test.go", snake)},
		{Layered: fmt.Sprintf("app/rest/routes/%s.routes.go", snake), Feature: fmt.Sprintf("app/%s/routes.go", snake)},
		{Layered: fmt.Sprintf("app/di/providers/%s.go", snake), Feature: fmt.Sprintf("app/%s/wire.go", snake)},
	}
}

// ---------- Reverse direction (feature → layered) ----------

// LayeredDestination describes where a feature file belongs in the
// layered layout, plus the package name its package decl must take.
type LayeredDestination struct {
	Path        string // layered path (e.g. "app/services/user.service.go")
	PackageName string // target package decl ("services", "interfaces", "controllers", ...)
}

// ReversePerResourceMapping returns the (feature_path → layered_path)
// pairs for unwinding one resource back to layered. Inverse of
// PerResourceMapping. The PackageName field tells the reverse
// transformer which `package X` decl to write.
func ReversePerResourceMapping(snake string) []struct {
	Feature string
	Dest    LayeredDestination
} {
	return []struct {
		Feature string
		Dest    LayeredDestination
	}{
		{fmt.Sprintf("app/%s/dtos.go", snake), LayeredDestination{Path: fmt.Sprintf("app/dtos/%s.dtos.go", snake), PackageName: "dtos"}},
		{fmt.Sprintf("app/%s/dtos_test.go", snake), LayeredDestination{Path: fmt.Sprintf("app/dtos/%s.dtos_test.go", snake), PackageName: "dtos_test"}},
		{fmt.Sprintf("app/%s/repository.go", snake), LayeredDestination{Path: fmt.Sprintf("app/repositories/%s.repository.go", snake), PackageName: "repositories"}},
		{fmt.Sprintf("app/%s/repository_iface.go", snake), LayeredDestination{Path: fmt.Sprintf("app/repositories/interfaces/%s_repository.go", snake), PackageName: "interfaces"}},
		{fmt.Sprintf("app/%s/repository_test.go", snake), LayeredDestination{Path: fmt.Sprintf("app/repositories/%s.repository_test.go", snake), PackageName: "repositories_test"}},
		{fmt.Sprintf("app/%s/service.go", snake), LayeredDestination{Path: fmt.Sprintf("app/services/%s.service.go", snake), PackageName: "services"}},
		{fmt.Sprintf("app/%s/service_iface.go", snake), LayeredDestination{Path: fmt.Sprintf("app/services/interfaces/%s_service.go", snake), PackageName: "interfaces"}},
		{fmt.Sprintf("app/%s/service_test.go", snake), LayeredDestination{Path: fmt.Sprintf("app/services/%s.service_test.go", snake), PackageName: "services_test"}},
		{fmt.Sprintf("app/%s/errors.go", snake), LayeredDestination{Path: fmt.Sprintf("app/services/%s_errors.go", snake), PackageName: "services"}},
		{fmt.Sprintf("app/%s/inputs.go", snake), LayeredDestination{Path: fmt.Sprintf("app/services/%s_inputs.go", snake), PackageName: "services"}},
		{fmt.Sprintf("app/%s/inputs_test.go", snake), LayeredDestination{Path: fmt.Sprintf("app/services/%s_inputs_test.go", snake), PackageName: "services_test"}},
		{fmt.Sprintf("app/%s/controller.go", snake), LayeredDestination{Path: fmt.Sprintf("app/rest/controllers/%s.controller.go", snake), PackageName: "controllers"}},
		{fmt.Sprintf("app/%s/controller_test.go", snake), LayeredDestination{Path: fmt.Sprintf("app/rest/controllers/%s.controller_test.go", snake), PackageName: "controllers_test"}},
		{fmt.Sprintf("app/%s/routes.go", snake), LayeredDestination{Path: fmt.Sprintf("app/rest/routes/%s.routes.go", snake), PackageName: "routes"}},
		{fmt.Sprintf("app/%s/wire.go", snake), LayeredDestination{Path: fmt.Sprintf("app/di/providers/%s.go", snake), PackageName: "providers"}},
	}
}

// SharedRelocationsReverse mirrors SharedRelocations but inverted:
// app/shared/dtos/aliases.go → app/dtos/aliases.go.
func SharedRelocationsReverse() []PathPair {
	return []PathPair{
		{Layered: "app/shared/dtos/aliases.go", Feature: "app/dtos/aliases.go"},
		{Layered: "app/shared/dtos/generated-types.dtos.go", Feature: "app/dtos/generated-types.dtos.go"},
		// Field naming reuses PathPair's Layered/Feature slots — Layered
		// here is "the source path during reverse" (i.e. the feature
		// location); Feature is "the destination" (i.e. the layered
		// location). Refactor's reverse caller treats them accordingly.
	}
}
