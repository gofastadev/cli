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
package featurize

import (
	"bytes"
	"fmt"
	"go/format"
	"go/token"
	"maps"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

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

// collapsablePaths are the layered packages whose selector references
// collapse to bare identifiers when the file moves into the feature
// package. E.g. `services.UserService` → `UserService` because the
// service impl now lives in package <snake> alongside the caller.
//
// The transformer only collapses imports whose path STARTS with the
// project module path + one of these suffixes. Framework imports like
// `github.com/gofastadev/gofasta/pkg/models` are left alone.
//
// Intentionally NOT collapsed:
//
//   - app/models: model types stay in `package models` because DTO
//     mappers in the feature's dtos.go (UserFromModel etc.) need to
//     reference *models.User. The model stays as a cross-package
//     reference from the feature.
//   - app/validators: keeps the shared registration function set
//     (register.go calls isRecordExistByEmailForConflict etc., which
//     are package-private). Scattering per-resource validators would
//     break that wiring.
//
// Treated specially (Option B layout):
//
//   - app/dtos: split into two packages in feature mode. The shared
//     aliases (TPaginationObjectDto, SortOrientation, TCommon* etc.)
//     move to `app/shared/dtos/aliases.go` and stay in `package dtos`.
//     The per-resource DTOs (TCreateUserDto, UserFromModel, TUserResponseDto)
//     move into the feature package at `app/<snake>/dtos.go`. Bare
//     references to shared aliases inside the moved per-resource files
//     are qualified with `dtos.` and the import is rewritten to
//     `<mod>/app/shared/dtos`. Cross-package references from other
//     feature files retain the `dtos.` qualifier with the new path.
var collapsablePaths = []string{
	"/app/dtos",
	"/app/services",
	"/app/services/interfaces",
	"/app/repositories",
	"/app/repositories/interfaces",
	"/app/rest/controllers",
	"/app/rest/routes",
}

// sharedDtoSymbols are the identifiers exported by `app/shared/dtos/
// aliases.go` (formerly `app/dtos/aliases.go`). When the transformer
// collapses `dtos.X` references in feature mode, it keeps these as
// `dtos.X` (only the import path changes) while collapsing every other
// `dtos.Y` (per-resource types) to bare `Y`.
//
// Inside the moved per-resource dtos file these symbols were referenced
// bare (same package); after the move they need re-qualifying with
// `dtos.` since the file is now in the feature package.
var sharedDtoSymbols = map[string]bool{
	"TPaginationInputDto":  true,
	"TSortingInputDto":     true,
	"TPaginationObjectDto": true,
	"TCommonAPIErrorDto":   true,
	"TCommonResponseDto":   true,
	"SortOrientation":      true,
	"SortOrientationAsc":   true,
	"SortOrientationDesc":  true,
}

// TransformPerResource rewrites a single per-resource layered Go source
// file into its feature-package equivalent. Returns the rewritten
// source as bytes (gofmt'd).
//
// Operations:
//
//  1. Package declaration: `package <layered>` → `package <snake>`.
//     Also handles external test packages: `package <layered>_test`
//     → `package <snake>_test`.
//
//  2. Selector collapse: `X.Y` where X resolves to one of the
//     collapsable layered packages becomes just `Y`.
//
//  3. Drop imports that are no longer referenced after the collapse.
//
// The transformer is package-aware via the import block — it doesn't
// guess which identifier means what. An aliased import like
// `repoInterfaces "<mod>/app/repositories/interfaces"` correctly
// collapses every `repoInterfaces.X` reference, regardless of name.
//
//nolint:gocyclo // 10-step linear pipeline; splitting hides the pipeline order.
func TransformPerResource(src []byte, opts Options) ([]byte, error) {
	dec := decorator.NewDecorator(token.NewFileSet())
	file, err := dec.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("featurize: parse: %w", err)
	}

	// Step 1: rewrite package declaration.
	file.Name.Name = rewritePackageName(file.Name.Name, opts.Resource.Snake)

	// Determine whether this is an external test package — `<feature>_test`.
	// External tests can only access exported symbols of the feature
	// package via the package qualifier, not bare. So we route the
	// SelectorExpr collapse differently for these files: rewrite
	// `services.UserService` → `<snake>.UserService` instead of bare.
	isExternalTest := strings.HasSuffix(file.Name.Name, "_test")

	// Step 2: identify which imports are collapsable.
	// collapseAliases maps import alias → true for imports that should
	// be removed and whose selector references should collapse.
	collapseAliases := map[string]bool{}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if !isCollapsablePath(path, opts.ModulePath) {
			continue
		}
		alias := importAlias(imp, path)
		collapseAliases[alias] = true
	}

	// Step 3: walk and rewrite SelectorExprs.
	//
	// Special case for the dtos alias: shared symbols (TPaginationObjectDto
	// etc.) stay qualified as `dtos.X` because they live in the relocated
	// `app/shared/dtos` package; only per-resource symbols collapse.
	//
	// Same-package files (e.g. user/service.go in `package user`)
	// collapse `services.X` → `X`. External test files (e.g.
	// user/controller_test.go in `package user_test`) qualify with the
	// feature package: `services.X` → `<snake>.X`.
	dst.Inspect(file, func(n dst.Node) bool {
		sel, ok := n.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*dst.Ident)
		if !ok {
			return true
		}
		if !collapseAliases[ident.Name] {
			return true
		}
		// Don't collapse shared dtos symbols — they're cross-package
		// references after the move.
		if ident.Name == "dtos" && sharedDtoSymbols[sel.Sel.Name] {
			return true
		}
		if isExternalTest {
			// Qualify with feature package name so external tests can
			// reach the moved symbol: services.X → user.X.
			// Special case: `routes.<Name>Routes(r, c)` becomes
			// `<snake>.RegisterRoutes(r, c)` because the routes func
			// is renamed at move time.
			if ident.Name == "routes" && sel.Sel.Name == opts.Resource.Name+"Routes" {
				sel.Sel.Name = "RegisterRoutes"
			}
			ident.Name = opts.Resource.Snake
			return true
		}
		// Same-package collapse: `<Name>Routes(...)` → `RegisterRoutes(...)`
		// when collapsing routes refs from outside the routes file.
		if ident.Name == "routes" && sel.Sel.Name == opts.Resource.Name+"Routes" {
			sel.Sel.Name = "RegisterRoutes"
		}
		// Mark the X identifier as the bare Y — the parent node will
		// replace this SelectorExpr with the bare Sel ident in the
		// post-process pass below.
		ident.Name = "" // sentinel; handled in Replace pass
		return true
	})

	// We can't replace SelectorExpr with Ident directly inside Inspect
	// without parent context, so do a structural rewrite pass: walk
	// every field/list/expr container and substitute SelectorExpr with
	// sentinel ident.
	replaceSelectorsWithIdent(file)

	// Step 4: handle the dtos package specially. If the file imports
	// `<mod>/app/dtos` AND any reference to `dtos.X` survived the
	// collapse (i.e. X is a shared alias), rewrite the import path to
	// `<mod>/app/shared/dtos`. The package qualifier `dtos.` stays.
	rewriteDtosImportPath(file, opts.ModulePath)

	// Step 5: requalify bare shared-alias references. The per-resource
	// dtos file referenced `TPaginationObjectDto` bare (in-package in
	// layered) — after moving to the feature package, those refs need
	// `dtos.` prefix + the shared-dtos import.
	requalifyBareSharedAliases(file, opts.ModulePath)

	// Step 6: drop now-unused imports. Match by path → drop.
	dropCollapsableImports(file, opts.ModulePath)

	// Step 6b: for external test packages, add `<mod>/app/<snake>`
	// import so the qualified references introduced in Step 3
	// resolve. Skipped for same-package files (they reach symbols
	// in-package).
	if isExternalTest && opts.ModulePath != "" {
		ensureImport(file, opts.ModulePath+"/app/"+opts.Resource.Snake, "")
	}

	// Step 7: drop duplicate var declarations that collide with the
	// canonical version in another feature file. The layered code has
	// Err<Resource>NotDeletable declared in BOTH repositories/interfaces
	// AND services — separate packages, both legitimate. Once collapsed
	// into one package, the two declarations conflict. The service's
	// errors.go is the canonical version (it's documented as the
	// service's domain sentinel error); drop the repo iface duplicate.
	dropDuplicateErrorVars(file, opts.Resource.Name)

	// Step 8: requalify bare references to types that live in shared
	// packages but were bare in the layered scaffold (because the
	// source file was IN that package). Example: `Validator` is defined
	// in app/rest/controllers/validator.go; the layered user.controller.go
	// references it bare. After the move to app/user/, the bare ref
	// becomes undefined — featurize qualifies with `controllers.` and
	// adds the import.
	requalifyBareSharedTypes(file, opts.ModulePath)

	// Step 9: drop now-orphaned imports — primarily the `errors` import
	// left dangling when dropDuplicateErrorVars removed the only
	// `errors.New(...)` call site.
	dropOrphanedImports(file)

	// Step 10: rename the per-resource routes function from
	// `<Name>Routes(r chi.Router, ...)` to `RegisterRoutes(r chi.Router, ...)`.
	// The convention drops the redundant `<Name>` prefix once inside
	// the feature package — and TransformIndexRoutes rewrites the
	// caller to use `<snake>pkg.RegisterRoutes(...)`.
	renameRoutesFunc(file, opts.Resource.Name)

	return renderFile(file)
}

