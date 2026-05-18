// Package featurize — reverse transform (feature → layered).
//
// TransformPerResourceReverse is the entry point; the rest of this
// file is the reverse-direction helpers (symbol map, package-alias
// resolution, self-qualifier dropping, routes-function rename back).
//
// Pipeline:
//
//   1. Rewrite package decl to layered destination
//   2. Build symbol-to-layered-package map for this resource
//   3a. Walk SelectorExprs — flip `<snake>.X` to right alias or bare
//   3b. Walk bare Idents — qualify if cross-package
//   3c. Drop `<mod>/app/<snake>` import
//   4. Drop self-qualifier (controllers.Validator inside package controllers)
//   5. Rename RegisterRoutes back to <R>Routes (routes file only)
//   6. dtos files moving back to `package dtos` drop the shared-dtos
//      qualifier; other destinations flip the import path back
//   7. Drop the same-dir controllers import added during forward
//   8. Add imports the requalification needs

package featurize

import (
	"fmt"
	"go/token"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// reverseSymbolPackage describes which layered package + import path a
// given bare identifier belongs to in the layered layout. Used by the
// reverse transformer to re-qualify bare in-feature references.
type reverseSymbolPackage struct {
	alias   string // package alias to qualify with (e.g. "repoInterfaces")
	pathTPL string // import path template — joined with module path at apply time
}

// buildReverseSymbolMap returns the bare-identifier-to-layered-package
// map for one resource. Keys are the in-feature bare names; values are
// the layered package alias + import path.
func buildReverseSymbolMap(r Resource) map[string]reverseSymbolPackage {
	repoInterfaces := reverseSymbolPackage{alias: "repoInterfaces", pathTPL: "/app/repositories/interfaces"}
	repositories := reverseSymbolPackage{alias: "repositories", pathTPL: "/app/repositories"}
	svcInterfaces := reverseSymbolPackage{alias: "svcInterfaces", pathTPL: "/app/services/interfaces"}
	services := reverseSymbolPackage{alias: "services", pathTPL: "/app/services"}
	controllers := reverseSymbolPackage{alias: "controllers", pathTPL: "/app/rest/controllers"}
	dtos := reverseSymbolPackage{alias: "dtos", pathTPL: "/app/dtos"}

	return map[string]reverseSymbolPackage{
		// repository-interfaces
		r.Name + "RepositoryInterface": repoInterfaces,
		// repository impls
		r.Name + "Repository":         repositories,
		"New" + r.Name + "Repository": repositories,
		// service-interfaces
		r.Name + "ServiceInterface": svcInterfaces,
		// service impls + domain types
		r.Name + "Service":                 services,
		"New" + r.Name + "Service":         services,
		"Create" + r.Name + "Input":        services,
		"Update" + r.Name + "Patch":        services,
		"List" + r.Plural + "Filter":       services,
		"Err" + r.Name + "NotFound":        services,
		"Err" + r.Name + "VersionConflict": services,
		"Err" + r.Name + "NotDeletable":    services,
		"PasswordGenerator":                services,
		"NewDefaultPasswordGenerator":      services,
		"DefaultPasswordGenerator":         services,
		// controllers
		r.Name + "Controller":                 controllers,
		"New" + r.Name + "ControllerInstance": controllers,
		// dtos (per-resource types — moved into feature, going back to dtos)
		r.Name:                                 dtos, // the DTO User type
		"T" + r.Name + "ResponseDto":           dtos,
		"T" + r.Plural + "ResponseDto":         dtos,
		"TCreate" + r.Name + "Dto":             dtos,
		"TUpdate" + r.Name + "Dto":             dtos,
		"TArchive" + r.Name + "Dto":            dtos,
		"TFind" + r.Name + "ByIDDto":           dtos,
		"T" + r.Name + "FiltersQueryParamsDto": dtos,
		"TUpdate" + r.Name + "GraphQLInput":    dtos,
		r.Name + "FromModel":                   dtos,
		r.Plural + "FromModels":                dtos,
	}
}

// TransformPerResourceReverse migrates a single per-resource source
// file from feature-package layout back to its layered location. It's
// the inverse of TransformPerResource:
//
//  1. Package decl: `package <snake>` → `package <layered>` (or
//     `package <layered>_test` for external test files).
//  2. Bare refs to in-feature symbols that belong to OTHER layered
//     packages get qualified — e.g. `UserRepositoryInterface` →
//     `repoInterfaces.UserRepositoryInterface`. Required imports
//     auto-added.
//  3. Refs already qualified by package decl (e.g. `controllers.Validator`
//     when the destination is `package controllers`) shed the
//     redundant self-qualifier.
//  4. `dtos.X` for shared aliases (TPaginationObjectDto etc.) collapses
//     to bare `X` because the destination is `package dtos`. The
//     `<mod>/app/shared/dtos` import flips back to `<mod>/app/dtos`.
//  5. `RegisterRoutes` in routes files renames back to `<R>Routes`.
//
// Caveat: the reverse direction assumes the feature project follows
// scaffold-shaped conventions. Heavy hand-edits (renamed types,
// custom files, alternate folder layouts) may not unwind cleanly —
// in those cases the user should split the migration into smaller
// pieces or manually adjust the result.
//
//nolint:gocognit,gocyclo // 8-step linear pipeline; splitting hides the pipeline order.
func TransformPerResourceReverse(src []byte, dest LayeredDestination, opts Options) ([]byte, error) {
	dec := decorator.NewDecorator(token.NewFileSet())
	file, err := dec.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("featurize reverse: parse: %w", err)
	}

	// Step 1: rewrite package decl to the layered destination.
	file.Name.Name = dest.PackageName

	// Step 2: build the symbol-to-layered-package map for this resource.
	symMap := buildReverseSymbolMap(opts.Resource)

	// Step 3a: walk SelectorExprs first — external test files have
	// `<snake>.X` references (added by forward featurize) that need to
	// flip to `<layered-alias>.X` or collapse to bare `X` depending on
	// the destination package. Same-package services symbols in a
	// `services_test` file collapse to bare; cross-package symbols
	// rewrite to the right alias.
	destAlias := destPackageAlias(dest.PackageName)
	addedImports := map[string]string{}
	applyOnFile(file, func(c *applyCursor) bool {
		sel, ok := c.Node.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*dst.Ident)
		if !ok {
			return true
		}
		if ident.Name != opts.Resource.Snake {
			return true
		}
		// Special case: `<snake>.RegisterRoutes` → `routes.<R>Routes`.
		// Forward featurize renamed the routes function AND qualified
		// the external test's call with the snake alias; reverse must
		// undo both.
		if sel.Sel.Name == "RegisterRoutes" {
			c.Replace(&dst.SelectorExpr{
				X:   &dst.Ident{Name: "routes"},
				Sel: &dst.Ident{Name: opts.Resource.Name + "Routes"},
			})
			addedImports["routes"] = opts.ModulePath + "/app/rest/routes"
			return true
		}
		entry, ok := symMap[sel.Sel.Name]
		if !ok {
			return true
		}
		if entry.alias == destAlias {
			// Same package — collapse to bare.
			c.Replace(&dst.Ident{Name: sel.Sel.Name})
			return true
		}
		// Different package — rewrite the qualifier.
		c.Replace(&dst.SelectorExpr{
			X:   &dst.Ident{Name: entry.alias},
			Sel: &dst.Ident{Name: sel.Sel.Name},
		})
		addedImports[entry.alias] = opts.ModulePath + entry.pathTPL
		return true
	})

	// Step 3b: walk bare Ident references. If the symbol maps to a
	// layered package DIFFERENT from this file's destination, qualify.
	// The destination's own package alias is "" (no qualifier needed).
	applyOnFile(file, func(c *applyCursor) bool {
		ident, ok := c.Node.(*dst.Ident)
		if !ok {
			return true
		}
		entry, ok := symMap[ident.Name]
		if !ok {
			return true
		}
		// Same package: stays bare.
		if entry.alias == destAlias {
			return true
		}
		c.Replace(&dst.SelectorExpr{
			X:   &dst.Ident{Name: entry.alias},
			Sel: &dst.Ident{Name: ident.Name},
		})
		addedImports[entry.alias] = opts.ModulePath + entry.pathTPL
		return true
	})

	// Step 3c: drop the `<mod>/app/<snake>` import — it was added by
	// forward featurize for external test files; after the reverse
	// nothing references it.
	dropImportPath(file, opts.ModulePath+"/app/"+opts.Resource.Snake)

	// Step 4: drop self-qualifier when present — controller.go in
	// `package controllers` with `controllers.Validator` should become
	// bare `Validator`.
	dropSelfQualifier(file, destAlias)

	// Step 5: routes function rename back: `RegisterRoutes` →
	// `<Name>Routes`. Only in the routes file itself.
	if strings.HasSuffix(dest.Path, ".routes.go") {
		renameRegisterRoutesBack(file, opts.Resource.Name)
	}

	// Step 6: dtos files moving back to `package dtos` need their
	// shared-aliases qualifier dropped — `dtos.TPaginationObjectDto`
	// becomes bare `TPaginationObjectDto` again.
	if dest.PackageName == "dtos" {
		dropSelfQualifier(file, "dtos")
		dropImportPath(file, opts.ModulePath+"/app/shared/dtos")
	} else {
		// All other destination packages (controllers, services, etc.)
		// keep the `dtos.` qualifier but the import path needs to flip
		// from `<mod>/app/shared/dtos` back to `<mod>/app/dtos`.
		oldPath := opts.ModulePath + "/app/shared/dtos"
		newPath := opts.ModulePath + "/app/dtos"
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

	// Step 7: drop the `userpkg "<mod>/app/<snake>"`-style imports
	// added by Phase A's requalifyBareSharedTypes pass (Validator
	// qualification) when reversing into the controllers package.
	if destAlias == "controllers" {
		// The feature controller imported `controllers` to reach
		// `controllers.Validator` — that import is now self-referential
		// and must go.
		dropImportPath(file, opts.ModulePath+"/app/rest/controllers")
	}

	// Step 8: add the imports the requalification needs.
	for alias, path := range addedImports {
		// Skip self-imports (would be circular). destAlias is "" for
		// external test packages — they CAN import the same-directory
		// underlying package via its qualifier, so the path-suffix
		// check (`isSelfImport`) is wrong here. The destAlias match
		// is sufficient for true self-references.
		if alias == destAlias {
			continue
		}
		// dtos files re-qualifying services symbols — same as forward,
		// just add the import.
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
			Path: &dst.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("%q", path)},
		}
		// Add an explicit alias when the path's last segment differs
		// from the local alias we use (repoInterfaces vs the path's
		// last segment "interfaces"; svcInterfaces likewise).
		if alias == "repoInterfaces" || alias == "svcInterfaces" {
			newImport.Name = &dst.Ident{Name: alias}
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

	return renderFile(file)
}

