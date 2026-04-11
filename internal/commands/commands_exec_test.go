package commands

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errDummy = errors.New("dummy")

// chdirTemp creates a new temp dir and cd's into it for the duration of the test.
func chdirTemp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
}

// writeConfigYAML drops a minimal postgres config.yaml into cwd.
func writeConfigYAML(t *testing.T) {
	t.Helper()
	content := `database:
  driver: postgres
  user: u
  password: p
  host: localhost
  port: "5432"
  name: db
server:
  port: "8080"
`
	require.NoError(t, os.WriteFile("config.yaml", []byte(content), 0644))
}

// --- runMigration ---

func TestRunMigration_FakeSuccess(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	assert.NoError(t, runMigration("up"))
}

func TestRunMigration_FakeFailure(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 1)
	err := runMigration("down")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "migration down failed")
}

// --- runDBReset ---

func TestRunDBReset_FakeSuccess_SkipSeed(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	assert.NoError(t, runDBReset(true))
}

func TestRunDBReset_FakeSuccess_WithSeed(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	assert.NoError(t, runDBReset(false))
}

func TestRunDBReset_DropFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 1)
	err := runDBReset(true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "drop failed")
}

// stagedFakeExec returns a fake that exits with code[i] on the i-th call,
// repeating the final code if there are more calls than codes.
func stagedFakeExec(t *testing.T, codes ...int) {
	t.Helper()
	orig := execCommand
	call := 0
	execCommand = func(name string, args ...string) *exec.Cmd {
		code := codes[len(codes)-1]
		if call < len(codes) {
			code = codes[call]
		}
		call++
		return fakeExecCommand(code)(name, args...)
	}
	t.Cleanup(func() { execCommand = orig })
}

func TestRunDBReset_UpFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	stagedFakeExec(t, 0, 1) // drop ok, up fails
	err := runDBReset(true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "migration up failed")
}

func TestRunDBReset_SeedFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	stagedFakeExec(t, 0, 0, 1) // drop ok, up ok, seed fails
	err := runDBReset(false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "seeding failed")
}

// --- RunE wrappers ---
// These exist so we cover the anonymous func bodies inside each cobra.Command
// definition, which direct calls to runX bypass.

func TestDevCmd_RunE(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	assert.NoError(t, devCmd.RunE(devCmd, nil))
}

func TestConsoleCmd_RunE(t *testing.T) {
	orig := execLookPath
	execLookPath = func(name string) (string, error) { return "/fake/yaegi", nil }
	t.Cleanup(func() { execLookPath = orig })
	withFakeExec(t, 0)
	assert.NoError(t, consoleCmd.RunE(consoleCmd, nil))
}

func TestInitCmd_RunE(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	assert.NoError(t, initCmd.RunE(initCmd, nil))
}

func TestDoctorCmd_RunE(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	assert.NoError(t, doctorCmd.RunE(doctorCmd, nil))
}

func TestRoutesCmd_RunE(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.MkdirAll("app/rest/routes", 0755))
	assert.NoError(t, routesCmd.RunE(routesCmd, nil))
}

func TestMigrateUpCmd_RunE(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	assert.NoError(t, migrateUpCmd.RunE(migrateUpCmd, nil))
}

func TestMigrateDownCmd_RunE(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	assert.NoError(t, migrateDownCmd.RunE(migrateDownCmd, nil))
}

func TestDbResetCmd_RunE(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	assert.NoError(t, dbResetCmd.RunE(dbResetCmd, nil))
}

func TestNewCmd_RunE(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	// Set both flags so we cover the || branch
	newCmd.Flags().Set("graphql", "true")
	t.Cleanup(func() { newCmd.Flags().Set("graphql", "false") })
	assert.NoError(t, newCmd.RunE(newCmd, []string{"runelocaltestapp"}))
}

func TestUpgradeCmd_RunE(t *testing.T) {
	swapHTTP(t, func(url string) (*http.Response, error) { return nil, errDummy })
	err := upgradeCmd.RunE(upgradeCmd, nil)
	assert.Error(t, err)
}