// renameRoutesFunc renames `<R>Routes` (a top-level FuncDecl) to
// `RegisterRoutes`. No-op if the source file doesn't declare the
// function — most per-resource files don't (only routes.go does).
func renameRoutesFunc(file *dst.File, resourceName string) {
	target := resourceName + "Routes"
	for _, decl := range file.Decls {
		fd, ok := decl.(*dst.FuncDecl)
		if !ok {
			continue
		}
		if fd.Name.Name == target {
			fd.Name.Name = "RegisterRoutes"
			return
		}
	}
}

// sharedBareTypes maps bare identifier names to (package alias, import
// path) for types that were referenced bare in layered (because the
// source file was IN that package) but need qualifying when the source
// file moves to a different package.
//
//	`Validator` → controllers.Validator from app/rest/controllers
//
// Add new entries when you observe a "bare reference undefined" build
// failure on a feature scaffold. Each entry costs one Walk over the
// file in Step 8 — keep the list small.
type sharedBareType struct {
	pkgAlias   string
	pathSuffix string
}

var sharedBareTypes = map[string]sharedBareType{
	"Validator": {pkgAlias: "controllers", pathSuffix: "/app/rest/controllers"},
}

func requalifyBareSharedTypes(file *dst.File, mod string) {
	if mod == "" {
		return
	}
	addedAliases := map[string]string{} // alias → fullPath
	applyOnFile(file, func(c *applyCursor) bool {
		ident, ok := c.Node.(*dst.Ident)
		if !ok {
			return true
		}
		entry, ok := sharedBareTypes[ident.Name]
		if !ok {
			return true
		}
		c.Replace(&dst.SelectorExpr{
			X:   &dst.Ident{Name: entry.pkgAlias},
			Sel: &dst.Ident{Name: ident.Name},
		})
		addedAliases[entry.pkgAlias] = mod + entry.pathSuffix
		return true
	})
	for alias, path := range addedAliases {
		alreadyImported := false
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) == path {
				alreadyImported = true
				break
			}
		}
		if alreadyImported {
			continue
		}
		newImport := &dst.ImportSpec{
			Name: &dst.Ident{Name: alias},
			Path: &dst.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("%q", path)},
		}
		file.Imports = append(file.Imports, newImport)
		appended := false
		for _, decl := range file.Decls {
			gd, ok := decl.(*dst.GenDecl)
			if !ok || gd.Tok != token.IMPORT {
				continue
			}
			gd.Specs = append(gd.Specs, newImport)
			appended = true
			break
		}
		if !appended {
			file.Decls = append([]dst.Decl{&dst.GenDecl{
				Tok: token.IMPORT, Specs: []dst.Spec{newImport}, Lparen: true, Rparen: true,
			}}, file.Decls...)
		}
	}
}

