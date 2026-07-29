package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gofastadev/cli/internal/layout"
	"github.com/stretchr/testify/require"
)

// fakeExecOK swaps the runner execCommand to always succeed via TestHelperProcess.
func fakeExecOK(t *testing.T) {
	t.Helper()
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := make([]string, 0, 3+len(args))
		cs = append(cs, "-test.run=TestGenHelperProcess", "--", name)
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GOFASTA_GEN_HELPER=1",
			"GOFASTA_GEN_EXIT=0",
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })
}

// fakeExec returns an execCommand that runs a TestHelperSub
// subprocess with the configured exit code. Mirrors the pattern used
// in the commands package and powers the AutoVerify + scaffold-RunE
// coverage tests.
func fakeExec(exitCode int) func(name string, args ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperSub", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GENERATE_HELPER=1",
			"GENERATE_EXIT="+strconv.Itoa(exitCode),
		)
		return cmd
	}
}

// makeParentAFile replaces the given path with a regular file so that any
// subsequent MkdirAll on it returns an error. Parent directories are
// created first so only the leaf component is a file.
func makeParentAFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("not a dir"), 0o644))
}

// mkReadOnlyLeaf creates parent dirs at 0o755 then the leaf at 0o555 so the
// leaf exists (MkdirAll is a no-op in the generator) but writes inside fail.
func mkReadOnlyLeaf(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.Mkdir(path, 0o555))
	t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
}

// setupEndpointResource lays out the minimal scaffold layout that
// GenEndpoint expects: controller + routes + service-interface for one
// resource. Returns the project root.
func setupEndpointResource(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))

	mustWriteFile(t, filepath.Join(tmp, "app", "rest", "controllers", "order.controller.go"), `package controllers

import "net/http"

// OrderController is the order REST surface.
type OrderController struct{}

// List handles GET /orders.
func (c *OrderController) List(w http.ResponseWriter, r *http.Request) error { return nil }
`)
	mustWriteFile(t, filepath.Join(tmp, "app", "rest", "routes", "order.routes.go"), `package routes

import (
	"github.com/go-chi/chi/v5"
)

func OrderRoutes(r chi.Router) {
	r.Get("/orders", nil)
}
`)
	mustWriteFile(t, filepath.Join(tmp, "app", "services", "interfaces", "order_service.go"), `package interfaces

import "context"

// OrderServiceInterface is the order business-logic contract.
type OrderServiceInterface interface {
	List(ctx context.Context) error
}
`)
	return tmp
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func setupModelOnlyProject(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "config.yaml"),
		[]byte("database:\n  driver: postgres\n"), 0o644))

	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"), []byte(`package models

import "github.com/google/uuid"

// Order is the customer order entity.
type Order struct {
	ID    uuid.UUID `+"`gorm:\"primaryKey\"`"+`
	Total int       `+"`gorm:\"not null\"`"+`
}
`), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "db", "migrations"), 0o755))

	return tmp
}

var errStubGenerate = stubGenErr("stub")

type stubGenErr string

func (s stubGenErr) Error() string { return string(s) }

func setupScaffoldedResource(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))

	ifaceDir := filepath.Join(tmp, "app", "services", "interfaces")
	require.NoError(t, os.MkdirAll(ifaceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "order_service.go"), []byte(`package interfaces

import "context"

// OrderServiceInterface is the order business-logic contract.
type OrderServiceInterface interface {
	// Create persists a new order.
	Create(ctx context.Context, name string) error
}
`), 0o644))

	implDir := filepath.Join(tmp, "app", "services")
	require.NoError(t, os.MkdirAll(implDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(implDir, "order.service.go"), []byte(`package services

import "context"

type orderService struct{}

func (s *orderService) Create(ctx context.Context, name string) error {
	return nil
}
`), 0o644))

	return tmp
}

// chdirTest is a local test helper — switch cwd for the duration of one
// test, restore on cleanup. Mirrors the one in the commands package; kept
// here so the generate package's tests stay self-contained.
func chdirTest(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// setupMockProject lays out a minimal gofasta project tree under tmp
// with go.mod + one interface file. Returns the project root.
func setupMockProject(t *testing.T, ifaceSrc string) string {
	t.Helper()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))

	ifaceDir := filepath.Join(tmp, "app", "services", "interfaces")
	require.NoError(t, os.MkdirAll(ifaceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "thing_service.go"),
		[]byte(ifaceSrc), 0o644))

	return tmp
}

