// Coverage for reverse.go — the feature → layered direction:
// TransformPerResourceReverse and its per-destination handling.

package featurize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// TestTransformPerResourceReverse_RegisterRoutesCallIsRequalified covers the
// RegisterRoutes special case on the CALL side. Forward renamed the function
// and qualified an external test's call with the feature alias; reverse has to
// undo both, and add the routes import the restored call needs.
func TestTransformPerResourceReverse_RegisterRoutesCallIsRequalified(t *testing.T) {
	src := `package user_test

func TestRoutes(t *testing.T) {
	user.RegisterRoutes(r, c)
}
`
	got, err := TransformPerResourceReverse([]byte(src),
		LayeredDestination{Path: "app/rest/routes/user.routes_test.go", PackageName: "routes_test"},
		Options{ModulePath: testMod, Resource: userResource()})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, "routes.UserRoutes(r, c)",
		"the call must name the layered function through the routes package")
	assert.Contains(t, out, `"example.com/myapp/app/rest/routes"`,
		"the requalified call needs its import")
}

// TestTransformPerResourceReverse_UnknownSymbolsAreLeftAlone covers the
// symbol-table miss. Only symbols the table knows how to place get rewritten;
// anything else is the project's own code and must survive untouched.
func TestTransformPerResourceReverse_UnknownSymbolsAreLeftAlone(t *testing.T) {
	src := `package user

func Use() {
	_ = user.SomeProjectSpecificHelper
	_ = user.CreateUserInput{}
}
`
	got, err := TransformPerResourceReverse([]byte(src),
		LayeredDestination{Path: "app/services/user.service.go", PackageName: "services"},
		Options{ModulePath: testMod, Resource: userResource()})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, "user.SomeProjectSpecificHelper",
		"an unknown symbol is not the transformer's to rewrite")
	assert.Contains(t, out, "_ = CreateUserInput{}",
		"a known symbol landing in its own package collapses to bare")
}

// TestTransformPerResourceReverse_CrossPackageSymbolGetsItsAlias covers the
// branch where a known symbol belongs to a DIFFERENT package than the
// destination, so it keeps a qualifier and pulls in an aliased import.
func TestTransformPerResourceReverse_CrossPackageSymbolGetsItsAlias(t *testing.T) {
	src := `package user

type UserController struct {
	repo user.UserRepositoryInterface
}
`
	got, err := TransformPerResourceReverse([]byte(src),
		LayeredDestination{Path: "app/rest/controllers/user.controller.go", PackageName: "controllers"},
		Options{ModulePath: testMod, Resource: userResource()})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, "repoInterfaces.UserRepositoryInterface")
	assert.Contains(t, out, `repoInterfaces "example.com/myapp/app/repositories/interfaces"`,
		"an alias that differs from the path's last segment must be written explicitly")
}

// TestTransformPerResourceReverse_SharedDtosPathFlipsForNonDtosDestinations
// covers the else arm of the dtos handling: a destination that is not the dtos
// package keeps the `dtos.` qualifier, but the import path has to point back
// at the un-relocated package.
func TestTransformPerResourceReverse_SharedDtosPathFlipsForNonDtosDestinations(t *testing.T) {
	src := `package user

import (
	"example.com/myapp/app/shared/dtos"
)

type UserController struct {
	page dtos.TPaginationInputDto
}
`
	got, err := TransformPerResourceReverse([]byte(src),
		LayeredDestination{Path: "app/rest/controllers/user.controller.go", PackageName: "controllers"},
		Options{ModulePath: testMod, Resource: userResource()})
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, `"example.com/myapp/app/dtos"`)
	assert.NotContains(t, out, "app/shared/dtos",
		"back in the layered tree the dtos package is not relocated")
	assert.Contains(t, out, "dtos.TPaginationInputDto",
		"only the path moves — the qualifier stays")
}

// TestTransformPerResourceReverse_SkipsImportItAlreadyHas covers the
// already-imported guard when requalification would otherwise add a duplicate.
func TestTransformPerResourceReverse_SkipsImportItAlreadyHas(t *testing.T) {
	src := `package user

import (
	repoInterfaces "example.com/myapp/app/repositories/interfaces"
)

type UserController struct {
	a user.UserRepositoryInterface
	b repoInterfaces.UserRepositoryInterface
}
`
	got, err := TransformPerResourceReverse([]byte(src),
		LayeredDestination{Path: "app/rest/controllers/user.controller.go", PackageName: "controllers"},
		Options{ModulePath: testMod, Resource: userResource()})
	require.NoError(t, err)

	assert.Equal(t, 1, countOccurrences(string(got), `"example.com/myapp/app/repositories/interfaces"`),
		"the interfaces import must appear exactly once")
}

// TestTransformPerResourceReverse_SelfImportIsSkipped covers the guard that
// stops a destination package from importing itself, which would be circular.
func TestTransformPerResourceReverse_SelfImportIsSkipped(t *testing.T) {
	src := `package user

func New() {
	_ = user.NewUserService
}
`
	got, err := TransformPerResourceReverse([]byte(src),
		LayeredDestination{Path: "app/services/user.service.go", PackageName: "services"},
		Options{ModulePath: testMod, Resource: userResource()})
	require.NoError(t, err)
	out := string(got)

	assert.NotContains(t, out, `"example.com/myapp/app/services"`,
		"package services must not import itself")
}

// TestFixSharedDtosImportPathReverse_FlipsAParenthesizedImport covers the
// GenDecl walk, which is what a real file's grouped import block exercises.
func TestFixSharedDtosImportPathReverse_FlipsAParenthesizedImport(t *testing.T) {
	src := `package validators

import (
	"time"

	"example.com/myapp/app/shared/dtos"
)

func Check(in dtos.TPaginationInputDto) time.Time { return time.Now() }
`
	got, err := FixSharedDtosImportPathReverse([]byte(src), testMod)
	require.NoError(t, err)
	out := string(got)

	assert.Contains(t, out, `"example.com/myapp/app/dtos"`)
	assert.NotContains(t, out, "app/shared/dtos")
	assert.Contains(t, out, `"time"`, "unrelated imports in the block must survive")
}
