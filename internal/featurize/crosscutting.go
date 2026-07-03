// Package featurize — cross-cutting file transformations.
//
// container.go / wire.go / app/rest/routes/index.routes.go /
// app/di/providers/core.go are NOT per-resource files — they're
// shared infrastructure that knows about every resource. They get a
// different transformer than TransformPerResource: only their import
// shape + per-resource selector/field/call references change.
//
// The transformCrossCutting engine at the bottom of this file is the
// shared driver — each forward/reverse Transform{X} function builds
// a crossCuttingOptions struct (drop imports, add imports, rewrite
// selectors, rewrite calls) and hands it off.

package featurize

import (
	"fmt"
	"go/token"
	"strings"

	"github.com/dave/dst/decorator"
)

// ---------- Cross-cutting per-layout file transformations ----------

// TransformContainer rewrites app/di/container.go: replaces the
// layered cross-package imports (repoInterfaces, svcInterfaces,
// controllers) with per-feature alias imports, and rewrites every
// resource's field types to point at the feature packages.
func TransformContainer(src []byte, mod string, resources []Resource) ([]byte, error) {
	return transformCrossCutting(src, crossCuttingOptions{
		dropImports: []string{
			mod + "/app/repositories/interfaces",
			mod + "/app/services/interfaces",
			mod + "/app/rest/controllers",
		},
		addImports:    perFeatureImports(mod, resources),
		fieldRewrites: containerFieldRewrites(resources),
	})
}

// TransformWire rewrites app/di/wire.go: replaces
// `providers.<R>Set` with `<snake>pkg.<R>Set` and updates the import.
func TransformWire(src []byte, mod string, resources []Resource) ([]byte, error) {
	return transformCrossCutting(src, crossCuttingOptions{
		dropImports:   []string{mod + "/app/di/providers"},
		addImports:    perFeatureImports(mod, resources),
		selectorSwaps: wireSelectorSwaps(resources),
	})
}

// TransformIndexRoutes rewrites app/rest/routes/index.routes.go:
// replaces `<R>Routes(api, ...)` with `<snake>pkg.RegisterRoutes(api, ...)`
// and updates the controller field types in RouteConfig.
func TransformIndexRoutes(src []byte, mod string, resources []Resource) ([]byte, error) {
	return transformCrossCutting(src, crossCuttingOptions{
		dropImports:   []string{mod + "/app/rest/controllers"},
		addImports:    perFeatureImports(mod, resources),
		fieldRewrites: indexRoutesFieldRewrites(resources),
		callRewrites:  indexRoutesCallRewrites(resources),
	})
}

// TransformContainerReverse rewrites app/di/container.go from feature
// layout (per-feature alias imports) back to layered (repoInterfaces,
// svcInterfaces, controllers).
func TransformContainerReverse(src []byte, mod string, resources []Resource) ([]byte, error) {
	dropImports := make([]string, 0, len(resources))
	swaps := map[string]string{}
	for _, r := range resources {
		alias := r.Snake + "pkg"
		dropImports = append(dropImports, mod+"/app/"+r.Snake)
		swaps[alias+"."+r.Name+"RepositoryInterface"] = "repoInterfaces." + r.Name + "RepositoryInterface"
		swaps[alias+"."+r.Name+"ServiceInterface"] = "svcInterfaces." + r.Name + "ServiceInterface"
		swaps[alias+"."+r.Name+"Controller"] = "controllers." + r.Name + "Controller"
	}
	return transformCrossCutting(src, crossCuttingOptions{
		dropImports: dropImports,
		addImports: []importSpec{
			{alias: "repoInterfaces", path: mod + "/app/repositories/interfaces"},
			{alias: "svcInterfaces", path: mod + "/app/services/interfaces"},
			{alias: "", path: mod + "/app/rest/controllers"},
		},
		fieldRewrites: swaps,
	})
}

// TransformWireReverse rewrites app/di/wire.go back to layered:
// `<snake>pkg.UserSet` → `providers.UserSet`. The providers import
// is added (or kept — it might already be there for CoreSet).
func TransformWireReverse(src []byte, mod string, resources []Resource) ([]byte, error) {
	dropImports := make([]string, 0, len(resources))
	swaps := map[string]string{}
	for _, r := range resources {
		alias := r.Snake + "pkg"
		dropImports = append(dropImports, mod+"/app/"+r.Snake)
		swaps[alias+"."+r.Name+"Set"] = "providers." + r.Name + "Set"
	}
	return transformCrossCutting(src, crossCuttingOptions{
		dropImports: dropImports,
		addImports: []importSpec{
			{alias: "", path: mod + "/app/di/providers"},
		},
		selectorSwaps: swaps,
	})
}

