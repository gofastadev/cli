// Package featurize — forward transform (layered → feature).
//
// TransformPerResource is the entry point; everything else in this
// file is a private helper called by the 10-step pipeline:
//
//   1. Rewrite package decl
//   2. Identify collapsable imports
//   3. Walk + rewrite SelectorExprs (collapse to bare, or qualify
//      for external test packages)
//   4. Rewrite dtos import path to app/shared/dtos
//   5. Re-qualify bare shared-alias references
//   6. Drop now-unused collapsable imports
//   6b. Add `<mod>/app/<snake>` import for external test files
//   7. Drop duplicate Err<R>NotDeletable var (repo iface vs services)
//   8. Re-qualify bare shared types (Validator → controllers.Validator)
//   9. Drop orphaned imports (e.g. unused `errors`)
//  10. Rename <R>Routes → RegisterRoutes (routes.go only)

package featurize

import (
	"fmt"
	"go/token"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

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

// replaceSelectorsWithIdent walks the file and substitutes every
// SelectorExpr whose X.Name is the sentinel "" (set by the prior
// inspection pass) with a bare Ident carrying the Sel's name. The
// two-pass approach exists because dst.Inspect doesn't give parent
// context — we can mark the SelectorExpr for replacement in the first
// pass, then surgically replace at every container site in the second.
func replaceSelectorsWithIdent(file *dst.File) {
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
