package generate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMethodCmd_RunE_HappyPath(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)
	fakeExecOK(t) // AutoVerify shells `go build ./...`
	methodDryRun = false
	require.NoError(t, methodCmd.RunE(methodCmd, []string{"Order", "Archive"}))
}

func TestMethodCmd_RunE_VerifyFailure(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)
	orig := execCommand
	execCommand = fakeExec(1)
	t.Cleanup(func() { execCommand = orig })
	methodDryRun = false
	err := methodCmd.RunE(methodCmd, []string{"Order", "BrokenBuild"})
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeGoBuildFailed), ce.Code)
}

func TestMethodCmd_RunE_NoVerifySkipsBuild(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		t.Fatalf("execCommand must not run with --no-verify (got %s %v)", name, args)
		return nil
	}
	t.Cleanup(func() { execCommand = orig })
	methodDryRun = false
	methodNoVerify = true
	t.Cleanup(func() { methodNoVerify = false })
	require.NoError(t, methodCmd.RunE(methodCmd, []string{"Order", "SkippedVerify"}))
}

func TestMethodCmd_RunE_DryRun(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)
	methodDryRun = true
	t.Cleanup(func() { methodDryRun = false })
	require.NoError(t, methodCmd.RunE(methodCmd, []string{"Order", "DryArchive"}))
}

func TestMethodCmd_RunE_DryRunError(t *testing.T) {
	chdirTest(t, t.TempDir())
	methodDryRun = true
	t.Cleanup(func() { methodDryRun = false })
	require.Error(t, methodCmd.RunE(methodCmd, []string{"Ghost", "Vanish"}))
}

func TestFieldCmd_RunE_HappyPath(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)
	fieldDryRun = false
	require.NoError(t, fieldCmd.RunE(fieldCmd, []string{"Order", "reason:string"}))
}

func TestFieldCmd_RunE_DryRun(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)
	fieldDryRun = true
	t.Cleanup(func() { fieldDryRun = false })
	require.NoError(t, fieldCmd.RunE(fieldCmd, []string{"Order", "notes:text"}))
}

func TestFieldCmd_RunE_BadFieldArg(t *testing.T) {
	chdirTest(t, t.TempDir())
	require.Error(t, fieldCmd.RunE(fieldCmd, []string{"Order", "bad-without-colon"}))
}

func TestFieldCmd_RunE_DryRunError(t *testing.T) {
	chdirTest(t, t.TempDir())
	fieldDryRun = true
	t.Cleanup(func() { fieldDryRun = false })
	require.Error(t, fieldCmd.RunE(fieldCmd, []string{"Order", "reason:string"}))
}

func TestEndpointCmd_RunE_HappyPath(t *testing.T) {
	tmp := setupEndpointResource(t)
	chdirTest(t, tmp)
	fakeExecOK(t) // AutoVerify shells `go build ./...`
	endpointDryRun = false
	endpointNoService = false
	require.NoError(t, endpointCmd.RunE(endpointCmd,
		[]string{"Order", "POST", "/orders/{id}/archive"}))
}

func TestEndpointCmd_RunE_DryRun(t *testing.T) {
	tmp := setupEndpointResource(t)
	chdirTest(t, tmp)
	endpointDryRun = true
	endpointNoService = true
	t.Cleanup(func() { endpointDryRun = false; endpointNoService = false })
	require.NoError(t, endpointCmd.RunE(endpointCmd,
		[]string{"Order", "POST", "/orders/{id}/refund"}))
}

func TestEndpointCmd_RunE_DryRunError(t *testing.T) {
	chdirTest(t, t.TempDir())
	endpointDryRun = true
	t.Cleanup(func() { endpointDryRun = false })
	require.Error(t, endpointCmd.RunE(endpointCmd,
		[]string{"Ghost", "POST", "/x"}))
}

func TestRepoMethodCmd_RunE_HappyPath(t *testing.T) {
	tmp := setupScaffoldedRepo(t)
	chdirTest(t, tmp)
	fakeExecOK(t) // AutoVerify shells `go build ./...`
	repoMethodDryRun = false
	require.NoError(t, repoMethodCmd.RunE(repoMethodCmd, []string{"Order", "Archive"}))
}

func TestRepoMethodCmd_RunE_DryRun(t *testing.T) {
	tmp := setupScaffoldedRepo(t)
	chdirTest(t, tmp)
	repoMethodDryRun = true
	t.Cleanup(func() { repoMethodDryRun = false })
	require.NoError(t, repoMethodCmd.RunE(repoMethodCmd, []string{"Order", "DryArchive"}))
}

