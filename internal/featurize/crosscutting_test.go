// Coverage for crosscutting.go — the shared per-layout files whose
// import shape changes (container, wire, index routes, mocks, core providers).

package featurize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestTransformCrossCutting_ParseError(t *testing.T) {
	_, err := TransformContainer([]byte("package di\n\nfunc Broken( {\n"), testMod, []Resource{userResource()})
	require.Error(t, err)
}

func TestTransformContainerReverse(t *testing.T) {
	// Feature-shaped container.go: per-feature alias import + userpkg
	// qualified field types.
	src := `package di

import (
	"gorm.io/gorm"

	userpkg "example.com/myapp/app/user"
)

type ServiceContainer struct {
	DB             *gorm.DB
	UserRepo       userpkg.UserRepositoryInterface
	UserService    userpkg.UserServiceInterface
	UserController *userpkg.UserController
}
`
	got, err := TransformContainerReverse([]byte(src), "example.com/myapp", []Resource{
		{Name: "User", Snake: "user", Plural: "Users"},
	})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	out := string(got)
	if strings.Contains(out, "userpkg") {
		t.Errorf("userpkg alias still present:\n%s", out)
	}
	if !strings.Contains(out, "repoInterfaces.UserRepositoryInterface") {
		t.Errorf("field not reverted to repoInterfaces.UserRepositoryInterface:\n%s", out)
	}
	if !strings.Contains(out, "svcInterfaces.UserServiceInterface") {
		t.Errorf("field not reverted to svcInterfaces.UserServiceInterface:\n%s", out)
	}
	if !strings.Contains(out, "*controllers.UserController") {
		t.Errorf("field not reverted to *controllers.UserController:\n%s", out)
	}
	if !strings.Contains(out, `"example.com/myapp/app/repositories/interfaces"`) {
		t.Errorf("repoInterfaces import not added back:\n%s", out)
	}
}

func TestTransformWireReverse(t *testing.T) {
	src := `//go:build wireinject

package di

import (
	"github.com/google/wire"

	"example.com/myapp/app/di/providers"
	userpkg "example.com/myapp/app/user"
)

func InitializeServiceContainer() (*ServiceContainer, error) {
	wire.Build(
		providers.CoreSet,
		userpkg.UserSet,
		wire.Struct(new(ServiceContainer), "*"),
	)
	return nil, nil
}
`
	got, err := TransformWireReverse([]byte(src), "example.com/myapp", []Resource{
		{Name: "User", Snake: "user", Plural: "Users"},
	})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, "providers.UserSet") {
		t.Errorf("userpkg.UserSet not reverted to providers.UserSet:\n%s", out)
	}
	if strings.Contains(out, "userpkg") {
		t.Errorf("userpkg alias still present:\n%s", out)
	}
	// providers.CoreSet still references providers, so the import stays.
	if !strings.Contains(out, "providers.CoreSet") {
		t.Errorf("providers.CoreSet reference lost:\n%s", out)
	}
	if !strings.Contains(out, `"example.com/myapp/app/di/providers"`) {
		t.Errorf("providers import should remain:\n%s", out)
	}
}

func TestTransformIndexRoutesReverse(t *testing.T) {
	src := `package routes

import (
	"github.com/go-chi/chi/v5"

	userpkg "example.com/myapp/app/user"
)

type RouteConfig struct {
	UserController *userpkg.UserController
}

func InitAPIRoutes(config *RouteConfig) *chi.Mux {
	r := chi.NewRouter()
	api := chi.NewRouter()
	userpkg.RegisterRoutes(api, config.UserController)
	r.Mount("/api/v1", api)
	return r
}
`
	got, err := TransformIndexRoutesReverse([]byte(src), "example.com/myapp", []Resource{
		{Name: "User", Snake: "user", Plural: "Users"},
	})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, "*controllers.UserController") {
		t.Errorf("RouteConfig field not reverted to *controllers.UserController:\n%s", out)
	}
	if !strings.Contains(out, "UserRoutes(api") {
		t.Errorf("userpkg.RegisterRoutes not reverted to UserRoutes call:\n%s", out)
	}
	if strings.Contains(out, "userpkg") {
		t.Errorf("userpkg alias still present:\n%s", out)
	}
	if strings.Contains(out, "RegisterRoutes") {
		t.Errorf("RegisterRoutes call not reverted:\n%s", out)
	}
}

func TestTransformCoreProvidersReverse(t *testing.T) {
	src := `package providers

import (
	userpkg "example.com/myapp/app/user"
)

func providePasswordGenerator() userpkg.PasswordGenerator {
	return userpkg.NewDefaultPasswordGenerator()
}
`
	got, err := TransformCoreProvidersReverse([]byte(src), "example.com/myapp", []Resource{
		{Name: "User", Snake: "user", Plural: "Users"},
	})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, "services.NewDefaultPasswordGenerator") {
		t.Errorf("password generator not reverted to services.NewDefaultPasswordGenerator:\n%s", out)
	}
	if strings.Contains(out, "userpkg.NewDefaultPasswordGenerator") {
		t.Errorf("userpkg qualifier not removed from password generator:\n%s", out)
	}
	if !strings.Contains(out, `"example.com/myapp/app/services"`) {
		t.Errorf("services import not added back:\n%s", out)
	}
}

