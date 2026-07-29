// Coverage for forward.go — the layered → feature direction: the
// TransformPerResource pipeline and each of its rewrite steps.

package featurize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