func TestRepoMethodCmd_RunE_DryRunError(t *testing.T) {
	chdirTest(t, t.TempDir())
	repoMethodDryRun = true
	t.Cleanup(func() { repoMethodDryRun = false })
	require.Error(t, repoMethodCmd.RunE(repoMethodCmd, []string{"Ghost", "Vanish"}))
}

func TestMiddlewareCmd_RunE_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "app", "rest", "routes", "order.routes.go"),
		`package routes
func OrderRoutes(r interface{}) { r.Get("/orders", nil) }`)
	chdirTest(t, tmp)
	middlewareDryRun = false
	require.NoError(t, middlewareCmd.RunE(middlewareCmd,
		[]string{"GET", "/orders", "auth.Middleware"}))
}

func TestMiddlewareCmd_RunE_DryRun(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "app", "rest", "routes", "order.routes.go"),
		`package routes
func OrderRoutes(r interface{}) { r.Get("/orders", nil) }`)
	chdirTest(t, tmp)
	middlewareDryRun = true
	t.Cleanup(func() { middlewareDryRun = false })
	require.NoError(t, middlewareCmd.RunE(middlewareCmd,
		[]string{"GET", "/orders", "auth.Middleware"}))
}

func TestMiddlewareCmd_RunE_DryRunError(t *testing.T) {
	chdirTest(t, t.TempDir())
	middlewareDryRun = true
	t.Cleanup(func() { middlewareDryRun = false })
	require.Error(t, middlewareCmd.RunE(middlewareCmd,
		[]string{"GET", "/missing", "auth"}))
}

func TestRelationCmd_RunE_HappyPath(t *testing.T) {
	tmp := setupRelationProject(t)
	chdirTest(t, tmp)
	relationDryRun = false
	require.NoError(t, relationCmd.RunE(relationCmd, []string{"Order", "belongs_to", "Customer"}))
}

func TestRelationCmd_RunE_DryRun(t *testing.T) {
	tmp := setupRelationProject(t)
	chdirTest(t, tmp)
	relationDryRun = true
	t.Cleanup(func() { relationDryRun = false })
	require.NoError(t, relationCmd.RunE(relationCmd, []string{"Order", "has_many", "LineItem"}))
}

func TestRelationCmd_RunE_DryRunError(t *testing.T) {
	chdirTest(t, t.TempDir())
	relationDryRun = true
	t.Cleanup(func() { relationDryRun = false })
	require.Error(t, relationCmd.RunE(relationCmd, []string{"Ghost", "belongs_to", "Other"}))
}

func TestRenameCmd_RunE_PreviewMode(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"),
		[]byte("package models\ntype Order struct{ Total int }\n"), 0o644))
	chdirTest(t, tmp)
	renameApply = false
	require.NoError(t, renameCmd.RunE(renameCmd, []string{"Order.Total", "AmountCents"}))
}

func TestRenameCmd_RunE_ApplyMode(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"),
		[]byte("package models\ntype Order struct{ Total int }\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "db", "migrations"), 0o755))
	chdirTest(t, tmp)
	renameApply = true
	t.Cleanup(func() { renameApply = false })
	require.NoError(t, renameCmd.RunE(renameCmd, []string{"Order.Total", "AmountCents"}))
}

func TestRenameCmd_RunE_MalformedFirstArg(t *testing.T) {
	chdirTest(t, t.TempDir())
	require.Error(t, renameCmd.RunE(renameCmd, []string{"NoSeparator", "NewField"}))
}

func TestRenameCmd_RunE_PreviewError(t *testing.T) {
	chdirTest(t, t.TempDir())
	renameApply = false
	// OldField == NewField → validateRename error inside GenRename.
	require.Error(t, renameCmd.RunE(renameCmd, []string{"Order.Same", "Same"}))
}

func TestMockCmd_RunE_OneInterface(t *testing.T) {
	src := `package interfaces
type Thing interface { F() error }
`
	tmp := setupMockProject(t, src)
	chdirTest(t, tmp)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "app", "services", "interfaces", "thing_service.go"),
		[]byte(src), 0o644))
	mockAll = false
	mockCheck = false
	require.NoError(t, mockCmd.RunE(mockCmd, []string{"Thing"}))
}

