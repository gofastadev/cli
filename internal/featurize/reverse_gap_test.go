// Additional coverage for reverse.go — the symbol-requalification table and
// the import bookkeeping that unwinds a feature package back into its layered
// destination.

package featurize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
