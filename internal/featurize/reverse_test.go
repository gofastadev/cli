// Coverage for reverse.go — the feature → layered direction:
// TransformPerResourceReverse and its per-destination handling.

package featurize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixSharedDtosImportPathReverse(t *testing.T) {
	src := `package validators

import (
	dtos "example.com/myapp/app/shared/dtos"
)

func f() { _ = dtos.TPaginationObjectDto{} }
`
	got, err := FixSharedDtosImportPathReverse([]byte(src), "example.com/myapp")
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, `"example.com/myapp/app/dtos"`) {
		t.Errorf("import not flipped back to app/dtos:\n%s", out)
	}
	if strings.Contains(out, "app/shared/dtos") {
		t.Errorf("shared/dtos import path still present:\n%s", out)
	}
}

func TestTransformPerResourceReverse_Service(t *testing.T) {
	// Feature-shaped service file (package user): cross-package repo
	// interface reference is bare; same-package symbols stay bare.
	src := `package user

import (
	"github.com/google/uuid"

	"example.com/myapp/app/models"
)

type UserService struct {
	repo  UserRepositoryInterface
	pwGen PasswordGenerator
}

func NewUserService(repo UserRepositoryInterface, pwGen PasswordGenerator) *UserService {
	return &UserService{repo: repo, pwGen: pwGen}
}

func (s *UserService) Get(ctx uuid.UUID) (*models.User, error) {
	return nil, ErrUserNotFound
}
`
	got, err := TransformPerResourceReverse([]byte(src),
		LayeredDestination{Path: "app/services/user.service.go", PackageName: "services"},
		Options{
			ModulePath: "example.com/myapp",
			Resource:   Resource{Name: "User", Snake: "user", Plural: "Users"},
		})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, "package services") {
		t.Errorf("package decl not reverted to services:\n%s", out)
	}
	// Cross-package repo interface re-qualified.
	if !strings.Contains(out, "repoInterfaces.UserRepositoryInterface") {
		t.Errorf("UserRepositoryInterface not re-qualified to repoInterfaces:\n%s", out)
	}
	if !strings.Contains(out, `"example.com/myapp/app/repositories/interfaces"`) {
		t.Errorf("repoInterfaces import not added:\n%s", out)
	}
	// Same-package service symbols stay bare.
	if strings.Contains(out, "services.PasswordGenerator") {
		t.Errorf("PasswordGenerator should stay bare in package services:\n%s", out)
	}
	if strings.Contains(out, "services.ErrUserNotFound") {
		t.Errorf("ErrUserNotFound should stay bare in package services:\n%s", out)
	}
	// Model reference preserved.
	if !strings.Contains(out, "models.User") {
		t.Errorf("models.User reference should be preserved:\n%s", out)
	}
}

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

// TestBuildReverseSymbolMap_DtosEntriesMatchSharedList pins the
// extraction of the per-resource dtos symbol list: buildReverseSymbolMap
// must map to the dtos package exactly the names that
// perResourceDtoSymbolNames returns — the same list the GraphQL
// transforms consume. If either side gains a symbol the other misses,
// forward and reverse migrations would disagree about where it lives.
func TestBuildReverseSymbolMap_DtosEntriesMatchSharedList(t *testing.T) {
	r := userResource()
	symMap := buildReverseSymbolMap(r)

	var dtosEntries []string
	for name, pkg := range symMap {
		if pkg.alias == "dtos" {
			dtosEntries = append(dtosEntries, name)
		}
	}
	assert.ElementsMatch(t, perResourceDtoSymbolNames(r), dtosEntries)
}

// TestExpectedResourceSymbols pins the public recognition surface: it
// must contain everything buildReverseSymbolMap recognizes plus the
// rename-handled routes functions and the wire provider set. The
// refactor preflight diffs real projects against this set — a symbol
// missing here produces false "renamed" warnings for scaffold-shaped
// projects (the preflight's pristine-project tests catch that end to
// end; this pins the contract locally).
func TestExpectedResourceSymbols(t *testing.T) {
	r := userResource()
	got := ExpectedResourceSymbols(r)

	for name := range buildReverseSymbolMap(r) {
		assert.True(t, got[name], "missing reverse-map symbol %q", name)
	}
	for _, extra := range []string{"UserRoutes", "RegisterRoutes", "UserSet"} {
		assert.True(t, got[extra], "missing %q", extra)
	}
}