func TestTransformCoreProvidersReverse_NoUser(t *testing.T) {
	// No user resource → the transform is a no-op passthrough.
	src := `package providers

func provide() {}
`
	got, err := TransformCoreProvidersReverse([]byte(src), "example.com/myapp", []Resource{
		{Name: "Order", Snake: "order", Plural: "Orders"},
	})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	if string(got) != src {
		t.Errorf("expected passthrough when no user resource, got:\n%s", got)
	}
}

func TestTransformMockReverse(t *testing.T) {
	src := `package mocks

import (
	"example.com/myapp/app/models"
	userpkg "example.com/myapp/app/user"
)

type UserRepositoryMock struct{}

func (m *UserRepositoryMock) Iface() userpkg.UserRepositoryInterface {
	return nil
}

func (m *UserRepositoryMock) Get() (*models.User, error) {
	return nil, userpkg.ErrUserNotDeletable
}
`
	got, err := TransformMockReverse([]byte(src), "example.com/myapp", Resource{
		Name: "User", Snake: "user", Plural: "Users",
	})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	out := string(got)
	if strings.Contains(out, "userpkg") {
		t.Errorf("userpkg alias still present:\n%s", out)
	}
	if !strings.Contains(out, "repoInterfaces.UserRepositoryInterface") {
		t.Errorf("not reverted to repoInterfaces.UserRepositoryInterface:\n%s", out)
	}
	if !strings.Contains(out, "repoInterfaces.ErrUserNotDeletable") {
		t.Errorf("not reverted to repoInterfaces.ErrUserNotDeletable:\n%s", out)
	}
	// models.User stays a cross-package reference (model does not move).
	if !strings.Contains(out, "models.User") {
		t.Errorf("models.User reference should be preserved:\n%s", out)
	}
}

func TestTransformContainer(t *testing.T) {
	src := `package di

import (
	"gorm.io/gorm"
	repoInterfaces "example.com/myapp/app/repositories/interfaces"
	svcInterfaces "example.com/myapp/app/services/interfaces"
	"example.com/myapp/app/rest/controllers"
)

type ServiceContainer struct {
	DB             *gorm.DB
	UserRepo       repoInterfaces.UserRepositoryInterface
	UserService    svcInterfaces.UserServiceInterface
	UserController *controllers.UserController
}
`
	got, err := TransformContainer([]byte(src), "example.com/myapp", []Resource{
		{Name: "User", Snake: "user", Plural: "Users"},
	})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	out := string(got)
	if strings.Contains(out, "repoInterfaces") {
		t.Errorf("repoInterfaces alias still present:\n%s", out)
	}
	if !strings.Contains(out, `userpkg "example.com/myapp/app/user"`) {
		t.Errorf("userpkg import not added:\n%s", out)
	}
	if !strings.Contains(out, "userpkg.UserRepositoryInterface") {
		t.Errorf("UserRepositoryInterface not rewritten to userpkg.UserRepositoryInterface:\n%s", out)
	}
	if !strings.Contains(out, "*userpkg.UserController") {
		t.Errorf("*userpkg.UserController not produced:\n%s", out)
	}
}

func TestTransformWire(t *testing.T) {
	src := `//go:build wireinject

package di

import (
	"github.com/google/wire"
	"example.com/myapp/app/di/providers"
)

func InitializeServiceContainer() (*ServiceContainer, error) {
	wire.Build(
		providers.CoreSet,
		providers.UserSet,
		wire.Struct(new(ServiceContainer), "*"),
	)
	return nil, nil
}
`
	got, err := TransformWire([]byte(src), "example.com/myapp", []Resource{
		{Name: "User", Snake: "user", Plural: "Users"},
	})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, "userpkg.UserSet") {
		t.Errorf("providers.UserSet not rewritten to userpkg.UserSet:\n%s", out)
	}
	// providers.CoreSet still references the providers package, so
	// the import must stay (conservative drop — never strip an import
	// whose alias still has live references).
	if !strings.Contains(out, "providers.CoreSet") {
		t.Errorf("providers.CoreSet should still be referenced (CoreSet doesn't move):\n%s", out)
	}
	if !strings.Contains(out, `"example.com/myapp/app/di/providers"`) {
		t.Errorf("providers import should stay because providers.CoreSet still references it:\n%s", out)
	}
}

func TestTransformIndexRoutes(t *testing.T) {
	src := `package routes

import (
	"github.com/go-chi/chi/v5"

	"example.com/myapp/app/rest/controllers"
)

type RouteConfig struct {
	UserController *controllers.UserController
}

func InitAPIRoutes(config *RouteConfig) *chi.Mux {
	r := chi.NewRouter()
	api := chi.NewRouter()
	UserRoutes(api, config.UserController)
	r.Mount("/api/v1", api)
	return r
}
`
	got, err := TransformIndexRoutes([]byte(src), "example.com/myapp", []Resource{
		{Name: "User", Snake: "user", Plural: "Users"},
	})
	if err != nil {
		t.Fatalf("transform error: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, "*userpkg.UserController") {
		t.Errorf("RouteConfig field type not rewritten:\n%s", out)
	}
	if !strings.Contains(out, "userpkg.RegisterRoutes(api") {
		t.Errorf("UserRoutes call not rewritten to userpkg.RegisterRoutes:\n%s", out)
	}
}
