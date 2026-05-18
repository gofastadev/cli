// Package layout owns every project-structure-dependent path, import,
// and symbol used by the gofasta generators. It exists so the generators
// can be layout-agnostic — `Layout.ModelFile("user")` returns
// "app/models/user.model.go" under the layered layout and
// "app/user/model.go" under the feature layout, and the caller doesn't
// know the difference.
//
// Two implementations:
//
//   - Layered (the historical default): one directory per layer
//     (app/models/, app/services/, app/repositories/, app/dtos/,
//     app/rest/controllers/, app/rest/routes/, app/validators/,
//     app/di/providers/), with separate `interfaces` sub-packages.
//
//   - Feature: one directory per resource (app/<resource>/) containing
//     every layer's file for that resource. Shared concerns
//     (jobs/tasks/graphql/validators-infra/di) stay layered.
//
// The active layout for a project is recorded in config.yaml under
// `project.layout` and read via configutil.ReadLayout. Generators call
// Detect() once per invocation to resolve which Layout to use.
package layout

// Kind enumerates the supported project layouts.
//
// Adding a new layout (e.g. "hexagonal") is a matter of (a) implementing
// the Layout interface, (b) extending For(), and (c) registering the
// flag value in cli/internal/commands/new.go's supportedLayouts.
type Kind int

const (
	// Layered is the historical layout: one directory per layer.
	Layered Kind = iota

	// Feature is the alternative layout: one directory per resource.
	Feature
)

// String returns the lowercase name of the layout, matching the value
// stored in config.yaml's `project.layout` field.
func (k Kind) String() string {
	switch k {
	case Feature:
		return "feature"
	default:
		return "layered"
	}
}

// ParseKind converts the config.yaml value into a Kind. Unknown or empty
// values fall back to Layered — this keeps projects that pre-date the
// feature working without a config migration.
func ParseKind(s string) Kind {
	switch s {
	case "feature":
		return Feature
	default:
		return Layered
	}
}

// Layout is the contract every project layout must satisfy. Methods are
// grouped by purpose:
//
//   - Per-resource files: take a snake_case resource name and return the
//     absolute-from-project-root path the generator should write to.
//
//   - Per-layout single files: paths that exist once per project
//     (container, wire, route index, serve, resolver).
//
//   - Marker constants: scaffold templates ship marker comments that
//     patchers use as insertion points. Layered and feature scaffolds
//     ship the same markers; this method exists so future layouts can
//     diverge without changing every patcher call site.
//
//   - Import paths + symbol refs: templates use these to render
//     layout-appropriate import blocks and identifier prefixes
//     (e.g. `models.User` in layered, bare `User` in feature).
//
//   - InterfaceDirs(): the directories `gofasta g mock --all` scans for
//     interface declarations.
type Layout interface {
	// Kind reports which layout this implementation represents.
	Kind() Kind

	// IsFeature reports whether this layout is the feature-package layout.
	// Templates use this as a shorthand for the most common branch.
	IsFeature() bool

	// ----- Per-resource file paths -----

	ModelFile(snake string) string
	RepoIfaceFile(snake string) string
	RepoImplFile(snake string) string
	RepoTestFile(snake string) string
	SvcIfaceFile(snake string) string
	SvcImplFile(snake string) string
	SvcTestFile(snake string) string
	ErrorsFile(snake string) string
	InputsFile(snake string) string
	InputsTestFile(snake string) string
	DTOsFile(snake string) string
	DTOsTestFile(snake string) string
	ControllerFile(snake string) string
	ControllerTestFile(snake string) string
	RoutesFile(snake string) string
	ValidatorsFile(snake string) string
	WireProviderFile(snake string) string

	// ----- Per-layout single files -----

	ContainerFile() string
	WireFile() string
	RouteIndexFile() string
	ServeFile() string
	ResolverFile() string

	// ----- Directories scanned by tooling -----

	// InterfaceDirs returns the directories `gofasta g mock --all` walks
	// looking for interface declarations.
	InterfaceDirs() []string

	// RoutesDir returns the directory `gofasta g middleware` scans for
	// route registration files.
	RoutesDir() string

	// MigrationsDir returns the path to the SQL migrations directory.
	// Stable across layouts (always "db/migrations") but exposed via the
	// interface so future layouts can override.
	MigrationsDir() string
}

// For returns the Layout implementation matching the given Kind. Unknown
// kinds fall back to layered.
func For(k Kind) Layout {
	switch k {
	case Feature:
		return featureLayout{}
	default:
		return layeredLayout{}
	}
}
