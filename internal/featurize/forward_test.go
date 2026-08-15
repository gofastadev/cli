// Coverage for forward.go — the layered → feature direction: the
// TransformPerResource pipeline and each of its rewrite steps.

package featurize

import (
	"go/format"
	"go/token"
	"sort"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countOccurrences reports how many times sub appears in s.
func countOccurrences(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}

// nonIdentSelectors is a fragment every transform below embeds. Each transform
// walks SelectorExprs and bails when the receiver is not a bare identifier;
// without a source containing those shapes the guard never runs, and a
// regression that dropped it would go unnoticed until it crashed on a
// user's chained call.
const nonIdentSelectors = `
	_ = outer.inner.Field
	_ = build().Field
	_ = list[0].Field
`

// normalizeGo canonicalizes Go source with gofmt, then returns its
// non-empty, trimmed lines sorted. Comparing two files by this form
// makes the equivalence check robust to whitespace, import grouping,
// and import ordering while still catching any missing/extra
// declaration, reference, or import path.
func normalizeGo(t *testing.T, src string) string {
	t.Helper()
	formatted, err := format.Source([]byte(src))
	if err != nil {
		t.Fatalf("gofmt failed on source:\n%s\nerror: %v", src, err)
	}
	var lines []string
	for _, ln := range strings.Split(string(formatted), "\n") {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func TestTransformPerResource_RoundTrip_Service(t *testing.T) {
	// Layered service file → forward (feature) → reverse (layered).
	// The result must be equivalent to the original.
	original := `package services

import (
	"github.com/google/uuid"

	"example.com/myapp/app/models"
	repoInterfaces "example.com/myapp/app/repositories/interfaces"
)

type UserService struct {
	repo  repoInterfaces.UserRepositoryInterface
	pwGen PasswordGenerator
}

func NewUserService(repo repoInterfaces.UserRepositoryInterface, pwGen PasswordGenerator) *UserService {
	return &UserService{repo: repo, pwGen: pwGen}
}

func (s *UserService) Get(ctx uuid.UUID) (*models.User, error) {
	return nil, ErrUserNotFound
}
`
	resource := Resource{Name: "User", Snake: "user", Plural: "Users"}

	forward, err := TransformPerResource([]byte(original), Options{
		ModulePath: "example.com/myapp",
		Resource:   resource,
	})
	if err != nil {
		t.Fatalf("forward transform error: %v", err)
	}
	// Sanity: forward really produced feature layout.
	if !strings.Contains(string(forward), "package user") {
		t.Fatalf("forward did not produce package user:\n%s", forward)
	}

	reversed, err := TransformPerResourceReverse(forward,
		LayeredDestination{Path: "app/services/user.service.go", PackageName: "services"},
		Options{ModulePath: "example.com/myapp", Resource: resource})
	if err != nil {
		t.Fatalf("reverse transform error: %v", err)
	}

	if normalizeGo(t, original) != normalizeGo(t, string(reversed)) {
		t.Errorf("round-trip not equivalent to original.\n--- original ---\n%s\n--- reversed ---\n%s", original, reversed)
	}
}

func TestTransformPerResource_Model(t *testing.T) {
	src := `package models

import "github.com/gofastadev/gofasta/pkg/models"

type User struct {
	models.BaseModelImpl
	FirstName string ` + "`gorm:\"not null\"`" + `
}
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: "example.com/myapp",
		Resource:   Resource{Name: "User", Snake: "user", Plural: "Users"},
	})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	// package decl rewritten
	if !strings.Contains(string(got), "package user\n") {
		t.Errorf("package decl not rewritten:\n%s", got)
	}
	// framework BaseModelImpl preserved (it's an external `github.com/gofastadev/gofasta/pkg/models` reference)
	if !strings.Contains(string(got), "models.BaseModelImpl") {
		t.Errorf("framework reference models.BaseModelImpl was stripped — must be preserved:\n%s", got)
	}
}

func TestTransformPerResource_Service(t *testing.T) {
	src := `package services

import (
	"github.com/google/uuid"

	"example.com/myapp/app/models"
	repoInterfaces "example.com/myapp/app/repositories/interfaces"
)

type UserService struct {
	repo  repoInterfaces.UserRepositoryInterface
	pwGen PasswordGenerator
}

func NewUserService(repo repoInterfaces.UserRepositoryInterface, pwGen PasswordGenerator) *UserService {
	return &UserService{repo: repo, pwGen: pwGen}
}

func (s *UserService) Get(ctx uuid.UUID) (*models.User, error) {
	return nil, ErrUserNotFound
}
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: "example.com/myapp",
		Resource:   Resource{Name: "User", Snake: "user", Plural: "Users"},
	})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, "package user") {
		t.Errorf("package decl not rewritten:\n%s", out)
	}
	// Models intentionally stay in app/models/ — see featurize.go's
	// `collapsablePaths` comment for the architectural reason.
	if !strings.Contains(out, "models.User") {
		t.Errorf("models.User reference should survive (model stays in app/models/):\n%s", out)
	}
	if strings.Contains(out, `repoInterfaces "example.com/myapp/app/repositories/interfaces"`) {
		t.Errorf("repoInterfaces import not stripped:\n%s", out)
	}
	if strings.Contains(out, "repoInterfaces.UserRepositoryInterface") {
		t.Errorf("repoInterfaces.UserRepositoryInterface was not collapsed:\n%s", out)
	}
	if !strings.Contains(out, `"github.com/google/uuid"`) {
		t.Errorf("framework uuid import was stripped:\n%s", out)
	}
}

