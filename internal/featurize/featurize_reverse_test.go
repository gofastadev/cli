package featurize

import (
	"go/format"
	"sort"
	"strings"
	"testing"
)

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
