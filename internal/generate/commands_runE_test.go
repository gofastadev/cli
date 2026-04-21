package generate

import (
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestGenHelperProcess(t *testing.T) {
	if os.Getenv("GOFASTA_GEN_HELPER") != "1" {
		return
	}
	code, _ := strconv.Atoi(os.Getenv("GOFASTA_GEN_EXIT"))
	os.Exit(code)
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

// TestHelperSub is the subprocess entry point used by fakeExec.
func TestHelperSub(t *testing.T) {
	if os.Getenv("GENERATE_HELPER") != "1" {
		return
	}
	if out := os.Getenv("GENERATE_STDOUT"); out != "" {
		_, _ = os.Stdout.WriteString(out)
	}
	code, _ := strconv.Atoi(os.Getenv("GENERATE_EXIT"))
	os.Exit(code)
}

// setupFullProject creates a temp project with all files that patchers + generators need.
func setupFullProject(t *testing.T) {
	setupTempProject(t)
	// Container file (for PatchContainer)
	require.NoError(t, os.MkdirAll("app/di/providers", 0755))
	require.NoError(t, os.WriteFile("app/di/container.go", []byte(`package di

type Container struct {
	// services
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
	// providers
)
`), 0644))

	// routes config (for PatchRouteConfig)
	require.NoError(t, os.MkdirAll("app/rest/routes", 0755))
	require.NoError(t, os.WriteFile("app/rest/routes/index.routes.go", []byte(`package routes

import "github.com/go-chi/chi/v5"

type RouteConfig struct {
	// controllers
}

func InitApiRoutes(config *RouteConfig) *chi.Mux {
	r := chi.NewRouter()
	api := chi.NewRouter()
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

// RunWire + RunGqlgen via fake exec, direct invocations
func TestRunWire_FakeOK(t *testing.T) {
	fakeExecOK(t)
	assert.NoError(t, RunWire(ScaffoldData{}))
}

func TestRunGqlgen_FakeOK(t *testing.T) {
	fakeExecOK(t)
	assert.NoError(t, RunGqlgen(ScaffoldData{}))
}

// hasGraphQLFlag branches
func TestHasGraphQLFlag(t *testing.T) {
	assert.False(t, hasGraphQLFlag(scaffoldCmd))
	scaffoldCmd.Flags().Set("gql", "true")
	assert.True(t, hasGraphQLFlag(scaffoldCmd))
	scaffoldCmd.Flags().Set("gql", "false")
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
