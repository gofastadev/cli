package featurize

import (
	"strings"
	"testing"
)

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

func TestPerResourceMapping_AllPaths(t *testing.T) {
	pairs := PerResourceMapping("user")
	wantCount := 15 // models + validators stay in shared layered dirs; dtos collapse into feature per Option B
	if len(pairs) != wantCount {
		t.Errorf("PerResourceMapping returned %d pairs, want %d", len(pairs), wantCount)
	}
	expectedFeaturePaths := []string{
		"app/user/service.go",
		"app/user/service_iface.go",
		"app/user/controller.go",
		"app/user/routes.go",
		"app/user/wire.go",
		"app/user/errors.go",
		"app/user/repository_iface.go",
	}
	got := make(map[string]bool)
	for _, p := range pairs {
		got[p.Feature] = true
	}
	for _, want := range expectedFeaturePaths {
		if !got[want] {
			t.Errorf("PerResourceMapping missing feature path: %s", want)
		}
	}
}