// TransformIndexRoutesReverse rewrites app/rest/routes/index.routes.go
// back to layered. `<snake>pkg.RegisterRoutes(...)` becomes
// `<Name>Routes(...)`, and `*<snake>pkg.UserController` fields become
// `*controllers.UserController`.
func TransformIndexRoutesReverse(src []byte, mod string, resources []Resource) ([]byte, error) {
	dropImports := make([]string, 0, len(resources))
	fieldSwaps := map[string]string{}
	callSwaps := map[string]string{}
	for _, r := range resources {
		alias := r.Snake + "pkg"
		dropImports = append(dropImports, mod+"/app/"+r.Snake)
		fieldSwaps[alias+"."+r.Name+"Controller"] = "controllers." + r.Name + "Controller"
		// Reverse the routes call: <snake>pkg.RegisterRoutes → <R>Routes
		// This is a SelectorExpr swap where the result is a bare Ident.
		// Use selectorSwaps with a bare-name destination — the rewrite
		// engine handles single-word destinations as Ident replacements.
		callSwaps[alias+".RegisterRoutes"] = r.Name + "Routes"
	}
	return transformCrossCutting(src, crossCuttingOptions{
		dropImports: dropImports,
		addImports: []importSpec{
			{alias: "", path: mod + "/app/rest/controllers"},
		},
		fieldRewrites: fieldSwaps,
		selectorSwaps: callSwaps,
	})
}

// TransformCoreProvidersReverse rewrites app/di/providers/core.go back
// to layered. `userpkg.NewDefaultPasswordGenerator` →
// `services.NewDefaultPasswordGenerator` (PasswordGenerator moves
// back to app/services/).
func TransformCoreProvidersReverse(src []byte, mod string, resources []Resource) ([]byte, error) {
	user := findResource(resources, "user")
	if user == nil {
		return src, nil
	}
	return transformCrossCutting(src, crossCuttingOptions{
		dropImports: []string{mod + "/app/" + user.Snake},
		addImports: []importSpec{
			{alias: "", path: mod + "/app/services"},
		},
		selectorSwaps: map[string]string{
			user.Snake + "pkg.NewDefaultPasswordGenerator": "services.NewDefaultPasswordGenerator",
		},
	})
}

// TransformMockReverse rewrites a testutil/mocks/<snake>_*_mock.go
// file from feature-package back to layered: the `<snake>pkg` import
// + qualifier flip to `repoInterfaces` / `svcInterfaces` / `services`.
func TransformMockReverse(src []byte, mod string, resource Resource) ([]byte, error) {
	alias := resource.Snake + "pkg"
	return transformCrossCutting(src, crossCuttingOptions{
		dropImports: []string{mod + "/app/" + resource.Snake},
		addImports: []importSpec{
			{alias: "repoInterfaces", path: mod + "/app/repositories/interfaces"},
			{alias: "svcInterfaces", path: mod + "/app/services/interfaces"},
			{alias: "services", path: mod + "/app/services"},
		},
		selectorSwaps: map[string]string{
			alias + "." + resource.Name + "RepositoryInterface": "repoInterfaces." + resource.Name + "RepositoryInterface",
			alias + "." + resource.Name + "ServiceInterface":    "svcInterfaces." + resource.Name + "ServiceInterface",
			alias + ".Err" + resource.Name + "NotDeletable":     "repoInterfaces.Err" + resource.Name + "NotDeletable",
			alias + ".Create" + resource.Name + "Input":         "services.Create" + resource.Name + "Input",
			alias + ".Update" + resource.Name + "Patch":         "services.Update" + resource.Name + "Patch",
			alias + ".List" + resource.Plural + "Filter":        "services.List" + resource.Plural + "Filter",
		},
	})
}

