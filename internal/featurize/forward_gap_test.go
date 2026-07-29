// Additional coverage for forward.go — the import-block bookkeeping the
// transform steps do. Several of these branches guard against a spec that is
// not an *dst.ImportSpec sitting inside an import GenDecl. The Go parser
// cannot produce that shape, so those trees are built by hand; the point is
// that the helper copies such a spec through instead of discarding it.

package featurize

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// importGenDecl builds an import declaration containing the given specs,
// including any deliberately non-import spec.
func importGenDecl(specs ...dst.Spec) *dst.GenDecl {
	return &dst.GenDecl{Tok: token.IMPORT, Specs: specs}
}

func importSpecFor(path string) *dst.ImportSpec {
	return &dst.ImportSpec{Path: &dst.BasicLit{Kind: token.STRING, Value: `"` + path + `"`}}
}

// --- dropOrphanedImports ---

// TestDropOrphanedImports_KeepsAReferencedImport covers the branch where the
// candidate import is still in use: only an import with no remaining reference
// may be removed, or the file stops compiling.
func TestDropOrphanedImports_KeepsAReferencedImport(t *testing.T) {
	file := parseDST(t, `package user

import "errors"

var ErrSomethingElse = errors.New("still referenced")
`)
	dropOrphanedImports(file)

	out, err := renderFile(file)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"errors"`,
		"an import that is still referenced must survive")
}

func TestDropOrphanedImports_RemovesAnUnreferencedImport(t *testing.T) {
	file := parseDST(t, `package user

import "errors"

type T struct{}
`)
	dropOrphanedImports(file)

	out, err := renderFile(file)
	require.NoError(t, err)
	assert.NotContains(t, string(out), `"errors"`)
}

// TestDropOrphanedImports_LeavesNonImportSpecsAlone covers the defensive
// branch for a spec that is not an ImportSpec.
func TestDropOrphanedImports_LeavesNonImportSpecsAlone(t *testing.T) {
	keep := &dst.ValueSpec{Names: []*dst.Ident{{Name: "sentinel"}}}
	target := importSpecFor("errors")
	gd := importGenDecl(keep, target)
	file := &dst.File{
		Name:    &dst.Ident{Name: "user"},
		Decls:   []dst.Decl{gd},
		Imports: []*dst.ImportSpec{target},
	}

	dropOrphanedImports(file)

	assert.Equal(t, []dst.Spec{keep}, gd.Specs,
		"the non-import spec must be preserved while the orphaned import goes")
}

// --- rewriteDtosImportPath ---

// TestRewriteDtosImportPath_LeavesNonImportSpecsAlone covers the same guard in
// the path-flipping walk.
func TestRewriteDtosImportPath_LeavesNonImportSpecsAlone(t *testing.T) {
	keep := &dst.ValueSpec{Names: []*dst.Ident{{Name: "sentinel"}}}
	target := importSpecFor("example.com/myapp/app/dtos")
	gd := importGenDecl(keep, target)
	file := &dst.File{
		Name:    &dst.Ident{Name: "user"},
		Decls:   []dst.Decl{gd, dtosReferencingDecl()},
		Imports: []*dst.ImportSpec{target},
	}

	rewriteDtosImportPath(file, testMod)

	assert.Equal(t, `"example.com/myapp/app/shared/dtos"`, target.Path.Value,
		"the dtos import must be repointed at the relocated shared package")
	assert.Same(t, keep, gd.Specs[0], "the non-import spec must be untouched")
}

// dtosReferencingDecl returns a declaration containing a `dtos.X` selector, so
// rewriteDtosImportPath sees the file as still referencing the package.
func dtosReferencingDecl() dst.Decl {
	return &dst.GenDecl{
		Tok: token.VAR,
		Specs: []dst.Spec{&dst.ValueSpec{
			Names: []*dst.Ident{{Name: "_"}},
			Type: &dst.SelectorExpr{
				X:   &dst.Ident{Name: "dtos"},
				Sel: &dst.Ident{Name: "TPaginationInputDto"},
			},
		}},
	}
}

// --- dropCollapsableImports ---

// TestDropCollapsableImports_LeavesNonImportSpecsAlone covers the guard in the
// collapse pass.
func TestDropCollapsableImports_LeavesNonImportSpecsAlone(t *testing.T) {
	keep := &dst.ValueSpec{Names: []*dst.Ident{{Name: "sentinel"}}}
	collapsable := importSpecFor(testMod + "/app/services")
	other := importSpecFor("context")
	gd := importGenDecl(keep, collapsable, other)
	file := &dst.File{
		Name:    &dst.Ident{Name: "user"},
		Decls:   []dst.Decl{gd},
		Imports: []*dst.ImportSpec{collapsable, other},
	}

	dropCollapsableImports(file, testMod)

	assert.Equal(t, []dst.Spec{keep, other}, gd.Specs,
		"only the collapsable project import may be dropped")
}

// --- dropDuplicateErrorVars ---

// TestDropDuplicateErrorVars_KeepsNonValueSpecs covers the spec-level guard,
// and TestDropDuplicateErrorVars_KeepsOtherVars the name filter.
func TestDropDuplicateErrorVars_KeepsNonValueSpecs(t *testing.T) {
	file := parseDST(t, `package user

import "errors"

var ErrUserNotDeletable = errors.New("nope")
var ErrOther = errors.New("kept")

type UserRepositoryInterface interface {
	Delete(id string) error
}
`)
	dropDuplicateErrorVars(file, "User")

	out, err := renderFile(file)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "ErrUserNotDeletable")
	assert.Contains(t, string(out), "ErrOther", "unrelated vars must survive")
	assert.Contains(t, string(out), "UserRepositoryInterface")
}

// TestDropDuplicateErrorVars_RequiresAnInterface pins the heuristic: the
// duplicate is only dropped in a repository_iface-shaped file. The service's
// errors.go declares the same var and has no interface — dropping it there
// would delete the only definition.
func TestDropDuplicateErrorVars_RequiresAnInterface(t *testing.T) {
	file := parseDST(t, `package user

import "errors"

var ErrUserNotDeletable = errors.New("the real definition")
`)
	dropDuplicateErrorVars(file, "User")

	out, err := renderFile(file)
	require.NoError(t, err)
	assert.Contains(t, string(out), "ErrUserNotDeletable",
		"without an interface decl this is errors.go, not the iface re-export")
}

// TestDropDuplicateErrorVars_TypeDeclWithoutInterface covers the TypeSpec scan
// finding a non-interface type.
func TestDropDuplicateErrorVars_TypeDeclWithoutInterface(t *testing.T) {
	file := parseDST(t, `package user

import "errors"

var ErrUserNotDeletable = errors.New("kept")

type Alias struct{ X int }
`)
	dropDuplicateErrorVars(file, "User")

	out, err := renderFile(file)
	require.NoError(t, err)
	assert.Contains(t, string(out), "ErrUserNotDeletable",
		"a struct type is not an interface — the heuristic must not fire")
}

// --- routes selector rename, non-external-test path ---

// TestTransformPerResource_QualifiedRoutesCallInSamePackage covers the routes
// rename for a file that is NOT an external test: a layered file importing
// app/rest/routes and calling routes.UserRoutes must follow the rename.
func TestTransformPerResource_QualifiedRoutesCallInSamePackage(t *testing.T) {
	src := `package services

import (
	"example.com/myapp/app/rest/routes"
)

func Mount(r Router, c *Controller) {
	routes.UserRoutes(r, c)
}
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: testMod, Resource: userResource(),
	})
	require.NoError(t, err)

	assert.Contains(t, string(got), "RegisterRoutes(r, c)",
		"the renamed entry point must be used")
	assert.NotContains(t, string(got), "UserRoutes(")
}

