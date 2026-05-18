// Package featurize converts a layered project's per-resource source
// files into the feature-package shape:
//
//	app/models/<snake>.model.go              → app/<snake>/model.go
//	app/dtos/<snake>.dtos.go                 → app/<snake>/dtos.go
//	app/repositories/<snake>.repository.go   → app/<snake>/repository.go
//	app/repositories/interfaces/<snake>_repository.go → app/<snake>/repository_iface.go
//	app/services/<snake>.service.go          → app/<snake>/service.go
//	app/services/interfaces/<snake>_service.go → app/<snake>/service_iface.go
//	app/services/<snake>_errors.go           → app/<snake>/errors.go
//	app/services/<snake>_inputs.go           → app/<snake>/inputs.go
//	app/rest/controllers/<snake>.controller.go → app/<snake>/controller.go
//	app/rest/routes/<snake>.routes.go        → app/<snake>/routes.go
//	app/validators/<snake>.validators.go     → app/<snake>/validators.go
//	app/di/providers/<snake>.go              → app/<snake>/wire.go
//
// Plus cross-cutting per-layout files (container.go, wire.go,
// index.routes.go, di/providers/core.go) whose import shape changes.
//
// # AST-based transformation
//
// The transformer uses github.com/dave/dst (decorated syntax tree)
// rather than regex so that:
//
//   - aliased imports (`alias "path"`) round-trip correctly
//   - SelectorExpr collapse is identifier-aware (won't rewrite a
//     constant string that happens to contain "models.User")
//   - external test packages (`package controllers_test`) are
//     recognized by their syntax shape, not a fragile regex
//   - the rendered output is gofmt-clean
//
// The transformer operates on RENDERED Go source — text/template
// placeholders must be resolved by the caller before passing the bytes
// in. `gofasta new --layout=feature` renders the .tmpl file with
// ProjectData first, then routes the resulting source through here.
//
// Phase D's `gofasta refactor feature-package` command reuses this
// same engine on a user's project source. The transformations are
// identical — only the input source path / output destination differ.
//
// # File map
//
//	featurize.go    — public types (Resource, Options) and package doc
//	paths.go        — path-mapping tables (PerResourceMapping etc.)
//	forward.go      — TransformPerResource (layered → feature)
//	reverse.go      — TransformPerResourceReverse (feature → layered)
//	crosscutting.go — Transform{Container,Wire,IndexRoutes,Mock,…} +
//	                  the shared transformCrossCutting engine
//	ast.go          — generic dst utilities: imports, walker, rewrite
package featurize

// Resource describes one resource being featurized.
type Resource struct {
	Name   string // PascalCase ("User")
	Snake  string // snake_case ("user")
	Plural string // PascalCase plural ("Users")
}

// Options describes the transformation's environment.
type Options struct {
	ModulePath string   // "github.com/acme/myapp"
	Resource   Resource // the resource this file belongs to
}