func TestTransformPerResource_ExternalTestPackage(t *testing.T) {
	// External test pattern: `package controllers_test` instead of
	// `package controllers`. Must be rewritten to `package user_test`.
	src := `package controllers_test

import (
	"testing"
	"example.com/myapp/app/dtos"
)

func TestStub(t *testing.T) {
	_ = dtos.User{}
}
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: "example.com/myapp",
		Resource:   Resource{Name: "User", Snake: "user", Plural: "Users"},
	})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, "package user_test") {
		t.Errorf("external test package not rewritten:\n%s", out)
	}
	// Per Option B: dtos.User (per-resource) collapses to bare User
	// because per-resource DTOs move into the feature package. Only
	// shared aliases (TPaginationObjectDto etc.) survive as `dtos.X`.
	if strings.Contains(out, "dtos.User") {
		t.Errorf("dtos.User should be collapsed to bare User in feature mode:\n%s", out)
	}
}

func TestTransformPerResource_Routes(t *testing.T) {
	// Per-resource routes file: keep as-is for now — the
	// `func UserRoutes(...)` rename happens at the cross-cutting level
	// (TransformIndexRoutes wires the call differently). Within the
	// routes.go file itself, the function name stays UserRoutes for
	// API compat with the existing codebase; renaming to RegisterRoutes
	// is a future polish pass.
	src := `package routes

import (
	"github.com/go-chi/chi/v5"
	"github.com/gofastadev/gofasta/pkg/httputil"

	"example.com/myapp/app/rest/controllers"
)

func UserRoutes(r chi.Router, uc *controllers.UserController) {
	r.Get("/users", httputil.Handle(uc.ListUsers))
}
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: "example.com/myapp",
		Resource:   Resource{Name: "User", Snake: "user", Plural: "Users"},
	})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, "package user") {
		t.Errorf("package not rewritten:\n%s", out)
	}
	if strings.Contains(out, "controllers.UserController") {
		t.Errorf("controllers.UserController not collapsed:\n%s", out)
	}
	if !strings.Contains(out, "*UserController") {
		t.Errorf("collapsed UserController reference missing:\n%s", out)
	}
}

