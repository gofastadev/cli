// Package featurize — GraphQL resolver-file transformations.
//
// GraphQL files never MOVE between layouts: app/graphql/ is a shared
// directory in both the layered and the feature shape (gqlgen owns the
// resolver directory and generates every `{name}.resolvers.go` into it,
// so splitting the files across feature packages would fight the
// generator). What changes between layouts is where the symbols the
// resolvers reference live:
//
//   - per-resource DTOs, service interfaces, domain inputs/filters and
//     error sentinels move into `app/<snake>/` (package <snake>)
//   - shared dtos aliases and gqlgen-generated model types move from
//     `app/dtos` to `app/shared/dtos` (package name stays `dtos`)
//
// So the transform is a re-qualification, not a relocation: every
// `.go` file under app/graphql/resolvers/ keeps `package resolvers`
// and its path, and only its selectors + imports are rewritten. The
// files must NOT go through TransformPerResource — that transform
// collapses selectors to bare identifiers, which is only correct for
// files that move INTO the feature package.
package featurize

import (
	"fmt"
	"go/token"
	"strings"

	"github.com/dave/dst/decorator"
)

// perResourceDtoSymbolNames lists the identifiers exported by a
// resource's hand-written dtos file (app/dtos/<snake>.dtos.go in
// layered, app/<snake>/dtos.go in feature). This is the single source
// of truth shared by the forward GraphQL transform and
// buildReverseSymbolMap — keeping them on one list means the forward
// and reverse tables cannot drift apart.
func perResourceDtoSymbolNames(r Resource) []string {
	return []string{
		r.Name, // the DTO type itself ("User")
		"T" + r.Name + "ResponseDto",
		"T" + r.Plural + "ResponseDto",
		"TCreate" + r.Name + "Dto",
		"TUpdate" + r.Name + "Dto",
		"TArchive" + r.Name + "Dto",
		"TFind" + r.Name + "ByIDDto",
		"T" + r.Name + "FiltersQueryParamsDto",
		"TUpdate" + r.Name + "GraphQLInput",
		r.Name + "FromModel",
		r.Plural + "FromModels",
	}
}

// perResourceServiceSymbolNames lists the identifiers exported by a
// resource's service layer (app/services/ in layered, app/<snake>/ in
// feature) that resolver files plausibly reference: error sentinels,
// domain inputs and the list filter.
func perResourceServiceSymbolNames(r Resource) []string {
	return []string{
		"Err" + r.Name + "NotFound",
		"Err" + r.Name + "VersionConflict",
		"Err" + r.Name + "NotDeletable",
		"Create" + r.Name + "Input",
		"Update" + r.Name + "Patch",
		"List" + r.Plural + "Filter",
	}
}

// graphqlForwardSwaps builds the layered→feature selector map for the
// resolver files: `dtos.X` / `services.X` / `svcInterfaces.X` for
// per-resource symbols become `<snake>pkg.X`. Shared dtos symbols are
// deliberately absent — they keep the `dtos.` qualifier and only the
// import path flips.
func graphqlForwardSwaps(resources []Resource) map[string]string {
	out := map[string]string{}
	for _, r := range resources {
		alias := r.Snake + "pkg"
		out["svcInterfaces."+r.Name+"ServiceInterface"] = alias + "." + r.Name + "ServiceInterface"
		for _, sym := range perResourceServiceSymbolNames(r) {
			out["services."+sym] = alias + "." + sym
		}
		for _, sym := range perResourceDtoSymbolNames(r) {
			out["dtos."+sym] = alias + "." + sym
		}
	}
	return out
}

// graphqlReverseSwaps is the exact inverse of graphqlForwardSwaps.
func graphqlReverseSwaps(resources []Resource) map[string]string {
	out := map[string]string{}
	for _, r := range resources {
		alias := r.Snake + "pkg"
		out[alias+"."+r.Name+"ServiceInterface"] = "svcInterfaces." + r.Name + "ServiceInterface"
		for _, sym := range perResourceServiceSymbolNames(r) {
			out[alias+"."+sym] = "services." + sym
		}
		for _, sym := range perResourceDtoSymbolNames(r) {
			out[alias+"."+sym] = "dtos." + sym
		}
	}
	return out
}