func setupRelationProject(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "config.yaml"),
		[]byte("database:\n  driver: postgres\n"), 0o644))

	mustWriteFile(t, filepath.Join(tmp, "app", "models", "order.model.go"), `package models

import "github.com/google/uuid"

// Order is the customer order entity.
type Order struct {
	ID uuid.UUID `+"`gorm:\"primaryKey\"`"+`
}
`)
	mustWriteFile(t, filepath.Join(tmp, "app", "models", "customer.model.go"), `package models

import "github.com/google/uuid"

// Customer is the customer entity.
type Customer struct {
	ID uuid.UUID `+"`gorm:\"primaryKey\"`"+`
}
`)
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "db", "migrations"), 0o755))
	return tmp
}

func setupScaffoldedRepo(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))

	ifaceDir := filepath.Join(tmp, "app", "repositories", "interfaces")
	require.NoError(t, os.MkdirAll(ifaceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "order_repository.go"),
		[]byte(`package interfaces

import "context"

// OrderRepositoryInterface persists Order rows.
type OrderRepositoryInterface interface {
	Create(ctx context.Context, name string) error
}
`), 0o644))

	implDir := filepath.Join(tmp, "app", "repositories")
	require.NoError(t, os.MkdirAll(implDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(implDir, "order.repository.go"),
		[]byte(`package repositories

import "context"

type orderRepository struct{}

func (r *orderRepository) Create(ctx context.Context, name string) error {
	return nil
}
`), 0o644))
	return tmp
}

// featureScaffoldData is sampleScaffoldData switched to the feature layout.
func featureScaffoldData() ScaffoldData {
	d := sampleScaffoldData()
	d.Layout = layout.For(layout.Feature)
	return d
}

// resetPlannerState clears any dry-run state left over from earlier
// tests. Called at the top of every test to isolate from other tests
// that toggle the package-level planner flag.
func resetPlannerState(t *testing.T) {
	t.Helper()
	SetDryRun(false)
	// Clear the slice by re-enabling + disabling, which flushes via
	// the "enabled" branch of SetDryRun.
	SetDryRun(true)
	SetDryRun(false)
}

// setupTempProject creates a temp dir with a minimal go.mod and db/migrations dir,
// changes cwd to it, and returns a cleanup function that restores the original cwd.
func setupTempProject(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	os.MkdirAll(filepath.Join(dir, "db", "migrations"), 0755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/testorg/testapp\n\ngo 1.25.0\n"), 0644)
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("database:\n  driver: postgres\n"), 0644)

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

// sampleScaffoldData returns a fully populated ScaffoldData for testing.
func sampleScaffoldData() ScaffoldData {
	return ScaffoldData{
		Name:              "Product",
		LowerName:         "product",
		SnakeName:         "product",
		PluralName:        "Products",
		PluralSnake:       "products",
		PluralLower:       "products",
		Fields:            sampleFields(),
		MigrationNum:      "000001",
		IncludeController: true,
		IncludeGraphQL:    false,
		DBDriver:          "postgres",
		ModulePath:        "github.com/testorg/testapp",
		Layout:            layout.For(layout.Layered),
	}
}

// sampleFields returns a set of fields for testing.
func sampleFields() []Field {
	return []Field{
		{
			Name:            "Name",
			JSONName:        "name",
			SnakeName:       "name",
			GoType:          "string",
			GormType:        `gorm:"not null"`,
			GQLType:         "String",
			SQLType:         "VARCHAR(255) NOT NULL",
			SQLTypePostgres: "VARCHAR(255) NOT NULL",
			SQLTypeMySQL:    "VARCHAR(255) NOT NULL",
			SQLTypeSQLite:   "TEXT NOT NULL",
		},
		{
			Name:            "Price",
			JSONName:        "price",
			SnakeName:       "price",
			GoType:          "float64",
			GormType:        `gorm:"not null"`,
			GQLType:         "Float",
			SQLType:         "DECIMAL(10,2) NOT NULL",
			SQLTypePostgres: "DECIMAL(10,2) NOT NULL",
			SQLTypeMySQL:    "DECIMAL(10,2) NOT NULL",
			SQLTypeSQLite:   "REAL NOT NULL",
		},
	}
}

// writeTestFile is a helper to write a file in the current temp directory.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// readTestFile reads a file and returns its content.
func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