// TestFixDtosImportPath moves shared infra files onto the relocated dtos
// package. These files (app_validator.go, resolver.go, ...) do not move
// themselves, but the package they import does.
func TestFixDtosImportPath(t *testing.T) {
	src := `package validators

import (
	"example.com/myapp/app/dtos"
)

func Check(in dtos.TPaginationInputDto) error { return nil }
`
	got, err := FixDtosImportPath([]byte(src), testMod)
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, `"example.com/myapp/app/shared/dtos"`)
	assert.NotContains(t, out, `"example.com/myapp/app/dtos"`)
	assert.Contains(t, out, "dtos.TPaginationInputDto", "the qualifier is unchanged — only the path moves")
}

// TestFixDtosImportPath_RoundTrip pins it against its inverse.
func TestFixDtosImportPath_RoundTrip(t *testing.T) {
	src := `package validators

import "example.com/myapp/app/dtos"

func Check(in dtos.TPaginationInputDto) error { return nil }
`
	forward, err := FixDtosImportPath([]byte(src), testMod)
	require.NoError(t, err)

	back, err := FixSharedDtosImportPathReverse(forward, testMod)
	require.NoError(t, err)

	assert.Contains(t, string(back), `"example.com/myapp/app/dtos"`)
	assert.NotContains(t, string(back), "app/shared/dtos")
}

func TestFixDtosImportPath_ParseError(t *testing.T) {
	_, err := FixDtosImportPath([]byte("package validators\n\nfunc Broken( {\n"), testMod)
	require.Error(t, err)
}

// TestTransformPerResource_RequalifiesBareValidator covers the shared-bare-type
// table. A controller referenced `Validator` bare while it lived in package
// controllers; once it moves into the feature package that bare name no longer
// resolves, so it must gain the controllers qualifier plus the import.
func TestTransformPerResource_RequalifiesBareValidator(t *testing.T) {
	src := `package controllers

type UserController struct {
	validator Validator
}

func NewUserController(v Validator) *UserController {
	return &UserController{validator: v}
}
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: testMod,
		Resource:   userResource(),
	})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, "controllers.Validator")
	assert.Contains(t, out, `controllers "example.com/myapp/app/rest/controllers"`,
		"the qualifier is useless without its import")
}

// TestTransformPerResource_RequalifyValidatorKeepsExistingImport covers the
// already-imported branch: the import must not be added a second time.
func TestTransformPerResource_RequalifyValidatorKeepsExistingImport(t *testing.T) {
	src := `package services

import (
	controllers "example.com/myapp/app/rest/controllers"
)

func Use(v Validator) controllers.Validator { return v }
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: testMod,
		Resource:   userResource(),
	})
	require.NoError(t, err)

	assert.Equal(t, 1, countOccurrences(string(got), `"example.com/myapp/app/rest/controllers"`),
		"the controllers import must appear exactly once")
}

// TestTransformPerResource_RequalifiesSharedDtoSymbols covers the dtos file
// move. Inside app/dtos/user.dtos.go these shared symbols were bare (same
// package); once the file becomes app/user/dtos.go they must be qualified and
// pointed at the relocated shared package.
func TestTransformPerResource_RequalifiesSharedDtoSymbols(t *testing.T) {
	src := `package dtos

type ListUsersQueryDto struct {
	Pagination TPaginationInputDto
	Sorting    TSortingInputDto
	Order      SortOrientation
}

func Default() SortOrientation { return SortOrientationAsc }
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: testMod,
		Resource:   userResource(),
	})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, "dtos.TPaginationInputDto")
	assert.Contains(t, out, "dtos.TSortingInputDto")
	assert.Contains(t, out, "dtos.SortOrientation")
	assert.Contains(t, out, "dtos.SortOrientationAsc")
	assert.Contains(t, out, `"example.com/myapp/app/shared/dtos"`,
		"requalifying without adding the import would not compile")
}

// TestTransformPerResource_KeepsExistingSharedDtosImport covers the branch
// where the shared import is already present.
func TestTransformPerResource_KeepsExistingSharedDtosImport(t *testing.T) {
	src := `package dtos

import "example.com/myapp/app/shared/dtos"

type ListUsersQueryDto struct {
	Pagination TPaginationInputDto
	Existing   dtos.TCommonResponseDto
}
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: testMod,
		Resource:   userResource(),
	})
	require.NoError(t, err)

	assert.Equal(t, 1, countOccurrences(string(got), `"example.com/myapp/app/shared/dtos"`))
}

