package featurize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testMod = "example.com/myapp"

func userResource() Resource {
	return Resource{Name: "User", Snake: "user", Plural: "Users"}
}

// --- TransformMock ---

// TestTransformMock flips a generated testify mock from the layered interface
// packages to the feature package. The mock lives in `package mocks`, a
// different package from the feature it mocks, so references stay
// SelectorExprs — only the qualifier changes.
func TestTransformMock(t *testing.T) {
	src := `package mocks

import (
	"github.com/stretchr/testify/mock"
	"example.com/myapp/app/models"
	repoInterfaces "example.com/myapp/app/repositories/interfaces"
	svcInterfaces "example.com/myapp/app/services/interfaces"
	"example.com/myapp/app/services"
)

type UserServiceMock struct{ mock.Mock }

var _ svcInterfaces.UserServiceInterface = (*UserServiceMock)(nil)

func (m *UserServiceMock) Create(in services.CreateUserInput) (*models.User, error) {
	_ = repoInterfaces.ErrUserNotDeletable
	return nil, nil
}

func (m *UserServiceMock) Update(p services.UpdateUserPatch) error { return nil }

func (m *UserServiceMock) List(f services.ListUsersFilter) ([]*models.User, error) { return nil, nil }

func (m *UserServiceMock) Repo() repoInterfaces.UserRepositoryInterface { return nil }
`
	got, err := TransformMock([]byte(src), testMod, userResource())
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, `userpkg "example.com/myapp/app/user"`)
	assert.Contains(t, out, "userpkg.UserServiceInterface")
	assert.Contains(t, out, "userpkg.UserRepositoryInterface")
	assert.Contains(t, out, "userpkg.CreateUserInput")
	assert.Contains(t, out, "userpkg.UpdateUserPatch")
	assert.Contains(t, out, "userpkg.ListUsersFilter")
	assert.Contains(t, out, "userpkg.ErrUserNotDeletable")

	// The model deliberately stays put: under Option B it remains in
	// package models, so the mock keeps the cross-package reference.
	assert.Contains(t, out, "models.User")
	assert.Contains(t, out, `"example.com/myapp/app/models"`,
		"the models import must survive — the model does not move")

	assert.NotContains(t, out, "repoInterfaces.")
	assert.NotContains(t, out, "svcInterfaces.")
	assert.Contains(t, out, "package mocks", "the mock stays in package mocks")
}

// TestTransformMock_RoundTripsWithReverse pins the pair: featurizing a mock and
// unwinding it must restore the layered qualifiers.
func TestTransformMock_RoundTripsWithReverse(t *testing.T) {
	src := `package mocks

import (
	repoInterfaces "example.com/myapp/app/repositories/interfaces"
	svcInterfaces "example.com/myapp/app/services/interfaces"
	"example.com/myapp/app/services"
)

type UserServiceMock struct{}

var _ svcInterfaces.UserServiceInterface = (*UserServiceMock)(nil)

func (m *UserServiceMock) Create(in services.CreateUserInput) error { return nil }

func (m *UserServiceMock) Repo() repoInterfaces.UserRepositoryInterface { return nil }
`
	forward, err := TransformMock([]byte(src), testMod, userResource())
	require.NoError(t, err)

	back, err := TransformMockReverse(forward, testMod, userResource())
	require.NoError(t, err)

	out := string(back)
	assert.Contains(t, out, "svcInterfaces.UserServiceInterface")
	assert.Contains(t, out, "repoInterfaces.UserRepositoryInterface")
	assert.Contains(t, out, "services.CreateUserInput")
	assert.NotContains(t, out, "userpkg.")
}

// --- TransformCoreProviders ---