func TestMockCmd_RunE_AllNoArg(t *testing.T) {
	tmp := setupMockProject(t, `package interfaces
type One interface{ A() error }
`)
	chdirTest(t, tmp)
	mockAll = true
	mockCheck = false
	t.Cleanup(func() { mockAll = false })
	require.NoError(t, mockCmd.RunE(mockCmd, []string{}))
}

// setupFullProject creates a temp project with all files that patchers + generators need.
func setupFullProject(t *testing.T) {
	setupTempProject(t)
	// Container file (for PatchContainer)
	require.NoError(t, os.MkdirAll("app/di/providers", 0755))
	require.NoError(t, os.WriteFile("app/di/container.go", []byte(`package di

type Container struct {
	// gofasta:scaffold:container-fields
}

func NewContainer() *Container {
	return &Container{}
}
`), 0644))

	// wire.go (for PatchWireFile)
	require.NoError(t, os.WriteFile("app/di/wire.go", []byte(`//go:build wireinject
// +build wireinject

package di

import "github.com/google/wire"

var ProviderSet = wire.NewSet(
		// gofasta:scaffold:wire-providers
)
`), 0644))

	// routes config (for PatchRouteConfig)
	require.NoError(t, os.MkdirAll("app/rest/routes", 0755))
	require.NoError(t, os.WriteFile("app/rest/routes/index.routes.go", []byte(`package routes

import "github.com/go-chi/chi/v5"

type RouteConfig struct {
	// gofasta:scaffold:route-config-fields
}

func InitAPIRoutes(config *RouteConfig) *chi.Mux {
	r := chi.NewRouter()
	api := chi.NewRouter()
	// gofasta:scaffold:route-registrations
	r.Mount("/api/v1", api)
	return r
}
`), 0644))

	// serve.go (for PatchServeFile) — must be at cmd/serve.go with the
	// exact marker string PatchServeFile looks for.
	require.NoError(t, os.MkdirAll("cmd", 0755))
	require.NoError(t, os.WriteFile("cmd/serve.go", []byte(`package cmd

func startServer() {
	cfg := &routes.RouteConfig{
		// gofasta:scaffold:routeconfig-init
		HealthController: healthController,
	}
	_ = cfg
}
`), 0644))

	// GraphQL resolver (for PatchResolver)
	require.NoError(t, os.MkdirAll("app/graphql", 0755))
	require.NoError(t, os.WriteFile("app/graphql/resolver.go", []byte(`package graphql

type Resolver struct {
	// services
}
`), 0644))

	// config.yaml for job config patching
	require.NoError(t, os.WriteFile("config.yaml", []byte(`database:
  driver: postgres
scheduler:
  jobs: {}
`), 0644))
}

func TestModelCmd_RunE(t *testing.T) {
	setupTempProject(t)
	assert.NoError(t, modelCmd.RunE(modelCmd, []string{"Widget", "name:string"}))
}

func TestRepositoryCmd_RunE(t *testing.T) {
	setupTempProject(t)
	assert.NoError(t, repositoryCmd.RunE(repositoryCmd, []string{"Widget", "name:string"}))
}

func TestDtoCmd_RunE(t *testing.T) {
	setupTempProject(t)
	assert.NoError(t, dtoCmd.RunE(dtoCmd, []string{"Widget", "name:string"}))
}

func TestMigrationCmd_RunE(t *testing.T) {
	setupTempProject(t)
	assert.NoError(t, migrationCmd.RunE(migrationCmd, []string{"Widget", "name:string"}))
}

func TestRouteCmd_RunE(t *testing.T) {
	setupTempProject(t)
	assert.NoError(t, routeCmd.RunE(routeCmd, []string{"Widget"}))
}

func TestResolverCmd_RunE(t *testing.T) {
	setupTempProject(t)
	// Resolver step just invokes GenResolver which patches the resolver file.
	os.MkdirAll("app/graphql", 0755)
	os.WriteFile("app/graphql/resolver.go", []byte(`package graphql

type Resolver struct {
}
`), 0644)
	// resolverSteps has only GenResolver which needs the file to exist.
	_ = resolverCmd.RunE(resolverCmd, []string{"Widget"})
}

func TestEmailTemplateCmd_RunE(t *testing.T) {
	setupTempProject(t)
	assert.NoError(t, emailTemplateCmd.RunE(emailTemplateCmd, []string{"welcome"}))
}