// TestTransformPerResource_RewritesDtosImportPath covers the path flip for a
// file that keeps referencing dtos.X after the collapse.
func TestTransformPerResource_RewritesDtosImportPath(t *testing.T) {
	src := `package services

import (
	"example.com/myapp/app/dtos"
)

func List(in dtos.TPaginationInputDto) dtos.TCommonResponseDto {
	return dtos.TCommonResponseDto{}
}
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: testMod,
		Resource:   userResource(),
	})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, `"example.com/myapp/app/shared/dtos"`)
	assert.NotContains(t, out, `"example.com/myapp/app/dtos"`)
}

// TestTransformPerResource_EmptyModulePathIsANoOp covers the guard clauses in
// the module-path-dependent steps.
func TestTransformPerResource_EmptyModulePathIsANoOp(t *testing.T) {
	src := `package controllers

func Use(v Validator) error { return nil }
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: "",
		Resource:   userResource(),
	})
	require.NoError(t, err)

	assert.NotContains(t, string(got), "controllers.Validator",
		"without a module path there is no import to add, so the bare name must be left alone")
}

func TestTransformPerResource_ParseError(t *testing.T) {
	_, err := TransformPerResource([]byte("package x\n\nfunc Broken( {\n"), Options{
		ModulePath: testMod,
		Resource:   userResource(),
	})
	require.Error(t, err)
}

// TestTransformPerResource_RenamesRoutesFunc covers the same-package rename:
// every feature exposes RegisterRoutes so index.routes.go can call a uniform
// entry point.
func TestTransformPerResource_RenamesRoutesFunc(t *testing.T) {
	src := `package routes

func UserRoutes(r Router, c *Controller) {` + nonIdentSelectors + `}
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: testMod, Resource: userResource(),
	})
	require.NoError(t, err)

	assert.Contains(t, string(got), "func RegisterRoutes(")
	assert.NotContains(t, string(got), "func UserRoutes(")
}

// TestTransformPerResource_ExternalTestQualifiesRoutesCall covers the
// external-test arm of the same rename: a `routes_test` file calls the routes
// function through the package qualifier, so both the qualifier and the
// function name have to change together.
func TestTransformPerResource_ExternalTestQualifiesRoutesCall(t *testing.T) {
	src := `package routes_test

import "example.com/myapp/app/rest/routes"

func TestRoutes(t *testing.T) {
	routes.UserRoutes(r, c)` + nonIdentSelectors + `}
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: testMod, Resource: userResource(),
	})
	require.NoError(t, err)

	assert.Contains(t, string(got), "user.RegisterRoutes(r, c)",
		"an external test must reach the renamed function through the feature package")
}

// TestTransformPerResource_RequalifyValidatorWithForeignControllersImport
// covers the already-imported check in requalifyBareSharedTypes.
//
// KNOWN LIMITATION (not asserted as correct): the check matches on import PATH
// while the qualifier it emits is an ALIAS. When a file already binds the name
// `controllers` to some other module's path, the project's own controllers
// path is still absent, so a second spec is added under the same alias and the
// output does not compile ("controllers redeclared"). Reaching this needs a
// foreign import aliased exactly `controllers`, which is why it has not been
// hit in practice. The assertions below pin only what is unambiguously
// required — the bare Validator gains its qualifier — so this test keeps
// passing once the alias-collision is fixed.
func TestTransformPerResource_RequalifyValidatorWithForeignControllersImport(t *testing.T) {
	src := `package services

import (
	controllers "other.example/lib/app/rest/controllers"
)

func Use(v Validator) controllers.Thing { return nil }
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: testMod, Resource: userResource(),
	})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, "controllers.Validator", "the bare type must be qualified")
	assert.Contains(t, out, `"other.example/lib/app/rest/controllers"`,
		"the pre-existing foreign import must survive")
}

// TestTransformPerResource_SharedDtoSymbolsJoinExistingImportBlock covers the
// append-to-existing-block arm: the file has imports, but not the shared dtos
// one, so the new spec joins the block rather than starting a new decl.
func TestTransformPerResource_SharedDtoSymbolsJoinExistingImportBlock(t *testing.T) {
	src := `package dtos

import (
	"time"
)

type ListUsersQueryDto struct {
	When       time.Time
	Pagination TPaginationInputDto
}
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: testMod, Resource: userResource(),
	})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, "dtos.TPaginationInputDto")
	assert.Contains(t, out, `"example.com/myapp/app/shared/dtos"`)
	assert.Contains(t, out, `"time"`, "the pre-existing import must survive")
}