// ensureImport adds an import with optional alias if it isn't already
// present in the file.
func ensureImport(file *dst.File, path, alias string) {
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) == path {
			return
		}
	}
	spec := &dst.ImportSpec{
		Path: &dst.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("%q", path)},
	}
	if alias != "" {
		spec.Name = &dst.Ident{Name: alias}
	}
	file.Imports = append(file.Imports, spec)
	for _, decl := range file.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		gd.Specs = append(gd.Specs, spec)
		return
	}
	file.Decls = append([]dst.Decl{&dst.GenDecl{
		Tok: token.IMPORT, Specs: []dst.Spec{spec}, Lparen: true, Rparen: true,
	}}, file.Decls...)
}

// dropOrphanedImports removes imports that have no remaining references
// in the file. Currently scoped to a small allowlist of imports that
// transformer steps are known to leave dangling (e.g. `errors` after
// dropDuplicateErrorVars). A broader unused-import detector would
// require type info; that's overkill for the specific cases we hit.
//
//nolint:gocognit // 4-layer walk (decls → genDecl → specs → ident match) is the natural shape; helpers don't reduce branches.
func dropOrphanedImports(file *dst.File) {
	candidates := map[string]string{
		"errors": "errors", // alias → import path
	}
	for alias, path := range candidates {
		referenced := false
		dst.Inspect(file, func(n dst.Node) bool {
			sel, ok := n.(*dst.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*dst.Ident)
			if !ok {
				return true
			}
			if ident.Name == alias {
				referenced = true
				return false
			}
			return true
		})
		if referenced {
			continue
		}
		// Drop the import.
		keptImports := make([]*dst.ImportSpec, 0, len(file.Imports))
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) == path {
				continue
			}
			keptImports = append(keptImports, imp)
		}
		file.Imports = keptImports
		for _, decl := range file.Decls {
			gd, ok := decl.(*dst.GenDecl)
			if !ok || gd.Tok != token.IMPORT {
				continue
			}
			kept := make([]dst.Spec, 0, len(gd.Specs))
			for _, spec := range gd.Specs {
				is, ok := spec.(*dst.ImportSpec)
				if !ok {
					kept = append(kept, spec)
					continue
				}
				if strings.Trim(is.Path.Value, `"`) == path {
					continue
				}
				kept = append(kept, is)
			}
			gd.Specs = kept
		}
	}
}

// rewriteDtosImportPath flips `<mod>/app/dtos` to `<mod>/app/shared/dtos`
// IF the file still references `dtos.X` after the collapse (i.e. the
// references are shared aliases that didn't collapse). When no such
// reference remains, the import is just dropped by dropCollapsableImports.
func rewriteDtosImportPath(file *dst.File, mod string) {
	if mod == "" {
		return
	}
	hasDtosRef := false
	dst.Inspect(file, func(n dst.Node) bool {
		sel, ok := n.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*dst.Ident)
		if !ok {
			return true
		}
		if ident.Name == "dtos" {
			hasDtosRef = true
			return false
		}
		return true
	})
	if !hasDtosRef {
		return
	}
	oldPath := mod + "/app/dtos"
	newPath := mod + "/app/shared/dtos"
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) == oldPath {
			imp.Path.Value = fmt.Sprintf("%q", newPath)
		}
	}
	for _, decl := range file.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		for _, spec := range gd.Specs {
			is, ok := spec.(*dst.ImportSpec)
			if !ok {
				continue
			}
			if strings.Trim(is.Path.Value, `"`) == oldPath {
				is.Path.Value = fmt.Sprintf("%q", newPath)
			}
		}
	}
}