// TransformGraphQL rewrites one `.go` file under app/graphql/resolvers/
// from the layered to the feature shape. The file keeps its path and
// `package resolvers`; only selectors and imports change:
//
//  1. Per-resource references re-qualify to the feature package:
//     `dtos.TCreateUserDto` → `userpkg.TCreateUserDto`,
//     `services.ErrUserNotFound` → `userpkg.ErrUserNotFound`,
//     `svcInterfaces.UserServiceInterface` → `userpkg.UserServiceInterface`.
//  2. Surviving `dtos.X` references (shared aliases and gqlgen-generated
//     types) keep their qualifier; the `<mod>/app/dtos` import flips to
//     `<mod>/app/shared/dtos`.
//  3. Imports for the emptied layered packages (`app/services`,
//     `app/services/interfaces`, and a fully-requalified `app/dtos`)
//     drop when no reference survives; one `<snake>pkg "<mod>/app/<snake>"`
//     import is added per resource that is actually referenced.
func TransformGraphQL(src []byte, mod string, resources []Resource) ([]byte, error) {
	dec := decorator.NewDecorator(token.NewFileSet())
	file, err := dec.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("featurize graphql: parse: %w", err)
	}

	rewriteSelectors(file, graphqlForwardSwaps(resources))

	// Flip <mod>/app/dtos → <mod>/app/shared/dtos when shared `dtos.X`
	// references survive the re-qualification.
	rewriteDtosImportPath(file, mod)

	// Drop the layered imports whose references were all re-qualified.
	// `app/dtos` only remains at its old path when nothing referenced
	// `dtos.` anymore (the flip above is reference-gated), so a guarded
	// drop removes exactly the unused leftover.
	for _, p := range []string{
		mod + "/app/dtos",
		mod + "/app/services",
		mod + "/app/services/interfaces",
	} {
		alias := importAliasForPath(file, p)
		if alias != "" && aliasReferenced(file, alias) {
			continue
		}
		dropImportPath(file, p)
	}

	// Add one feature import per resource actually referenced.
	for _, r := range resources {
		alias := r.Snake + "pkg"
		if !aliasReferenced(file, alias) {
			continue
		}
		ensureImport(file, mod+"/app/"+r.Snake, alias)
	}

	return renderFile(file)
}

// TransformGraphQLReverse rewrites one `.go` file under
// app/graphql/resolvers/ from the feature shape back to layered — the
// exact inverse of TransformGraphQL.
func TransformGraphQLReverse(src []byte, mod string, resources []Resource) ([]byte, error) {
	dec := decorator.NewDecorator(token.NewFileSet())
	file, err := dec.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("featurize graphql reverse: parse: %w", err)
	}

	rewriteSelectors(file, graphqlReverseSwaps(resources))

	// Flip <mod>/app/shared/dtos back to <mod>/app/dtos.
	oldPath := mod + "/app/shared/dtos"
	newPath := mod + "/app/dtos"
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) == oldPath {
			imp.Path.Value = fmt.Sprintf("%q", newPath)
		}
	}

	// Drop the per-feature imports whose references were re-qualified.
	for _, r := range resources {
		p := mod + "/app/" + r.Snake
		alias := importAliasForPath(file, p)
		if alias != "" && aliasReferenced(file, alias) {
			continue
		}
		dropImportPath(file, p)
	}

	// Add back the layered imports that are now referenced. The dtos
	// import may already be present via the path flip above —
	// ensureImport is a no-op in that case.
	for _, ai := range []importSpec{
		{alias: "svcInterfaces", path: mod + "/app/services/interfaces"},
		{alias: "", path: mod + "/app/services"},
		{alias: "", path: mod + "/app/dtos"},
	} {
		checkAlias := ai.alias
		if checkAlias == "" {
			idx := strings.LastIndex(ai.path, "/")
			checkAlias = ai.path[idx+1:]
		}
		if !aliasReferenced(file, checkAlias) {
			continue
		}
		ensureImport(file, ai.path, ai.alias)
	}

	return renderFile(file)
}
