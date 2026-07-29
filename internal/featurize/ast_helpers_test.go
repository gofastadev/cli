package featurize

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Direct unit tests for the dst helpers in ast.go. Several of the branches
// below cannot be reached through a parsed file — an import GenDecl holding a
// non-ImportSpec, for instance, is not something the parser can produce — so
// those trees are constructed by hand.

func parseDST(t *testing.T, src string) *dst.File {
	t.Helper()
	file, err := decorator.NewDecorator(token.NewFileSet()).Parse(src)
	require.NoError(t, err)
	return file
}

// --- ensureImport ---

// TestEnsureImport_NoExistingImportBlock covers the prepend path: a file with
// no import declaration at all needs a whole GenDecl synthesized in front of
// its existing declarations.
func TestEnsureImport_NoExistingImportBlock(t *testing.T) {
	file := parseDST(t, "package user\n\ntype T struct{}\n")
	ensureImport(file, "example.com/myapp/app/models", "")

	out, err := renderFile(file)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"example.com/myapp/app/models"`)
	assert.Contains(t, string(out), "type T struct{}", "existing declarations must survive")
}

// TestEnsureImport_AddsToExistingBlock covers the case where an import GenDecl
// is already present and the new spec has to join it rather than start a new
// one. (The decl-loop's skip-non-import branch is covered by the test above:
// with no import block, every declaration in the file is skipped in turn.)
func TestEnsureImport_AddsToExistingBlock(t *testing.T) {
	file := parseDST(t, "package user\n\nimport \"context\"\n\ntype T struct{}\n")
	ensureImport(file, "example.com/myapp/app/models", "models")

	importDecls := 0
	for _, decl := range file.Decls {
		if gd, ok := decl.(*dst.GenDecl); ok && gd.Tok == token.IMPORT {
			importDecls++
		}
	}
	assert.Equal(t, 1, importDecls, "the import must join the existing block, not create a second one")

	out, err := renderFile(file)
	require.NoError(t, err)
	assert.Contains(t, string(out), `models "example.com/myapp/app/models"`)
}

func TestEnsureImport_AlreadyPresentIsANoOp(t *testing.T) {
	file := parseDST(t, "package user\n\nimport \"context\"\n")
	before := len(file.Imports)
	ensureImport(file, "context", "")
	assert.Equal(t, before, len(file.Imports))
}

// --- importAlias ---

func TestImportAlias(t *testing.T) {
	cases := []struct {
		name  string
		alias string
		path  string
		want  string
	}{
		{"explicit alias wins", "repoInterfaces", "example.com/a/b/interfaces", "repoInterfaces"},
		{"last path segment", "", "example.com/a/b/models", "models"},
		{"blank import is not an alias", "_", "example.com/a/b/models", "models"},
		{"path with no separator", "", "errors", "errors"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := &dst.ImportSpec{
				Path: &dst.BasicLit{Kind: token.STRING, Value: `"` + tc.path + `"`},
			}
			if tc.alias != "" {
				spec.Name = &dst.Ident{Name: tc.alias}
			}
			assert.Equal(t, tc.want, importAlias(spec, tc.path))
		})
	}
}

// --- importAliasForPath ---

func TestImportAliasForPath(t *testing.T) {
	file := parseDST(t, `package user

import (
	"context"
	repoInterfaces "example.com/myapp/app/repositories/interfaces"
)
`)
	assert.Equal(t, "repoInterfaces", importAliasForPath(file, "example.com/myapp/app/repositories/interfaces"))
	assert.Equal(t, "context", importAliasForPath(file, "context"))
	assert.Empty(t, importAliasForPath(file, "example.com/not/imported"),
		"an absent import has no local name")
}

// --- dropImportPath ---

// TestDropImportPath_LeavesNonImportSpecsAlone covers the defensive branch for
// a spec that is not an ImportSpec. The parser cannot produce that shape, so
// the tree is built by hand — the point is that the helper copies such a spec
// through rather than dropping it.
func TestDropImportPath_LeavesNonImportSpecsAlone(t *testing.T) {
	keep := &dst.ValueSpec{Names: []*dst.Ident{{Name: "sentinel"}}}
	target := &dst.ImportSpec{Path: &dst.BasicLit{Kind: token.STRING, Value: `"drop/me"`}}
	other := &dst.ImportSpec{Path: &dst.BasicLit{Kind: token.STRING, Value: `"keep/me"`}}

	gd := &dst.GenDecl{Tok: token.IMPORT, Specs: []dst.Spec{keep, target, other}}
	file := &dst.File{
		Name:    &dst.Ident{Name: "user"},
		Decls:   []dst.Decl{gd},
		Imports: []*dst.ImportSpec{target, other},
	}

	dropImportPath(file, "drop/me")

	assert.Equal(t, []dst.Spec{keep, other}, gd.Specs,
		"the non-import spec must be preserved and only the target import removed")
	assert.Equal(t, []*dst.ImportSpec{other}, file.Imports)
}

// TestDropImportPath_SkipsNonImportDecls covers the decl-loop continue.
func TestDropImportPath_SkipsNonImportDecls(t *testing.T) {
	file := parseDST(t, `package user

import "context"

const X = 1
`)
	dropImportPath(file, "context")
	assert.Empty(t, file.Imports)
}

// --- aliasReferenced ---

// TestAliasReferenced covers both non-Ident cases: a selector whose X is
// itself a selector (a.b.c) and one whose X is a call result.
func TestAliasReferenced(t *testing.T) {
	file := parseDST(t, `package user

func f() {
	_ = pkg.Symbol
	_ = outer.inner.Field
	_ = build().Field
}
`)
	assert.True(t, aliasReferenced(file, "pkg"))
	assert.False(t, aliasReferenced(file, "absent"))
	assert.True(t, aliasReferenced(file, "outer"),
		"a nested selector's innermost X still counts as a reference")
}

// --- rewriteSelectors ---

// TestRewriteSelectors covers the replacement shapes and the skip for a
// selector whose X is not a bare identifier.
func TestRewriteSelectors(t *testing.T) {
	file := parseDST(t, `package user

func f() {
	_ = services.UserService
	_ = outer.inner.Untouched
	_ = build().Untouched
	_ = services.ToBare
}
`)
	rewriteSelectors(file, map[string]string{
		"services.UserService": "userpkg.UserService",
		"services.ToBare":      "ToBare",
	})

	out, err := renderFile(file)
	require.NoError(t, err)
	got := string(out)

	assert.Contains(t, got, "userpkg.UserService", "dotted replacements become selectors")
	assert.Contains(t, got, "_ = ToBare", "a replacement with no dot becomes a bare identifier")
	assert.Contains(t, got, "outer.inner.Untouched", "non-Ident receivers are left alone")
	assert.Contains(t, got, "build().Untouched")
}

// --- rewriteCallNames ---

// TestRewriteCallNames covers callee rewriting, including a call whose Fun is
// already a selector (must be skipped) and a replacement with no dot.
func TestRewriteCallNames(t *testing.T) {
	file := parseDST(t, `package routes

func f() {
	UserRoutes(r, c)
	pkg.OtherRoutes(r, c)
	Renamed(r)
	NotInTheMap(r)
}
`)
	rewriteCallNames(file, map[string]string{
		"UserRoutes": "userpkg.RegisterRoutes",
		"Renamed":    "PlainName",
	})

	out, err := renderFile(file)
	require.NoError(t, err)
	got := string(out)

	assert.Contains(t, got, "userpkg.RegisterRoutes(r, c)")
	assert.Contains(t, got, "PlainName(r)", "a replacement with no dot stays a bare callee")
	assert.Contains(t, got, "pkg.OtherRoutes(r, c)", "an already-qualified callee is not a bare Ident")
	assert.Contains(t, got, "NotInTheMap(r)", "a callee absent from the map is left alone")
}

// --- renderFile ---

// TestRenderFile_FallsBackWhenGofmtFails covers the deliberate nilerr: an
// unformattable render is returned as-is so the caller can still inspect it,
// rather than failing the whole transform.
func TestRenderFile_FallsBackWhenGofmtFails(t *testing.T) {
	// `package func` restores fine but is not valid Go, so gofmt rejects it.
	file := parseDST(t, "package user\n")
	file.Name.Name = "func"

	out, err := renderFile(file)
	require.NoError(t, err, "a gofmt failure must not fail the transform")
	assert.Contains(t, string(out), "package func",
		"the unformatted bytes are returned so the caller can inspect them")
}

// --- applyOnExpr ---

func TestApplyOnExpr_NilExpressionIsSafe(t *testing.T) {
	var e dst.Expr
	called := false
	applyOnExpr(&e, func(*applyCursor) bool {
		called = true
		return true
	})
	assert.False(t, called, "a nil expression must not reach the visitor")
}