// requalifyBareSharedAliases walks the file for bare Ident nodes whose
// name matches a shared dtos alias, and converts them into
// `dtos.<Name>` SelectorExprs. Used when a per-resource dtos file
// (which referenced these symbols bare in layered) moves into the
// feature package. Adds the `<mod>/app/shared/dtos` import if any
// requalification happens.
func requalifyBareSharedAliases(file *dst.File, mod string) {
	if mod == "" {
		return
	}
	addedImport := false
	applyOnFile(file, func(c *applyCursor) bool {
		ident, ok := c.Node.(*dst.Ident)
		if !ok {
			return true
		}
		if !sharedDtoSymbols[ident.Name] {
			return true
		}
		c.Replace(&dst.SelectorExpr{
			X:   &dst.Ident{Name: "dtos"},
			Sel: &dst.Ident{Name: ident.Name},
		})
		addedImport = true
		return true
	})
	if !addedImport {
		return
	}
	// Add `<mod>/app/shared/dtos` import if not already present.
	target := mod + "/app/shared/dtos"
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) == target {
			return
		}
	}
	newImport := &dst.ImportSpec{
		Path: &dst.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("%q", target)},
	}
	file.Imports = append(file.Imports, newImport)
	for _, decl := range file.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		gd.Specs = append(gd.Specs, newImport)
		return
	}
	file.Decls = append([]dst.Decl{&dst.GenDecl{
		Tok: token.IMPORT, Specs: []dst.Spec{newImport}, Lparen: true, Rparen: true,
	}}, file.Decls...)
}

// dropDuplicateErrorVars removes the `var ErrXNotDeletable = errors.New(...)`
// declaration that the layered repository_iface.go ships as a re-export
// for callers that import `repoInterfaces`. In feature mode the same
// variable is declared in errors.go, so the duplicate must go.
//
//nolint:gocognit,gocyclo // small dispatch over GenDecl/ValueSpec/Names; flattening adds indirection without simplifying control flow.
func dropDuplicateErrorVars(file *dst.File, resourceName string) {
	target := "Err" + resourceName + "NotDeletable"
	// Only drop the duplicate when this file is ALSO importing
	// "errors" AND defines an interface — the heuristic identifies a
	// repository_iface.go (the source of the duplicate). The service
	// errors.go has no interface decl.
	hasInterface := false
	for _, decl := range file.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*dst.TypeSpec)
			if !ok {
				continue
			}
			if _, ok := ts.Type.(*dst.InterfaceType); ok {
				hasInterface = true
				break
			}
		}
	}
	if !hasInterface {
		return
	}
	keptDecls := make([]dst.Decl, 0, len(file.Decls))
	for _, decl := range file.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.VAR {
			keptDecls = append(keptDecls, decl)
			continue
		}
		// Filter specs: drop the matching name.
		filteredSpecs := make([]dst.Spec, 0, len(gd.Specs))
		for _, spec := range gd.Specs {
			vs, ok := spec.(*dst.ValueSpec)
			if !ok {
				filteredSpecs = append(filteredSpecs, spec)
				continue
			}
			drop := false
			for _, n := range vs.Names {
				if n.Name == target {
					drop = true
					break
				}
			}
			if !drop {
				filteredSpecs = append(filteredSpecs, spec)
			}
		}
		if len(filteredSpecs) == 0 {
			continue // drop the whole GenDecl
		}
		gd.Specs = filteredSpecs
		keptDecls = append(keptDecls, gd)
	}
	file.Decls = keptDecls
}

// rewritePackageName maps a layered package decl to its feature
// equivalent. Handles both regular and `_test` external-test packages.
func rewritePackageName(layered, snake string) string {
	for _, pkg := range layeredPackageNames {
		if layered == pkg {
			return snake
		}
		if layered == pkg+"_test" {
			return snake + "_test"
		}
	}
	// Already feature-shaped (idempotent) or unrelated — leave alone.
	return layered
}

// layeredPackageNames is the closed set of layer package names we
// recognize. Adding a layer to the project structure requires adding
// it here (and to collapsablePaths).
var layeredPackageNames = []string{
	"models",
	"dtos",
	"repositories",
	"interfaces", // matches both repositories/interfaces and services/interfaces
	"services",
	"controllers",
	"routes",
	"validators",
	"providers",
}

// isCollapsablePath reports whether the import path points at one of
// the project's layered packages (vs. a framework or third-party path).
// The match anchors on `<mod>` + collapsable suffix to avoid false
// positives on framework paths that happen to end in "/services" etc.
func isCollapsablePath(path, mod string) bool {
	if mod == "" {
		return false
	}
	for _, suffix := range collapsablePaths {
		if path == mod+suffix {
			return true
		}
	}
	return false
}

// importAlias returns the local name an import is referenced by inside
// the file — the explicit alias when one is set, otherwise the last
// segment of the path. Mirrors how Go's compiler resolves identifiers.
func importAlias(imp *dst.ImportSpec, path string) string {
	if imp.Name != nil && imp.Name.Name != "" && imp.Name.Name != "_" {
		return imp.Name.Name
	}
	idx := strings.LastIndex(path, "/")
	if idx == -1 {
		return path
	}
	return path[idx+1:]
}

// replaceSelectorsWithIdent walks the file and substitutes every
// SelectorExpr whose X.Name is the sentinel "" (set by the prior
// inspection pass) with a bare Ident carrying the Sel's name.
//
// The two-pass approach exists because dst.Inspect doesn't give parent
// context — we can mark the SelectorExpr for replacement in the first
// pass, then surgically replace at every container site in the second.
func replaceSelectorsWithIdent(file *dst.File) {
	replaceInExprList := func(list []dst.Expr) {
		for i, expr := range list {
			if sel, ok := expr.(*dst.SelectorExpr); ok {
				if ident, ok := sel.X.(*dst.Ident); ok && ident.Name == "" {
					list[i] = &dst.Ident{Name: sel.Sel.Name}
				}
			}
		}
	}
	_ = replaceInExprList
	// The Apply API gives parent context — use it to replace the
	// SelectorExpr in any slot a Node can sit in.
	post := func(c *applyCursor) bool {
		sel, ok := c.Node.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*dst.Ident)
		if !ok {
			return true
		}
		if ident.Name != "" {
			return true
		}
		// Replace the SelectorExpr in-place with a bare Ident.
		c.Replace(&dst.Ident{Name: sel.Sel.Name})
		return true
	}
	applyOnFile(file, post)
}

