package featurize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// --- forward: routes renaming, both package shapes ---

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

// --- reverse ---

// TestTransformPerResourceReverse_SelfQualifierDropped covers dropSelfQualifier
// plus the non-Ident guards. Moving back into package services means
// `services.X` references become bare again.
func TestTransformPerResourceReverse_SelfQualifierDropped(t *testing.T) {
	src := `package user

func Use() {
	_ = services.CreateUserInput{}` + nonIdentSelectors + `}
`
	got, err := TransformPerResourceReverse([]byte(src),
		LayeredDestination{Path: "app/services/user.service.go", PackageName: "services"},
		Options{ModulePath: testMod, Resource: userResource()})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, "package services")
	assert.Contains(t, out, "_ = CreateUserInput{}",
		"a reference to the destination package itself must lose its qualifier")
	assert.Contains(t, out, "outer.inner.Field", "non-Ident receivers are untouched")
}

// TestTransformPerResourceReverse_DtosPathRestored covers the dtos destination
// arm, which points the import back at the un-relocated shared package.
func TestTransformPerResourceReverse_DtosPathRestored(t *testing.T) {
	src := `package user

import (
	"example.com/myapp/app/shared/dtos"
)

type ListUsersQueryDto struct {
	Pagination dtos.TPaginationInputDto
}
`
	got, err := TransformPerResourceReverse([]byte(src),
		LayeredDestination{Path: "app/dtos/user.dtos.go", PackageName: "dtos"},
		Options{ModulePath: testMod, Resource: userResource()})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, "package dtos")
	assert.NotContains(t, out, "app/shared/dtos",
		"back in package dtos the shared symbols are local again")
}

// TestTransformPerResourceReverse_ControllerImportsAdded covers the controllers
// destination arm, which has to import the packages a controller references
// once it is no longer co-located with them.
func TestTransformPerResourceReverse_ControllerImportsAdded(t *testing.T) {
	src := `package user

type UserController struct {
	svc UserServiceInterface
}

func NewUserController(s UserServiceInterface) *UserController {
	return &UserController{svc: s}
}
`
	got, err := TransformPerResourceReverse([]byte(src),
		LayeredDestination{Path: "app/rest/controllers/user.controller.go", PackageName: "controllers"},
		Options{ModulePath: testMod, Resource: userResource()})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, "package controllers")
	assert.Contains(t, out, "svcInterfaces.UserServiceInterface",
		"the interface is in another package once the controller moves back")
	assert.Contains(t, out, `"example.com/myapp/app/services/interfaces"`)
}

// TestTransformPerResourceReverse_ControllerKeepsExistingImports covers the
// already-imported branch of the same arm.
func TestTransformPerResourceReverse_ControllerKeepsExistingImports(t *testing.T) {
	src := `package user

import (
	svcInterfaces "example.com/myapp/app/services/interfaces"
)

type UserController struct {
	svc svcInterfaces.UserServiceInterface
}
`
	got, err := TransformPerResourceReverse([]byte(src),
		LayeredDestination{Path: "app/rest/controllers/user.controller.go", PackageName: "controllers"},
		Options{ModulePath: testMod, Resource: userResource()})
	require.NoError(t, err)

	assert.Equal(t, 1, countOccurrences(string(got), `"example.com/myapp/app/services/interfaces"`))
}

// TestTransformPerResourceReverse_ExternalTestDoesNotSelfQualify covers the
// empty-alias early return in dropSelfQualifier: an external test package is a
// separate unit and must keep its qualifiers.
func TestTransformPerResourceReverse_ExternalTestDoesNotSelfQualify(t *testing.T) {
	src := `package user_test

func TestThing(t *testing.T) {
	_ = services.CreateUserInput{}
}
`
	got, err := TransformPerResourceReverse([]byte(src),
		LayeredDestination{Path: "app/services/user.service_test.go", PackageName: "services_test"},
		Options{ModulePath: testMod, Resource: userResource()})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, "package services_test")
	assert.Contains(t, out, "services.CreateUserInput",
		"an external test reaches the package through its qualifier and must keep it")
}

// TestFixSharedDtosImportPathReverse_NoMatchingImport covers the walk over a
// file whose imports do not include the shared dtos path.
func TestFixSharedDtosImportPathReverse_NoMatchingImport(t *testing.T) {
	src := `package validators

import (
	"time"
)

var _ = time.Now
`
	got, err := FixSharedDtosImportPathReverse([]byte(src), testMod)
	require.NoError(t, err)
	assert.Contains(t, string(got), `"time"`)
}

// --- crosscutting ---

func TestTransformCrossCutting_ParseError(t *testing.T) {
	_, err := TransformContainer([]byte("package di\n\nfunc Broken( {\n"), testMod, []Resource{userResource()})
	require.Error(t, err)
}