// rootCmd.Run (the Run func that shows help + banner) is invoked when no subcommand is given.
func TestRootCmd_Run_Help(t *testing.T) {
	// rootCmd.SetArgs([]string{}) with no Execute call still goes through runExecute("")
	rootCmd.SetArgs([]string{})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	assert.NoError(t, runExecute("test"))
}

// --- runInit ---

func TestRunInit_FakeSuccess(t *testing.T) {
	chdirTemp(t)
	// Create .env.example so runInit uses that branch
	os.WriteFile(".env.example", []byte("FOO=bar\n"), 0644)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	assert.NoError(t, runInit())
	// .env should exist
	_, err := os.Stat(".env")
	assert.NoError(t, err)
}

func TestRunInit_EnvAlreadyExists(t *testing.T) {
	chdirTemp(t)
	os.WriteFile(".env", []byte("existing\n"), 0644)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	assert.NoError(t, runInit())
}

func TestRunInit_NoEnvExample(t *testing.T) {
	chdirTemp(t)
	// No .env, no .env.example — should create empty .env
	writeConfigYAML(t)
	withFakeExec(t, 0)
	assert.NoError(t, runInit())
	_, err := os.Stat(".env")
	assert.NoError(t, err)
}

func TestRunInit_WithGQLGen(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	os.WriteFile("gqlgen.yml", []byte("schema: schema.graphql\n"), 0644)
	withFakeExec(t, 0)
	assert.NoError(t, runInit())
}

func TestRunInit_ModTidyFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 1)
	err := runInit()
	assert.Error(t, err)
}

// Staged: go mod tidy ok, wire fails (warning, not fatal), gqlgen skipped,
// migrate ok (stage 4), go build ok (stage 5).
func TestRunInit_WireFails_NonFatal(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	// Stages: tidy=0, wire=1, migrate=0, build=0
	stagedFakeExec(t, 0, 1, 0, 0)
	assert.NoError(t, runInit())
}

// gqlgen failure (non-fatal warning)
func TestRunInit_GqlgenFails_NonFatal(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	os.WriteFile("gqlgen.yml", []byte("schema: s\n"), 0644)
	// Stages: tidy=0, wire=0, gqlgen=1, migrate=0, build=0
	stagedFakeExec(t, 0, 0, 1, 0, 0)
	assert.NoError(t, runInit())
}

// migrate failure (non-fatal warning)
func TestRunInit_MigrateFails_NonFatal(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	// Stages: tidy=0, wire=0, migrate=1, build=0
	stagedFakeExec(t, 0, 0, 1, 0)
	assert.NoError(t, runInit())
}

// build failure is fatal
func TestRunInit_BuildFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	// Stages: tidy=0, wire=0, migrate=0, build=1
	stagedFakeExec(t, 0, 0, 0, 1)
	err := runInit()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "build")
}

// --- runDev ---

func TestRunDev_FakeSuccess(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	// runDev starts air in foreground, fake exits 0 immediately, returns nil
	assert.NoError(t, runDev())
}

func TestRunDev_WithGraphQLFile(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	os.WriteFile("gqlgen.yml", []byte("schema: s\n"), 0644)
	withFakeExec(t, 0)
	assert.NoError(t, runDev())
}

func TestRunDev_AirFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 1)
	// Both migrate + air "fail" — migrate is non-fatal, air error returns
	err := runDev()
	assert.Error(t, err)
}

// --- runConsole ---

func TestRunConsole_YaegiNotFound(t *testing.T) {
	orig := execLookPath
	execLookPath = func(name string) (string, error) {
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { execLookPath = orig })

	err := runConsole()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "yaegi")
}

func TestRunConsole_FakeSuccess(t *testing.T) {
	orig := execLookPath
	execLookPath = func(name string) (string, error) { return "/fake/yaegi", nil }
	t.Cleanup(func() { execLookPath = orig })
	withFakeExec(t, 0)

	assert.NoError(t, runConsole())
}