// dropCollapsableImports removes the import block entries that point at
// the project's layered packages — after the selector collapse pass,
// they're unused.
//
// Imports are dropped from both file.Imports and the import GenDecl's
// Specs slice so the rendered output omits the line entirely. dst's
// gofmt pass cleans up an empty import block to `import ()` or removes
// it; format.Source then drops the empty block.
func dropCollapsableImports(file *dst.File, mod string) {
	keptImports := make([]*dst.ImportSpec, 0, len(file.Imports))
	dropPaths := map[string]bool{}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if isCollapsablePath(path, mod) {
			dropPaths[path] = true
			continue
		}
		keptImports = append(keptImports, imp)
	}
	file.Imports = keptImports

	// Walk file.Decls and rebuild any import GenDecl, dropping the
	// matching specs.
	for _, decl := range file.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		kept := make([]dst.Spec, 0, len(gd.Specs))
		for _, spec := range gd.Specs {
			is, ok := spec.(*dst.ImportSpec)
			if !ok {
				kept = append(kept, spec)
				continue
			}
			path := strings.Trim(is.Path.Value, `"`)
			if dropPaths[path] {
				continue
			}
			kept = append(kept, is)
		}
		gd.Specs = kept
	}
}

// renderFile restores a dst.File to []byte and runs gofmt. Mirrors
// astpatch.Render's contract but inlined to keep this package free of
// the astpatch dependency (avoids an import cycle if astpatch ever
// imports featurize).
func renderFile(file *dst.File) ([]byte, error) {
	var buf bytes.Buffer
	if err := decorator.NewRestorer().Fprint(&buf, file); err != nil {
		return nil, fmt.Errorf("featurize: restore: %w", err)
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		// Return un-formatted source rather than failing — downstream
		// callers can still inspect the output, and the failure is
		// usually a transient gofmt input quirk.
		return buf.Bytes(), nil //nolint:nilerr // intentional fallback
	}
	return out, nil
}

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
func SharedRelocations() []PathPair {
	return []PathPair{
		{Layered: "app/dtos/aliases.go", Feature: "app/shared/dtos/aliases.go"},
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

// ---------- Cross-cutting per-layout file transformations ----------

// TransformContainer rewrites app/di/container.go: replaces the
// layered cross-package imports (repoInterfaces, svcInterfaces,
// controllers) with per-feature alias imports, and rewrites every
// resource's field types to point at the feature packages.
func TransformContainer(src []byte, mod string, resources []Resource) ([]byte, error) {
	return transformCrossCutting(src, mod, resources, crossCuttingOptions{
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
	return transformCrossCutting(src, mod, resources, crossCuttingOptions{
		dropImports:   []string{mod + "/app/di/providers"},
		addImports:    perFeatureImports(mod, resources),
		selectorSwaps: wireSelectorSwaps(resources),
	})
}

// TransformIndexRoutes rewrites app/rest/routes/index.routes.go:
// replaces `<R>Routes(api, ...)` with `<snake>pkg.RegisterRoutes(api, ...)`
// and updates the controller field types in RouteConfig.
func TransformIndexRoutes(src []byte, mod string, resources []Resource) ([]byte, error) {
	return transformCrossCutting(src, mod, resources, crossCuttingOptions{
		dropImports:   []string{mod + "/app/rest/controllers"},
		addImports:    perFeatureImports(mod, resources),
		fieldRewrites: indexRoutesFieldRewrites(resources),
		callRewrites:  indexRoutesCallRewrites(resources),
	})
}

// TransformResourceDTO rewrites a per-resource dtos file (e.g.
// app/dtos/user.dtos.go) which STAYS in the dtos package in feature
// layout. The file imports the model and service packages — those have
// moved to the feature package, so the imports and selectors need to
// flip:
//
//	models.User          → userpkg.User
//	services.CreateUserInput → userpkg.CreateUserInput
//	services.UpdateUserPatch → userpkg.UpdateUserPatch
//	services.ListUsersFilter → userpkg.ListUsersFilter
//
// Same applies to validator files staying in app/validators/ that
// reference per-feature types (rare in the user scaffold but possible
// for resources that build cross-resource validators).
func TransformResourceDTO(src []byte, mod string, resource Resource) ([]byte, error) {
	alias := resource.Snake + "pkg"
	return transformCrossCutting(src, mod, []Resource{resource}, crossCuttingOptions{
		dropImports: []string{
			mod + "/app/models",
			mod + "/app/services",
		},
		addImports: []importSpec{
			{alias: alias, path: mod + "/app/" + resource.Snake},
		},
		selectorSwaps: map[string]string{
			"models." + resource.Name:                    alias + "." + resource.Name,
			"services.Create" + resource.Name + "Input":  alias + ".Create" + resource.Name + "Input",
			"services.Update" + resource.Name + "Patch":  alias + ".Update" + resource.Name + "Patch",
			"services.List" + resource.Plural + "Filter": alias + ".List" + resource.Plural + "Filter",
		},
	})
}

// FixDtosImportPath rewrites a file's `<mod>/app/dtos` import to
// `<mod>/app/shared/dtos`. No-op if the file doesn't import dtos.
// Used by shared infra files (app/validators/app_validator.go,
// app/rest/controllers/validator.go, etc.) that stay in their layered
// location but reference the shared dtos package which has moved.
func FixDtosImportPath(src []byte, mod string) ([]byte, error) {
	dec := decorator.NewDecorator(token.NewFileSet())
	file, err := dec.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("featurize: parse: %w", err)
	}
	rewriteDtosImportPath(file, mod)
	return renderFile(file)
}

// TransformMock rewrites a testutil/mocks/<snake>_*_mock.go file: the
// mock stays in `package mocks` but the imports flip from the layered
// `<mod>/app/models` + `<mod>/app/repositories/interfaces` (or
// `<mod>/app/services/interfaces`) to a single per-feature alias
// import `<snake>pkg "<mod>/app/<snake>"`, and every selector
// referencing the layered packages is rewritten to point at the
// feature package.
//
//	models.User                          → userpkg.User
//	repoInterfaces.UserRepositoryInterface → userpkg.UserRepositoryInterface
//	svcInterfaces.UserServiceInterface     → userpkg.UserServiceInterface
//	repoInterfaces.ErrUserNotDeletable     → userpkg.ErrUserNotDeletable
//
// The mock impl is in a separate package from the feature it mocks
// (testify/mock convention), so the references stay as SelectorExprs
// — only the X identifier changes.
func TransformMock(src []byte, mod string, resource Resource) ([]byte, error) {
	alias := resource.Snake + "pkg"
	return transformCrossCutting(src, mod, []Resource{resource}, crossCuttingOptions{
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
	return transformCrossCutting(src, mod, resources, crossCuttingOptions{
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
// from one place. The four use different combinations of the same
// primitives (import drop/add, selector rewrite, call rewrite).
func transformCrossCutting(src []byte, mod string, _ []Resource, opts crossCuttingOptions) ([]byte, error) {
	_ = mod
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
	for _, ai := range opts.addImports {
		if !aliasReferenced(file, ai.alias) {
			continue
		}
		ensureImport(file, ai.path, ai.alias)
	}

	return renderFile(file)
}

// importAliasForPath returns the alias used to reference the given
// import path in the file — the explicit alias when one is set,
// otherwise the path's last segment. Returns "" if the import is not
// present.
func importAliasForPath(file *dst.File, path string) string {
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) != path {
			continue
		}
		return importAlias(imp, path)
	}
	return ""
}

// dropImportPath removes an import entry by path from both file.Imports
// and the underlying import GenDecl.
func dropImportPath(file *dst.File, path string) {
	keptImports := make([]*dst.ImportSpec, 0, len(file.Imports))
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) == path {
			continue
		}
		keptImports = append(keptImports, imp)
	}
	file.Imports = keptImports
	for _, decl := range file.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		kept := make([]dst.Spec, 0, len(gd.Specs))
		for _, spec := range gd.Specs {
			is, ok := spec.(*dst.ImportSpec)
			if !ok {
				kept = append(kept, spec)
				continue
			}
			if strings.Trim(is.Path.Value, `"`) == path {
				continue
			}
			kept = append(kept, is)
		}
		gd.Specs = kept
	}
}

// aliasReferenced reports whether any SelectorExpr in the file has its
// X identifier equal to alias.
func aliasReferenced(file *dst.File, alias string) bool {
	found := false
	dst.Inspect(file, func(n dst.Node) bool {
		sel, ok := n.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*dst.Ident)
		if !ok {
			return true
		}
		if ident.Name == alias {
			found = true
			return false
		}
		return true
	})
	return found
}

func mergeRewrites(a, b map[string]string) map[string]string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make(map[string]string, len(a)+len(b))
	maps.Copy(out, a)
	maps.Copy(out, b)
	return out
}

// rewriteSelectors walks the file and rewrites every SelectorExpr that
// matches "X.Y" in the rewrites map. The replacement is itself a
// SelectorExpr "A.B" parsed from the right-hand side.
func rewriteSelectors(file *dst.File, rewrites map[string]string) {
	applyOnFile(file, func(c *applyCursor) bool {
		sel, ok := c.Node.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*dst.Ident)
		if !ok {
			return true
		}
		key := ident.Name + "." + sel.Sel.Name
		repl, ok := rewrites[key]
		if !ok {
			return true
		}
		dot := strings.LastIndex(repl, ".")
		if dot == -1 {
			c.Replace(&dst.Ident{Name: repl})
			return true
		}
		c.Replace(&dst.SelectorExpr{
			X:   &dst.Ident{Name: repl[:dot]},
			Sel: &dst.Ident{Name: repl[dot+1:]},
		})
		return true
	})
}