// TransformMock rewrites a testutil/mocks/<snake>_*_mock.go file: the
// mock stays in `package mocks` but the imports flip from the layered
// `<mod>/app/models` + `<mod>/app/repositories/interfaces` (or
// `<mod>/app/services/interfaces`) to a single per-feature alias
// import `<snake>pkg "<mod>/app/<snake>"`, and every selector
// referencing the layered packages is rewritten to point at the
// feature package.
//
// The mock impl is in a separate package from the feature it mocks
// (testify/mock convention), so the references stay as SelectorExprs
// — only the X identifier changes.
func TransformMock(src []byte, mod string, resource Resource) ([]byte, error) {
	alias := resource.Snake + "pkg"
	return transformCrossCutting(src, crossCuttingOptions{
		dropImports: []string{
			// `app/models` is intentionally NOT dropped — under Option B
			// the model stays in package models, so the mock keeps
			// `*models.User` as a cross-package reference.
			mod + "/app/services",
			mod + "/app/repositories/interfaces",
			mod + "/app/services/interfaces",
		},
		addImports: []importSpec{
			{alias: alias, path: mod + "/app/" + resource.Snake},
		},
		selectorSwaps: map[string]string{
			// models.X stays as-is.
			"repoInterfaces." + resource.Name + "RepositoryInterface": alias + "." + resource.Name + "RepositoryInterface",
			"svcInterfaces." + resource.Name + "ServiceInterface":     alias + "." + resource.Name + "ServiceInterface",
			"repoInterfaces.Err" + resource.Name + "NotDeletable":     alias + ".Err" + resource.Name + "NotDeletable",
			// Domain input types live in <feature>/inputs.go in feature
			// layout. The mock methods reference these by name.
			"services.Create" + resource.Name + "Input":  alias + ".Create" + resource.Name + "Input",
			"services.Update" + resource.Name + "Patch":  alias + ".Update" + resource.Name + "Patch",
			"services.List" + resource.Plural + "Filter": alias + ".List" + resource.Plural + "Filter",
		},
	})
}

// TransformCoreProviders rewrites app/di/providers/core.go: replaces
// the `app/services` import + `services.NewDefaultPasswordGenerator`
// reference with the user feature's equivalent. PasswordGenerator
// follows the user feature (only consumer in the bootstrap scaffold).
func TransformCoreProviders(src []byte, mod string, resources []Resource) ([]byte, error) {
	// Only rewrite the password generator binding — the rest of core.go
	// stays valid (it imports app/validators, app/rest/controllers,
	// app/devtools — all still present in feature layout).
	userResource := findResource(resources, "user")
	if userResource == nil {
		return src, nil // no user feature → nothing to rewrite
	}
	return transformCrossCutting(src, crossCuttingOptions{
		dropImports: []string{mod + "/app/services"},
		addImports: []importSpec{
			{alias: userResource.Snake + "pkg", path: mod + "/app/" + userResource.Snake},
		},
		selectorSwaps: map[string]string{
			"services.NewDefaultPasswordGenerator": userResource.Snake + "pkg.NewDefaultPasswordGenerator",
		},
	})
}

// findResource looks up a resource by snake name.
func findResource(resources []Resource, snake string) *Resource {
	for i := range resources {
		if resources[i].Snake == snake {
			return &resources[i]
		}
	}
	return nil
}

// perFeatureImports builds one alias import per resource, e.g.
// `userpkg "<mod>/app/user"`. The alias avoids name collision with
// dst's existing identifier resolution (e.g. when a function param is
// also named `user`).
func perFeatureImports(mod string, resources []Resource) []importSpec {
	out := make([]importSpec, 0, len(resources))
	for _, r := range resources {
		out = append(out, importSpec{
			alias: r.Snake + "pkg",
			path:  fmt.Sprintf("%s/app/%s", mod, r.Snake),
		})
	}
	return out
}

// containerFieldRewrites maps layered RouteConfig/ServiceContainer
// field types to feature equivalents.
//
//	repoInterfaces.UserRepositoryInterface   → userpkg.UserRepositoryInterface
//	svcInterfaces.UserServiceInterface       → userpkg.UserServiceInterface
//	*controllers.UserController              → *userpkg.UserController
//
// Because we keep type names as-is (`UserService` stays `UserService`,
// not collapsed to `Service`), the cross-cutting file only needs to
// rewrite the package qualifier — the suffix is untouched.
func containerFieldRewrites(resources []Resource) map[string]string {
	out := map[string]string{}
	for _, r := range resources {
		alias := r.Snake + "pkg"
		out["repoInterfaces."+r.Name+"RepositoryInterface"] = alias + "." + r.Name + "RepositoryInterface"
		out["svcInterfaces."+r.Name+"ServiceInterface"] = alias + "." + r.Name + "ServiceInterface"
		out["controllers."+r.Name+"Controller"] = alias + "." + r.Name + "Controller"
	}
	return out
}