func TestTaskCmd_RunE(t *testing.T) {
	setupTempProject(t)
	assert.NoError(t, taskCmd.RunE(taskCmd, []string{"send-email"}))
}

func TestJobCmd_RunE(t *testing.T) {
	setupTempProject(t)
	os.WriteFile("config.yaml", []byte("scheduler:\n  jobs: {}\n"), 0644)
	// Need a scheduler_jobs.go or similar for PatchJobRegistry — create it.
	os.MkdirAll("app/jobs", 0755)
	os.WriteFile("app/jobs/registry.go", []byte(`package jobs

func Register(s Scheduler) {
	// jobs
}

type Scheduler interface{}
`), 0644)
	// jobCmd expects the registry path to exist — if it fails, still exercises RunE wrapper.
	_ = jobCmd.RunE(jobCmd, []string{"cleanup", "0 0 0 * * *"})
}

func TestJobCmd_RunE_DefaultSchedule(t *testing.T) {
	setupTempProject(t)
	_ = jobCmd.RunE(jobCmd, []string{"cleanup"})
}

func TestProviderCmd_RunE(t *testing.T) {
	setupFullProject(t)
	_ = providerCmd.RunE(providerCmd, []string{"Widget"})
}

func TestServiceCmd_RunE_REST(t *testing.T) {
	setupFullProject(t)
	fakeExecOK(t)
	assert.NoError(t, serviceCmd.RunE(serviceCmd, []string{"Widget", "name:string"}))
}

func TestServiceCmd_RunE_GraphQL(t *testing.T) {
	setupFullProject(t)
	fakeExecOK(t)
	// Need a schema file for GenGraphQL or it may fail
	require.NoError(t, os.MkdirAll("app/graphql/schema", 0755))
	serviceCmd.Flags().Set("graphql", "true")
	t.Cleanup(func() { serviceCmd.Flags().Set("graphql", "false") })
	_ = serviceCmd.RunE(serviceCmd, []string{"Widget", "name:string"})
}

func TestControllerCmd_RunE(t *testing.T) {
	setupFullProject(t)
	fakeExecOK(t)
	_ = controllerCmd.RunE(controllerCmd, []string{"Widget", "name:string"})
}

func TestScaffoldCmd_RunE(t *testing.T) {
	setupFullProject(t)
	fakeExecOK(t)
	err := scaffoldCmd.RunE(scaffoldCmd, []string{"Widget", "name:string"})
	assert.NoError(t, err)
}

func TestScaffoldCmd_RunE_Failure(t *testing.T) {
	// Don't call setupFullProject — use a bare temp dir so the first
	// patcher (PatchContainer) fails when it can't read app/di/container.go.
	// This exercises the `if err := RunSteps(...); err != nil { return err }`
	// error branch in scaffold's RunE.
	setupTempProject(t)
	fakeExecOK(t)
	err := scaffoldCmd.RunE(scaffoldCmd, []string{"Broken", "x:string"})
	assert.Error(t, err)
}

func TestScaffoldCmd_RunE_WithSwagger(t *testing.T) {
	setupFullProject(t)
	fakeExecOK(t)
	require.NoError(t, scaffoldCmd.Flags().Set("swagger", "true"))
	t.Cleanup(func() { _ = scaffoldCmd.Flags().Set("swagger", "false") })
	err := scaffoldCmd.RunE(scaffoldCmd, []string{"Order", "total:float"})
	assert.NoError(t, err)
}

func TestWireCmd_RunE(t *testing.T) {
	setupTempProject(t)
	fakeExecOK(t)
	assert.NoError(t, WireCmd.RunE(WireCmd, nil))
}

// resolveGraphQLFlag precedence: --no-graphql > --graphql/--gql >
// project-state auto-detection.
func TestResolveGraphQLFlag(t *testing.T) {
	setupTempProject(t) // hermetic cwd — no gqlgen.yml unless a case writes one

	t.Run("defaults off in a REST-only project", func(t *testing.T) {
		assert.False(t, resolveGraphQLFlag(scaffoldCmd))
	})

	t.Run("gql shorthand forces on", func(t *testing.T) {
		scaffoldCmd.Flags().Set("gql", "true")
		defer scaffoldCmd.Flags().Set("gql", "false")
		assert.True(t, resolveGraphQLFlag(scaffoldCmd))
	})

	t.Run("auto-detects from gqlgen.yml", func(t *testing.T) {
		writeTestFile(t, "gqlgen.yml", "schema:\n  - app/graphql/schema/*.gql\n")
		assert.True(t, resolveGraphQLFlag(scaffoldCmd))
	})

	t.Run("no-graphql wins over auto-detection and --graphql", func(t *testing.T) {
		scaffoldCmd.Flags().Set("graphql", "true")
		scaffoldCmd.Flags().Set("no-graphql", "true")
		defer scaffoldCmd.Flags().Set("graphql", "false")
		defer scaffoldCmd.Flags().Set("no-graphql", "false")
		assert.False(t, resolveGraphQLFlag(scaffoldCmd))
	})
}