// rewriteCallNames walks the file and rewrites every bare Ident that
// matches the rewrites map and is in CALLEE position
// (i.e. CallExpr.Fun) into a SelectorExpr. Used for the index.routes
// transform where `UserRoutes(...)` → `userpkg.RegisterRoutes(...)`.
func rewriteCallNames(file *dst.File, rewrites map[string]string) {
	applyOnFile(file, func(c *applyCursor) bool {
		call, ok := c.Node.(*dst.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*dst.Ident)
		if !ok {
			return true
		}
		repl, ok := rewrites[ident.Name]
		if !ok {
			return true
		}
		dot := strings.LastIndex(repl, ".")
		if dot == -1 {
			call.Fun = &dst.Ident{Name: repl}
			return true
		}
		call.Fun = &dst.SelectorExpr{
			X:   &dst.Ident{Name: repl[:dot]},
			Sel: &dst.Ident{Name: repl[dot+1:]},
		}
		return true
	})
}

// ---------- dstutil.Apply mini-wrapper ----------
//
// dst doesn't ship dstutil.Apply (yet) — github.com/dave/dst/dstutil
// exists but the parent-context cursor isn't part of dst v0.27.3.
// Instead we use a hand-rolled walker that gives us a Replace seam at
// every Expr container.

type applyCursor struct {
	Node    dst.Node
	replace func(dst.Node)
}