// TestTransformPerResource_DropsDuplicateErrorVar covers the repository_iface
// heuristic: the layered iface file re-exports ErrUserNotDeletable for
// `repoInterfaces` callers, and in feature layout errors.go already declares
// it — keeping both is a redeclaration.
func TestTransformPerResource_DropsDuplicateErrorVar(t *testing.T) {
	src := `package interfaces

import "errors"

var ErrUserNotDeletable = errors.New("user is not deletable")

type UserRepositoryInterface interface {
	Delete(id string) error
}
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: testMod, Resource: userResource(),
	})
	require.NoError(t, err)
	out := string(got)

	assert.NotContains(t, out, "ErrUserNotDeletable = errors.New",
		"the duplicate re-export must be dropped")
	assert.NotContains(t, out, `"errors"`,
		"the errors import is orphaned once the var goes and must be dropped too")
	assert.Contains(t, out, "UserRepositoryInterface interface")
}

// TestTransformPerResource_DropsGroupedDuplicateErrorVars covers the branch
// where filtering empties a grouped var block entirely.
func TestTransformPerResource_DropsGroupedDuplicateErrorVars(t *testing.T) {
	src := `package interfaces

import "errors"

var (
	ErrUserNotDeletable = errors.New("user is not deletable")
)

type UserRepositoryInterface interface {
	Delete(id string) error
}
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: testMod, Resource: userResource(),
	})
	require.NoError(t, err)
	assert.NotContains(t, string(got), "ErrUserNotDeletable")
}

// TestRewritePackageName_UnknownPackageIsLeftAlone covers the fallback: a
// package this transformer does not recognize keeps its name rather than being
// forced into the feature package.
func TestRewritePackageName_UnknownPackageIsLeftAlone(t *testing.T) {
	assert.Equal(t, "user", rewritePackageName("services", "user"))
	assert.Equal(t, "user_test", rewritePackageName("services_test", "user"))
	assert.Equal(t, "somethingelse", rewritePackageName("somethingelse", "user"),
		"an unrecognized package name must be preserved")
}

func TestIsCollapsablePath_EmptyModule(t *testing.T) {
	assert.False(t, isCollapsablePath("example.com/myapp/app/services", ""),
		"without a module path nothing can be identified as in-project")
}

// importGenDecl builds an import declaration containing the given specs,
// including any deliberately non-import spec.
func importGenDecl(specs ...dst.Spec) *dst.GenDecl {
	return &dst.GenDecl{Tok: token.IMPORT, Specs: specs}
}

func importSpecFor(path string) *dst.ImportSpec {
	return &dst.ImportSpec{Path: &dst.BasicLit{Kind: token.STRING, Value: `"` + path + `"`}}
}

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

// TestTransformPerResource_RewritesRoutesDocComment covers the doc-comment
// rewrite riding along with the <Name>Routes → RegisterRoutes rename:
// revive's exported rule requires the comment to open with the new name.
func TestTransformPerResource_RewritesRoutesDocComment(t *testing.T) {
	src := `package routes

// UserRoutes registers the user endpoints.
func UserRoutes(r Router, c *Controller) {` + nonIdentSelectors + `}
`
	got, err := TransformPerResource([]byte(src), Options{
		ModulePath: testMod, Resource: userResource(),
	})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, "// RegisterRoutes registers the user endpoints.")
	assert.NotContains(t, out, "// UserRoutes")
}