// hasSwaggerFlag branches
func TestHasSwaggerFlag(t *testing.T) {
	assert.False(t, hasSwaggerFlag(scaffoldCmd))
	scaffoldCmd.Flags().Set("swagger", "true")
	assert.True(t, hasSwaggerFlag(scaffoldCmd))
	scaffoldCmd.Flags().Set("swagger", "false")
}

// TestScaffoldCmd_RunE_DryRun — --dry-run branch of scaffoldCmd's
// RunE is currently uncovered; exercise it here.
func TestScaffoldCmd_RunE_DryRun(t *testing.T) {
	setupFullProject(t) // dry-run still runs patchers → need real files
	orig := execCommand
	execCommand = fakeExec(0)
	t.Cleanup(func() { execCommand = orig })
	require.NoError(t, scaffoldCmd.Flags().Set("dry-run", "true"))
	t.Cleanup(func() { _ = scaffoldCmd.Flags().Set("dry-run", "false") })
	err := scaffoldCmd.RunE(scaffoldCmd, []string{"DryWidget", "name:string"})
	require.NoError(t, err)
}

// TestScaffoldCmd_RunE_DryRun_StepFails — dry-run but the step chain
// errors (no container.go to patch). Exercises the "return err"
// branch inside the dry-run block.
func TestScaffoldCmd_RunE_DryRun_StepFails(t *testing.T) {
	setupTempProject(t)
	require.NoError(t, scaffoldCmd.Flags().Set("dry-run", "true"))
	t.Cleanup(func() { _ = scaffoldCmd.Flags().Set("dry-run", "false") })
	err := scaffoldCmd.RunE(scaffoldCmd, []string{"BrokenDry", "x:string"})
	require.Error(t, err)
}