// Replace substitutes the current node with the given node via the
// cursor's parent-aware setter.
func (c *applyCursor) Replace(n dst.Node) {
	if c.replace != nil {
		c.replace(n)
	}
}

// applyOnFile walks every meaningful node in the file (declarations,
// expressions, statements) and invokes fn with a cursor at each.
// Replacements landed via cursor.Replace are written back at the
// parent slot they belong to.
//
// Coverage: function declarations + their bodies, top-level var/const
// initializer expressions, struct field types, import specs. Anything
// useful for featurize. Doesn't try to cover every dst node shape —
// extends as needed.
func applyOnFile(file *dst.File, fn func(*applyCursor) bool) {
	for _, decl := range file.Decls {
		applyOnDecl(decl, fn)
	}
}

func applyOnDecl(decl dst.Decl, fn func(*applyCursor) bool) {
	switch d := decl.(type) {
	case *dst.FuncDecl:
		if d.Type != nil {
			applyOnFuncType(d.Type, fn)
		}
		if d.Body != nil {
			applyOnStmtList(d.Body.List, fn)
		}
	case *dst.GenDecl:
		for _, spec := range d.Specs {
			applyOnSpec(spec, fn)
		}
	}
}

func applyOnSpec(spec dst.Spec, fn func(*applyCursor) bool) {
	switch s := spec.(type) {
	case *dst.ValueSpec:
		for i := range s.Values {
			applyOnExpr(&s.Values[i], fn)
		}
		for i := range s.Names {
			_ = s.Names[i] // identifiers aren't selectors
		}
		if s.Type != nil {
			applyOnExpr(&s.Type, fn)
		}
	case *dst.TypeSpec:
		if s.Type != nil {
			applyOnExpr(&s.Type, fn)
		}
	}
}

func applyOnFuncType(ft *dst.FuncType, fn func(*applyCursor) bool) {
	if ft.Params != nil {
		applyOnFieldList(ft.Params, fn)
	}
	if ft.Results != nil {
		applyOnFieldList(ft.Results, fn)
	}
}

func applyOnFieldList(fl *dst.FieldList, fn func(*applyCursor) bool) {
	for _, f := range fl.List {
		if f.Type != nil {
			applyOnExpr(&f.Type, fn)
		}
	}
}

func applyOnStmtList(stmts []dst.Stmt, fn func(*applyCursor) bool) {
	for i := range stmts {
		applyOnStmt(&stmts[i], fn)
	}
}

//nolint:gocognit,gocyclo,gocritic // ptrToRefParam: the *dst.Stmt is intentional so callers can replace the statement in-place via *s = ...
func applyOnStmt(s *dst.Stmt, fn func(*applyCursor) bool) {
	switch st := (*s).(type) {
	case *dst.ExprStmt:
		applyOnExpr(&st.X, fn)
	case *dst.AssignStmt:
		for i := range st.Lhs {
			applyOnExpr(&st.Lhs[i], fn)
		}
		for i := range st.Rhs {
			applyOnExpr(&st.Rhs[i], fn)
		}
	case *dst.ReturnStmt:
		for i := range st.Results {
			applyOnExpr(&st.Results[i], fn)
		}
	case *dst.IfStmt:
		if st.Init != nil {
			applyOnStmt(&st.Init, fn)
		}
		if st.Cond != nil {
			applyOnExpr(&st.Cond, fn)
		}
		if st.Body != nil {
			applyOnStmtList(st.Body.List, fn)
		}
		if st.Else != nil {
			applyOnStmt(&st.Else, fn)
		}
	case *dst.ForStmt:
		if st.Init != nil {
			applyOnStmt(&st.Init, fn)
		}
		if st.Cond != nil {
			applyOnExpr(&st.Cond, fn)
		}
		if st.Post != nil {
			applyOnStmt(&st.Post, fn)
		}
		if st.Body != nil {
			applyOnStmtList(st.Body.List, fn)
		}
	case *dst.RangeStmt:
		if st.Key != nil {
			applyOnExpr(&st.Key, fn)
		}
		if st.Value != nil {
			applyOnExpr(&st.Value, fn)
		}
		if st.X != nil {
			applyOnExpr(&st.X, fn)
		}
		if st.Body != nil {
			applyOnStmtList(st.Body.List, fn)
		}
	case *dst.SwitchStmt:
		if st.Init != nil {
			applyOnStmt(&st.Init, fn)
		}
		if st.Tag != nil {
			applyOnExpr(&st.Tag, fn)
		}
		if st.Body != nil {
			for _, c := range st.Body.List {
				if cc, ok := c.(*dst.CaseClause); ok {
					for i := range cc.List {
						applyOnExpr(&cc.List[i], fn)
					}
					applyOnStmtList(cc.Body, fn)
				}
			}
		}
	case *dst.TypeSwitchStmt:
		if st.Init != nil {
			applyOnStmt(&st.Init, fn)
		}
		if st.Assign != nil {
			applyOnStmt(&st.Assign, fn)
		}
		if st.Body != nil {
			for _, c := range st.Body.List {
				if cc, ok := c.(*dst.CaseClause); ok {
					for i := range cc.List {
						applyOnExpr(&cc.List[i], fn)
					}
					applyOnStmtList(cc.Body, fn)
				}
			}
		}
	case *dst.BlockStmt:
		applyOnStmtList(st.List, fn)
	case *dst.DeferStmt:
		if st.Call != nil {
			expr := dst.Expr(st.Call)
			applyOnExpr(&expr, fn)
			st.Call = expr.(*dst.CallExpr)
		}
	case *dst.GoStmt:
		if st.Call != nil {
			expr := dst.Expr(st.Call)
			applyOnExpr(&expr, fn)
			st.Call = expr.(*dst.CallExpr)
		}
	case *dst.DeclStmt:
		if st.Decl != nil {
			applyOnDecl(st.Decl, fn)
		}
	case *dst.SendStmt:
		applyOnExpr(&st.Chan, fn)
		applyOnExpr(&st.Value, fn)
	case *dst.IncDecStmt:
		applyOnExpr(&st.X, fn)
	case *dst.LabeledStmt:
		applyOnStmt(&st.Stmt, fn)
	case *dst.SelectStmt:
		if st.Body != nil {
			for _, c := range st.Body.List {
				if cc, ok := c.(*dst.CommClause); ok {
					if cc.Comm != nil {
						applyOnStmt(&cc.Comm, fn)
					}
					applyOnStmtList(cc.Body, fn)
				}
			}
		}
	}
}