// --- serve / seed / swagger command RunE ---

func TestServeCmd_RunE_FakeSuccess(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, serveCmd.RunE(serveCmd, nil))
}

func TestSeedCmd_RunE_FakeSuccess(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, seedCmd.RunE(seedCmd, nil))
}

func TestSeedCmd_RunE_Fresh(t *testing.T) {
	withFakeExec(t, 0)
	seedCmd.Flags().Set("fresh", "true")
	t.Cleanup(func() { seedCmd.Flags().Set("fresh", "false") })
	assert.NoError(t, seedCmd.RunE(seedCmd, nil))
}

func TestSwaggerCmd_RunE_FakeSuccess(t *testing.T) {
	withFakeExec(t, 0)
	assert.NoError(t, swaggerCmd.RunE(swaggerCmd, nil))
}

// --- runDoctor ---

func TestRunDoctor_AllSuccess(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	err := runDoctor()
	// no config.yaml present, so only required + optional checks run; all succeed
	assert.NoError(t, err)
}

func TestRunDoctor_AllFail(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 1)
	err := runDoctor()
	assert.Error(t, err)
}

func TestRunDoctor_WithConfigYaml(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	err := runDoctor()
	assert.NoError(t, err)
}

func TestRunDoctor_WithConfigYaml_MigrateFails(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 1)
	err := runDoctor()
	assert.Error(t, err)
}

// --- individual check helpers ---

func TestCheckGoVersion_FakeSuccess(t *testing.T) {
	withFakeExec(t, 0)
	_, ok := checkGoVersion()
	assert.True(t, ok)
}

func TestCheckGoVersion_FakeFail(t *testing.T) {
	withFakeExec(t, 1)
	_, ok := checkGoVersion()
	assert.False(t, ok)
}

func TestCheckMigrateVersion_FakeSuccess(t *testing.T) {
	withFakeExec(t, 0)
	_, ok := checkMigrateVersion()
	assert.True(t, ok)
}

func TestCheckMigrateVersion_FakeFail(t *testing.T) {
	withFakeExec(t, 1)
	_, ok := checkMigrateVersion()
	assert.False(t, ok)
}

func TestCheckDockerVersion_FakeSuccess(t *testing.T) {
	withFakeExec(t, 0)
	_, ok := checkDockerVersion()
	assert.True(t, ok)
}

func TestCheckDockerVersion_FakeFail(t *testing.T) {
	withFakeExec(t, 1)
	_, ok := checkDockerVersion()
	assert.False(t, ok)
}

func TestCheckGoTool_FakeSuccess(t *testing.T) {
	withFakeExec(t, 0)
	fn := checkGoTool("air")
	_, ok := fn()
	assert.True(t, ok)
}

func TestCheckGoTool_FakeFail(t *testing.T) {
	withFakeExec(t, 1)
	fn := checkGoTool("air")
	msg, ok := fn()
	assert.False(t, ok)
	assert.Contains(t, msg, "air-verse/air")
}

// --- printCheck ---

func TestPrintCheck(t *testing.T) {
	// Just smoke — it writes to stdout
	assert.NotPanics(t, func() {
		printCheck("foo", "bar", true)
		printCheck("foo", "bar", false)
	})
}

// --- printBanner ---

func TestPrintBanner(t *testing.T) {
	assert.NotPanics(t, printBanner)
}

// --- runExecute (root.go) ---