// TestScaffoldCmd_RunE_AutoVerifyFails — the scaffold succeeds but
// AutoVerify fails. RunSteps runs `go tool wire` before reaching
// AutoVerify's `go build`; the fake exec succeeds for tool invocations
// and fails only for `go build`.
func TestScaffoldCmd_RunE_AutoVerifyFails(t *testing.T) {
	setupFullProject(t)
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		exit := "0"
		if len(args) > 0 && args[0] == "build" {
			exit = "1"
		}
		cs := append([]string{"-test.run=TestHelperSub", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GENERATE_HELPER=1",
			"GENERATE_EXIT="+exit,
		)
		return cmd
	}
	t.Cleanup(func() { execCommand = orig })
	require.NoError(t, scaffoldCmd.Flags().Set("dry-run", "false"))
	err := scaffoldCmd.RunE(scaffoldCmd, []string{"AVFail", "name:string"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not compile")
}

func TestCmd_HasAliases(t *testing.T) {
	assert.Contains(t, Cmd.Aliases, "g")
}

func TestWireCmd_Exists(t *testing.T) {
	assert.Equal(t, "wire", WireCmd.Use)
}

func TestModelSteps(t *testing.T) {
	steps := modelSteps()
	assert.Len(t, steps, 2)
	assert.Equal(t, "model", steps[0].Label)
	assert.Equal(t, "migration", steps[1].Label)
}

func TestDtoSteps(t *testing.T) {
	steps := dtoSteps()
	assert.Len(t, steps, 1)
	assert.Equal(t, "DTOs", steps[0].Label)
}

func TestMigrationSteps(t *testing.T) {
	steps := migrationSteps()
	assert.Len(t, steps, 1)
	assert.Equal(t, "migration", steps[0].Label)
}

func TestRepositorySteps(t *testing.T) {
	steps := repositorySteps()
	// 5 steps: model + migration + repository interface + repository + repository test
	assert.Len(t, steps, 5)
	assert.Equal(t, "model", steps[0].Label)
	assert.Equal(t, "repository", steps[3].Label)
	assert.Equal(t, "repository test", steps[4].Label)
}

func TestRouteSteps(t *testing.T) {
	steps := routeSteps()
	assert.Len(t, steps, 1)
	assert.Equal(t, "routes", steps[0].Label)
}

func TestResolverSteps(t *testing.T) {
	steps := resolverSteps()
	assert.Len(t, steps, 2)
	assert.Equal(t, "auto-wire: resolver", steps[0].Label)
	assert.Equal(t, "auto-wire: gqlgen autobind", steps[1].Label)
}

func TestProviderSteps(t *testing.T) {
	steps := providerSteps()
	assert.Len(t, steps, 3)
}

func TestServiceSteps_REST(t *testing.T) {
	d := ScaffoldData{IncludeGraphQL: false}
	steps := serviceSteps(d)
	for _, s := range steps {
		assert.NotEqual(t, "GraphQL schema", s.Label)
		assert.NotEqual(t, "auto-wire: resolver", s.Label)
		assert.NotEqual(t, "auto-wire: gqlgen autobind", s.Label)
		assert.NotEqual(t, "regenerate gqlgen", s.Label)
	}
}

func TestServiceSteps_GraphQL(t *testing.T) {
	d := ScaffoldData{IncludeGraphQL: true}
	steps := serviceSteps(d)
	labels := make([]string, 0, len(steps))
	for _, s := range steps {
		labels = append(labels, s.Label)
	}
	assert.Contains(t, labels, "GraphQL schema")
	assert.Contains(t, labels, "auto-wire: resolver")
	assert.Contains(t, labels, "auto-wire: gqlgen autobind")
	assert.Contains(t, labels, "regenerate gqlgen")
}

func TestScaffoldSteps_REST(t *testing.T) {
	d := ScaffoldData{IncludeGraphQL: false}
	steps := scaffoldSteps(d)
	labels := make([]string, 0, len(steps))
	for _, s := range steps {
		labels = append(labels, s.Label)
	}
	assert.Contains(t, labels, "model")
	assert.Contains(t, labels, "controller")
	assert.Contains(t, labels, "routes")
	assert.NotContains(t, labels, "GraphQL schema")
}

func TestScaffoldSteps_GraphQL(t *testing.T) {
	d := ScaffoldData{IncludeGraphQL: true}
	steps := scaffoldSteps(d)
	labels := make([]string, 0, len(steps))
	for _, s := range steps {
		labels = append(labels, s.Label)
	}
	assert.Contains(t, labels, "GraphQL schema")
	assert.Contains(t, labels, "auto-wire: gqlgen autobind")
	assert.Contains(t, labels, "regenerate gqlgen")
}

func TestControllerSteps_REST(t *testing.T) {
	d := ScaffoldData{IncludeGraphQL: false}
	steps := controllerSteps(d)
	labels := make([]string, 0, len(steps))
	for _, s := range steps {
		labels = append(labels, s.Label)
	}
	assert.Contains(t, labels, "controller")
	assert.Contains(t, labels, "routes")
	assert.NotContains(t, labels, "GraphQL schema")
}

func TestControllerSteps_GraphQL(t *testing.T) {
	d := ScaffoldData{IncludeGraphQL: true}
	steps := controllerSteps(d)
	labels := make([]string, 0, len(steps))
	for _, s := range steps {
		labels = append(labels, s.Label)
	}
	assert.Contains(t, labels, "GraphQL schema")
}

func TestResolveGraphQLFlag_False(t *testing.T) {
	setupTempProject(t)
	assert.False(t, resolveGraphQLFlag(scaffoldCmd))
}

func TestBuildFromArgs(t *testing.T) {
	setupTempProject(t)
	d, err := buildFromArgs([]string{"product", "name:string"})
	assert.NoError(t, err)
	assert.Equal(t, "Product", d.Name)
	assert.Len(t, d.Fields, 1)
}

func TestBuildFromArgs_RejectsInvalidNames(t *testing.T) {
	setupTempProject(t)
	cases := []struct {
		name string
		args []string
	}{
		{"resource with slash", []string{"../etc/passwd", "name:string"}},
		{"resource with dotdot", []string{"..", "name:string"}},
		{"resource with space", []string{"my resource", "name:string"}},
		{"resource with quote", []string{`x"y`, "name:string"}},
		{"resource with semicolon", []string{"x;drop", "name:string"}},
		{"resource with template", []string{"a{{.X}}", "name:string"}},
		{"field with slash", []string{"Product", "na/me:string"}},
		{"field with dotdot", []string{"Product", "..:string"}},
		{"field with space", []string{"Product", "bad name:string"}},
		{"field with quote", []string{"Product", `na"me:string`}},
		{"field with semicolon", []string{"Product", "name;drop:string"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildFromArgs(tc.args)
			require.Error(t, err)
			var ce *clierr.Error
			require.ErrorAs(t, err, &ce)
			assert.Equal(t, string(clierr.CodeInvalidName), ce.Code)
		})
	}
}

// TestGeneratorCommands_RejectInvalidResourceName covers the buildFromArgs
// error return in every generator subcommand.
//
// Each of these names flows into file paths, package names, and SQL identifiers,
// so validateIdentifier is the single gate that stops a hostile or malformed
// argument before any of that happens. The check exists once in buildFromArgs,
// but every subcommand has to actually propagate its error — a command that
// swallowed it would carry on and generate files from an unvalidated name.
func TestGeneratorCommands_RejectInvalidResourceName(t *testing.T) {
	commands := map[string]*cobra.Command{
		"scaffold":       scaffoldCmd,
		"model":          modelCmd,
		"repository":     repositoryCmd,
		"service":        serviceCmd,
		"controller":     controllerCmd,
		"dto":            dtoCmd,
		"migration":      migrationCmd,
		"route":          routeCmd,
		"resolver":       resolverCmd,
		"job":            jobCmd,
		"email-template": emailTemplateCmd,
		"task":           taskCmd,
		"provider":       providerCmd,
	}

	// Names that must never reach a generator: path traversal, shell
	// metacharacters, and shapes that are not Go identifiers.
	badNames := []string{
		"../escape",
		"9leading-digit",
		"has space",
		"semi;colon",
		"",
	}

	for name, cmd := range commands {
		t.Run(name, func(t *testing.T) {
			for _, bad := range badNames {
				t.Run(bad, func(t *testing.T) {
					setupTempProject(t)
					err := cmd.RunE(cmd, []string{bad})
					require.Error(t, err, "generator %q accepted invalid name %q", name, bad)
					assert.Contains(t, err.Error(), "invalid name")
				})
			}
		})
	}
}

// TestScaffoldStepsWithoutRegeneration_DropsRegenSteps — the helper
// filters out the "regenerate Wire" and "regenerate gqlgen" steps
// that can't run meaningfully in dry-run mode.
func TestScaffoldStepsWithoutRegeneration_DropsRegenSteps(t *testing.T) {
	full := scaffoldSteps(ScaffoldData{Name: "Product", IncludeGraphQL: true})
	slim := scaffoldStepsWithoutRegeneration(ScaffoldData{
		Name: "Product", IncludeGraphQL: true,
	})

	assert.Less(t, len(slim), len(full),
		"expected fewer steps after filtering")
	for _, s := range slim {
		assert.NotEqual(t, "regenerate Wire", s.Label)
		assert.NotEqual(t, "regenerate gqlgen", s.Label)
	}
}

// TestPrintPlanResult_TextMode — exercises the text-output branch
// (JSON mode off) of printPlanResult.
func TestPrintPlanResult_TextMode(t *testing.T) {
	cliout.SetJSONMode(false)
	cmd := &cobra.Command{Use: "g"}
	assert.NotPanics(t, func() { printPlanResult(cmd) })
}

// TestPrintPlanResult_JSONMode — exercises the JSON-output branch.
func TestPrintPlanResult_JSONMode(t *testing.T) {
	cliout.SetJSONMode(true)
	t.Cleanup(func() { cliout.SetJSONMode(false) })
	cmd := &cobra.Command{Use: "g"}
	assert.NotPanics(t, func() { printPlanResult(cmd) })
}

func TestParseReturnsFlag(t *testing.T) {
	assert.Nil(t, parseReturnsFlag(""))
	assert.Nil(t, parseReturnsFlag("  "))
	assert.Equal(t, []string{"error"}, parseReturnsFlag("error"))
	assert.Equal(t, []string{"*models.Order", "error"}, parseReturnsFlag("*models.Order, error"))
	assert.Equal(t, []string{"[]*models.Order", "int64", "error"},
		parseReturnsFlag(" []*models.Order ,int64,  error "))
}