// TestRequalifyBareSharedTypes_SkipsAnImportItAlreadyHas covers the
// already-imported guard. Driving the step directly is the only way to reach
// it: inside the full pipeline the project's own controllers import is
// collapsed away before this step runs, so it is never present to match.
func TestRequalifyBareSharedTypes_SkipsAnImportItAlreadyHas(t *testing.T) {
	file := parseDST(t, `package user

import (
	controllers "example.com/myapp/app/rest/controllers"
)

func Use(v Validator) controllers.Thing { return nil }
`)
	requalifyBareSharedTypes(file, testMod)

	out, err := renderFile(file)
	require.NoError(t, err)
	assert.Contains(t, string(out), "controllers.Validator", "the bare type must gain its qualifier")
	assert.Equal(t, 1, countOccurrences(string(out), `"example.com/myapp/app/rest/controllers"`),
		"the import must not be added a second time")
}

// TestRewriteDtosImportPath_FlipsASpecMissingFromFileImports covers the second
// walk, over the import GenDecl's specs.
//
// For a parsed file the two collections share pointers, so flipping via
// file.Imports already updates the GenDecl and this walk finds nothing left to
// do. It only has work when the collections diverge — which is why the tree
// here is built by hand with an empty file.Imports.
func TestRewriteDtosImportPath_FlipsASpecMissingFromFileImports(t *testing.T) {
	spec := importSpecFor(testMod + "/app/dtos")
	gd := importGenDecl(spec)
	file := &dst.File{
		Name:  &dst.Ident{Name: "user"},
		Decls: []dst.Decl{gd, dtosReferencingDecl()},
		// Deliberately empty: the spec is reachable only through the GenDecl.
		Imports: nil,
	}

	rewriteDtosImportPath(file, testMod)

	assert.Equal(t, `"example.com/myapp/app/shared/dtos"`, spec.Path.Value,
		"the GenDecl walk must flip a spec the Imports slice does not carry")
}

// TestDropDuplicateErrorVars_ToleratesUnexpectedSpecKinds covers the two
// spec-kind guards: a TYPE declaration holding something other than a TypeSpec,
// and a VAR declaration holding something other than a ValueSpec. Neither
// shape comes out of the parser, so the tree is hand-built.
func TestDropDuplicateErrorVars_ToleratesUnexpectedSpecKinds(t *testing.T) {
	strayInType := importSpecFor("stray/in/type")
	ifaceSpec := &dst.TypeSpec{
		Name: &dst.Ident{Name: "UserRepositoryInterface"},
		Type: &dst.InterfaceType{Methods: &dst.FieldList{}},
	}
	typeDecl := &dst.GenDecl{Tok: token.TYPE, Specs: []dst.Spec{strayInType, ifaceSpec}}

	strayInVar := importSpecFor("stray/in/var")
	target := &dst.ValueSpec{Names: []*dst.Ident{{Name: "ErrUserNotDeletable"}}}
	varDecl := &dst.GenDecl{Tok: token.VAR, Specs: []dst.Spec{strayInVar, target}}

	file := &dst.File{
		Name:  &dst.Ident{Name: "user"},
		Decls: []dst.Decl{typeDecl, varDecl},
	}

	dropDuplicateErrorVars(file, "User")

	assert.Equal(t, []dst.Spec{strayInVar}, varDecl.Specs,
		"the duplicate var goes; the unexpected spec is carried through")
	assert.Contains(t, typeDecl.Specs, ifaceSpec,
		"the interface decl that drives the heuristic must survive")
}