func TestRunExecute_Help(t *testing.T) {
	// With no subcommand, cobra runs the root Run func which calls printBanner + Help
	rootCmd.SetArgs([]string{"version"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	assert.NoError(t, runExecute("0.0.0-test"))
}

func TestRunExecute_UnknownSubcommand(t *testing.T) {
	rootCmd.SetArgs([]string{"definitely-not-a-cmd"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	assert.Error(t, runExecute("0.0.0-test"))
}

// --- runNew ---

func TestRunNew_FakeSuccess(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	err := runNew("testapp", false)
	assert.NoError(t, err)
	// The project dir should have been created
	_, err = os.Stat(filepath.Join("testapp", "config.yaml"))
	// config.yaml is one of the skeleton files; it should exist after a successful run
	assert.NoError(t, err)
}

func TestRunNew_FakeSuccess_GraphQL(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	err := runNew("gqlapp", true)
	assert.NoError(t, err)
}

func TestRunNew_GoModInitFails(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 1)
	err := runNew("failapp", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "go mod init")
}

// Staged: go mod init AND go get gofasta both succeed, everything after
// fails. The gofasta install is a hard-fail step (see runNew) because the
// scaffold is unusable without it, so to exercise the post-gofasta warning
// branches we need the first two exec calls to succeed.
func TestRunNew_WarningBranches(t *testing.T) {
	chdirTemp(t)
	stagedFakeExec(t, 0, 0, 1) // mod init ok, gofasta install ok, everything else fails
	err := runNew("warnapp", false)
	assert.NoError(t, err)
}

func TestRunNew_WarningBranches_GraphQL(t *testing.T) {
	chdirTemp(t)
	stagedFakeExec(t, 0, 0, 1)
	err := runNew("warnapp", true)
	assert.NoError(t, err)
}

// When `go get github.com/gofastadev/gofasta` fails (e.g. sum.golang.org
// has not yet indexed a freshly-published release), runNew must abort with
// a clear error instead of silently producing a broken scaffold.
func TestRunNew_GofastaInstallFails(t *testing.T) {
	chdirTemp(t)
	stagedFakeExec(t, 0, 1) // go mod init ok, go get gofasta fails
	err := runNew("failapp", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "github.com/gofastadev/gofasta")
	assert.Contains(t, err.Error(), "sum.golang.org", "error should mention sum DB lag as a likely cause")
}

// --- runRoutes ---

func TestRunRoutes_SampleProject(t *testing.T) {
	chdirTemp(t)
	routesDir := "app/rest/routes"
	require.NoError(t, os.MkdirAll(routesDir, 0755))

	index := `package routes
func InitApi(r *mux.Router) {
	r.PathPrefix("/api/v1")
	r.HandleFunc("/health", httputil.Handle(c.Ok)).Methods("GET")
}`
	require.NoError(t, os.WriteFile(routesDir+"/index.routes.go", []byte(index), 0644))

	user := `package routes
func UserRoutes(r *mux.Router) {
	r.HandleFunc("/users", httputil.Handle(c.List)).Methods("GET")
	r.HandleFunc("/users/{id}", httputil.Handle(c.Get)).Methods("GET")
}`
	require.NoError(t, os.WriteFile(routesDir+"/user.routes.go", []byte(user), 0644))

	assert.NoError(t, runRoutes())
}

func TestRunRoutes_Empty(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, os.MkdirAll("app/rest/routes", 0755))
	assert.NoError(t, runRoutes())
}

func TestRunRoutes_NoIndexFile(t *testing.T) {
	chdirTemp(t)
	routesDir := "app/rest/routes"
	require.NoError(t, os.MkdirAll(routesDir, 0755))
	// Only a non-index file — apiPrefix will stay empty
	user := `r.HandleFunc("/a", x).Methods("GET")`
	require.NoError(t, os.WriteFile(routesDir+"/a.routes.go", []byte(user), 0644))
	assert.NoError(t, runRoutes())
}

func TestRunRoutes_SkipsNonRouteFiles(t *testing.T) {
	chdirTemp(t)
	routesDir := "app/rest/routes"
	require.NoError(t, os.MkdirAll(routesDir, 0755))
	// A subdirectory + a non .routes.go file + an unreadable name
	require.NoError(t, os.MkdirAll(routesDir+"/subdir", 0755))
	require.NoError(t, os.WriteFile(routesDir+"/notaroute.go", []byte("package routes"), 0644))
	assert.NoError(t, runRoutes())
}