//nolint:gocognit,gocyclo,gocritic // ptrToRefParam: the *dst.Expr is intentional so callers can replace the expression in-place via *e = ...
func applyOnExpr(e *dst.Expr, fn func(*applyCursor) bool) {
	if *e == nil {
		return
	}
	// Recurse into children first (post-order rewrite).
	switch n := (*e).(type) {
	case *dst.SelectorExpr:
		applyOnExpr(&n.X, fn)
	case *dst.CallExpr:
		applyOnExpr(&n.Fun, fn)
		for i := range n.Args {
			applyOnExpr(&n.Args[i], fn)
		}
	case *dst.UnaryExpr:
		applyOnExpr(&n.X, fn)
	case *dst.BinaryExpr:
		applyOnExpr(&n.X, fn)
		applyOnExpr(&n.Y, fn)
	case *dst.IndexExpr:
		applyOnExpr(&n.X, fn)
		applyOnExpr(&n.Index, fn)
	case *dst.SliceExpr:
		applyOnExpr(&n.X, fn)
		if n.Low != nil {
			applyOnExpr(&n.Low, fn)
		}
		if n.High != nil {
			applyOnExpr(&n.High, fn)
		}
		if n.Max != nil {
			applyOnExpr(&n.Max, fn)
		}
	case *dst.TypeAssertExpr:
		applyOnExpr(&n.X, fn)
		if n.Type != nil {
			applyOnExpr(&n.Type, fn)
		}
	case *dst.ParenExpr:
		applyOnExpr(&n.X, fn)
	case *dst.StarExpr:
		applyOnExpr(&n.X, fn)
	case *dst.CompositeLit:
		if n.Type != nil {
			applyOnExpr(&n.Type, fn)
		}
		for i := range n.Elts {
			applyOnExpr(&n.Elts[i], fn)
		}
	case *dst.KeyValueExpr:
		// Skip walking a bare-Ident Key — in struct literals the Key
		// is a field NAME, not an identifier reference. Rewriting it
		// would emit `dtos.SortOrientation: &asc` which is invalid
		// (`invalid field name X.Y in struct literal`).
		if _, isIdent := n.Key.(*dst.Ident); !isIdent {
			applyOnExpr(&n.Key, fn)
		}
		applyOnExpr(&n.Value, fn)
	case *dst.FuncLit:
		if n.Type != nil {
			applyOnFuncType(n.Type, fn)
		}
		if n.Body != nil {
			applyOnStmtList(n.Body.List, fn)
		}
	case *dst.ArrayType:
		if n.Len != nil {
			applyOnExpr(&n.Len, fn)
		}
		applyOnExpr(&n.Elt, fn)
	case *dst.MapType:
		applyOnExpr(&n.Key, fn)
		applyOnExpr(&n.Value, fn)
	case *dst.ChanType:
		applyOnExpr(&n.Value, fn)
	case *dst.StructType:
		if n.Fields != nil {
			applyOnFieldList(n.Fields, fn)
		}
	case *dst.InterfaceType:
		if n.Methods != nil {
			for _, m := range n.Methods.List {
				if m.Type != nil {
					applyOnExpr(&m.Type, fn)
				}
			}
		}
	case *dst.FuncType:
		applyOnFuncType(n, fn)
	}
	// Visit the current node.
	c := &applyCursor{Node: *e, replace: func(n dst.Node) {
		if expr, ok := n.(dst.Expr); ok {
			*e = expr
		}
	}}
	fn(c)
}