// TestTransformCoreProviders rewrites only the password-generator binding.
// PasswordGenerator follows the user feature because it is the sole consumer
// in the bootstrap scaffold; everything else in core.go still resolves.
func TestTransformCoreProviders(t *testing.T) {
	src := `package providers

import (
	"github.com/google/wire"
	"example.com/myapp/app/services"
	"example.com/myapp/app/validators"
)

var CoreSet = wire.NewSet(
	services.NewDefaultPasswordGenerator,
	validators.NewAppValidator,
)
`
	got, err := TransformCoreProviders([]byte(src), testMod, []Resource{userResource()})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, "userpkg.NewDefaultPasswordGenerator")
	assert.Contains(t, out, `userpkg "example.com/myapp/app/user"`)
	assert.NotContains(t, out, "services.NewDefaultPasswordGenerator")

	// Untouched bindings must survive verbatim.
	assert.Contains(t, out, "validators.NewAppValidator")
	assert.Contains(t, out, `"example.com/myapp/app/validators"`)
}

// TestTransformCoreProviders_NoUserResource covers the early return. A project
// with no user feature has nothing to rebind, and the file must come back
// byte-identical rather than losing its services import.
func TestTransformCoreProviders_NoUserResource(t *testing.T) {
	src := `package providers

import "example.com/myapp/app/services"

var CoreSet = services.NewDefaultPasswordGenerator
`
	got, err := TransformCoreProviders([]byte(src), testMod, []Resource{
		{Name: "Order", Snake: "order", Plural: "Orders"},
	})
	require.NoError(t, err)
	assert.Equal(t, src, string(got), "with no user feature the source must be returned unchanged")
}

// --- FixDtosImportPath ---

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

// --- requalifyBareSharedTypes ---

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

// --- requalifyBareSharedAliases + rewriteDtosImportPath ---

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

// --- reverse: renameRegisterRoutesBack and destPackageAlias ---

// TestTransformPerResourceReverse_RenamesRegisterRoutes covers the routes
// unwind. Forward renames <Name>Routes to RegisterRoutes so each feature
// exposes the same entry point; reverse has to put the name back, or the
// layered index.routes.go would call a function that no longer exists.
func TestTransformPerResourceReverse_RenamesRegisterRoutes(t *testing.T) {
	src := `package user

import "github.com/go-chi/chi/v5"

func RegisterRoutes(r chi.Router, c *UserController) {
	r.Get("/users", c.List)
}
`
	got, err := TransformPerResourceReverse([]byte(src),
		LayeredDestination{Path: "app/rest/routes/user.routes.go", PackageName: "routes"},
		Options{ModulePath: testMod, Resource: userResource()})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, "func UserRoutes(")
	assert.NotContains(t, out, "func RegisterRoutes(")
	assert.Contains(t, out, "package routes")
}

// TestDestPackageAlias covers the alias table that decides whether a reverse
// destination qualifies its own symbols.
func TestDestPackageAlias(t *testing.T) {
	selfQualifying := map[string]string{
		"repositories": "repositories",
		"services":     "services",
		"controllers":  "controllers",
		"dtos":         "dtos",
		"routes":       "routes",
		"providers":    "providers",
	}
	for pkg, want := range selfQualifying {
		t.Run(pkg, func(t *testing.T) {
			assert.Equal(t, want, destPackageAlias(pkg))
		})
	}

	// External test packages are separate compilation units that reach the
	// underlying package through its qualifier, so they must not self-qualify.
	for _, pkg := range []string{"services_test", "controllers_test", "dtos_test", "repositories_test"} {
		t.Run(pkg, func(t *testing.T) {
			assert.Empty(t, destPackageAlias(pkg))
		})
	}

	// "interfaces" is ambiguous — both layered interface packages carry that
	// name — so symbols always stay qualified.
	assert.Empty(t, destPackageAlias("interfaces"),
		"repo and service interfaces share the package name; qualifying is the safe answer")
	assert.Empty(t, destPackageAlias("something-else"))
}

func TestTransformPerResourceReverse_ParseError(t *testing.T) {
	_, err := TransformPerResourceReverse([]byte("package user\n\nfunc Broken( {\n"),
		LayeredDestination{Path: "app/services/user.service.go", PackageName: "services"},
		Options{ModulePath: testMod, Resource: userResource()})
	require.Error(t, err)
}

func TestFixSharedDtosImportPathReverse_ParseError(t *testing.T) {
	_, err := FixSharedDtosImportPathReverse([]byte("package v\n\nfunc Broken( {\n"), testMod)
	require.Error(t, err)
}

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