// indexRoutesFieldRewrites: same as container but only the controller
// field (RouteConfig has no repo/service fields).
func indexRoutesFieldRewrites(resources []Resource) map[string]string {
	out := map[string]string{}
	for _, r := range resources {
		alias := r.Snake + "pkg"
		out["controllers."+r.Name+"Controller"] = alias + "." + r.Name + "Controller"
	}
	return out
}

// indexRoutesCallRewrites: `<R>Routes(api, config.<R>Controller)` →
// `<snake>pkg.RegisterRoutes(api, config.<R>Controller)`.
func indexRoutesCallRewrites(resources []Resource) map[string]string {
	out := map[string]string{}
	for _, r := range resources {
		alias := r.Snake + "pkg"
		out[r.Name+"Routes"] = alias + ".RegisterRoutes"
	}
	return out
}

// wireSelectorSwaps: `providers.<R>Set` → `<snake>pkg.<R>Set`.
func wireSelectorSwaps(resources []Resource) map[string]string {
	out := map[string]string{}
	for _, r := range resources {
		alias := r.Snake + "pkg"
		out["providers."+r.Name+"Set"] = alias + "." + r.Name + "Set"
	}
	return out
}

// importSpec is the minimal info needed to insert an import. Kept as a
// local type to avoid leaking dst details out of featurize's API.
type importSpec struct {
	alias string
	path  string
}

// crossCuttingOptions describes the work for one cross-cutting file
// transformation. All fields are optional — omit any that don't apply.
type crossCuttingOptions struct {
	dropImports   []string          // import paths to drop
	addImports    []importSpec      // imports to add
	fieldRewrites map[string]string // SelectorExpr "X.Y" → "A.B" for field types
	selectorSwaps map[string]string // SelectorExpr "X.Y" → "A.B" in expression position (calls, refs)
	callRewrites  map[string]string // bare Ident "Foo" → SelectorExpr "alias.Bar" in call position
}

// transformCrossCutting drives the four cross-cutting file transforms
func transformCrossCutting(src []byte, opts crossCuttingOptions) ([]byte, error) {
	dec := decorator.NewDecorator(token.NewFileSet())
	file, err := dec.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("featurize: parse: %w", err)
	}

	// Rewrite selectors and calls FIRST so we know which import aliases
	// the swapped references will actually need.
	merged := mergeRewrites(opts.fieldRewrites, opts.selectorSwaps)
	if len(merged) > 0 {
		rewriteSelectors(file, merged)
	}
	if len(opts.callRewrites) > 0 {
		rewriteCallNames(file, opts.callRewrites)
	}

	// Drop targeted imports — but only when no reference to the
	// import's alias survives in the file after the rewrites. The
	// scaffolded wire.go references both `providers.UserSet` (which
	// the swap moves to `userpkg.UserSet`) AND `providers.CoreSet`
	// (which stays). If we dropped the providers import unconditionally
	// the file wouldn't compile.
	if len(opts.dropImports) > 0 {
		for _, p := range opts.dropImports {
			alias := importAliasForPath(file, p)
			if alias != "" && aliasReferenced(file, alias) {
				continue
			}
			dropImportPath(file, p)
		}
	}

	// Add new imports ONLY if their alias is actually referenced in
	// the file after the rewrites. Otherwise the generated file ends
	// up with an unused import that fails to compile.
	//
	// When the importSpec has an empty alias, resolve to the path's
	// last segment (Go's default import name) and check for THAT.
	for _, ai := range opts.addImports {
		checkAlias := ai.alias
		if checkAlias == "" {
			idx := strings.LastIndex(ai.path, "/")
			if idx >= 0 && idx+1 < len(ai.path) {
				checkAlias = ai.path[idx+1:]
			}
		}
		if !aliasReferenced(file, checkAlias) {
			continue
		}
		ensureImport(file, ai.path, ai.alias)
	}

	return renderFile(file)
}

// importAliasForPath returns the alias used to reference the given
// import path in the file — the explicit alias when one is set,
// otherwise the path's last segment. Returns "" if the import is not