// destPackageAlias maps a layered destination package name to the
// alias used to qualify symbols pointing at it. Most map 1:1 to the
// package name; `interfaces` is ambiguous (could be repo or service
// interfaces) and is intentionally returned as "" so cross-package
// refs always qualify.
//
// External test packages (`services_test`, `controllers_test`, etc.)
// are NOT the same as their underlying package — Go treats them as
// separate units that access the underlying package via its qualifier.
func destPackageAlias(pkgName string) string {
	if strings.HasSuffix(pkgName, "_test") {
		return ""
	}
	switch pkgName {
	case "repositories":
		return "repositories"
	case "services":
		return "services"
	case "controllers":
		return "controllers"
	case "dtos":
		return "dtos"
	case "routes":
		return "routes"
	case "providers":
		return "providers"
	case "interfaces":
		// Can't distinguish repo vs service interfaces from package
		// name alone — both layered packages are named `interfaces`.
		// Return "" so symbols always qualify.
		return ""
	}
	return ""
}

// dropSelfQualifier walks the file and drops `X.Y` SelectorExprs
// where X matches the destination package alias — those would be
func dropSelfQualifier(file *dst.File, alias string) {
	if alias == "" {
		return
	}
	applyOnFile(file, func(c *applyCursor) bool {
		sel, ok := c.Node.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*dst.Ident)
		if !ok {
			return true
		}
		if ident.Name == alias {
			c.Replace(&dst.Ident{Name: sel.Sel.Name})
		}
		return true
	})
}

// renameRegisterRoutesBack renames the per-feature `RegisterRoutes`
func renameRegisterRoutesBack(file *dst.File, resourceName string) {
	for _, decl := range file.Decls {
		fd, ok := decl.(*dst.FuncDecl)
		if !ok {
			continue
		}
		if fd.Name.Name == "RegisterRoutes" {
			fd.Name.Name = resourceName + "Routes"
			return
		}
	}
}

// FixSharedDtosImportPathReverse flips `<mod>/app/shared/dtos` back to
// `<mod>/app/dtos` in shared infra files (app_validator.go,
// validator.go, resolver.go) — inverse of FixDtosImportPath.
func FixSharedDtosImportPathReverse(src []byte, mod string) ([]byte, error) {
	dec := decorator.NewDecorator(token.NewFileSet())
	file, err := dec.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("featurize reverse: parse: %w", err)
	}
	oldPath := mod + "/app/shared/dtos"
	newPath := mod + "/app/dtos"
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
	return renderFile(file)
}

// SharedRelocationsReverse mirrors SharedRelocations but inverted:
