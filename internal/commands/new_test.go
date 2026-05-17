package commands

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveProjectPaths_SimpleName(t *testing.T) {
	dir, name, mod := resolveProjectPaths("myapp")
	assert.Equal(t, "myapp", dir)
	assert.Equal(t, "myapp", name)
	assert.Equal(t, "myapp", mod)
}

func TestResolveProjectPaths_ModulePath(t *testing.T) {
	dir, name, mod := resolveProjectPaths("github.com/org/myapp")
	assert.Equal(t, "myapp", dir)
	assert.Equal(t, "myapp", name)
	assert.Equal(t, "github.com/org/myapp", mod)
}

func TestResolveProjectPaths_AbsolutePath(t *testing.T) {
	dir, name, mod := resolveProjectPaths("/tmp/myapp")
	assert.Equal(t, "/tmp/myapp", dir)
	assert.Equal(t, "myapp", name)
	assert.Equal(t, "myapp", mod)
}

func TestResolveProjectPaths_NestedAbsolutePath(t *testing.T) {
	dir, name, mod := resolveProjectPaths("/home/user/projects/myapp")
	assert.Equal(t, "/home/user/projects/myapp", dir)
	assert.Equal(t, "myapp", name)
	assert.Equal(t, "myapp", mod)
}

func TestResolveProjectPaths_DeepModulePath(t *testing.T) {
	dir, name, mod := resolveProjectPaths("github.com/gofastadev/cli")
	assert.Equal(t, "cli", dir)
	assert.Equal(t, "cli", name)
	assert.Equal(t, "github.com/gofastadev/cli", mod)
}

func TestDotfileRenames(t *testing.T) {
	expected := map[string]string{
		"dot-env.example": ".env.example",
		"dot-env":         ".env",
		"dot-gitignore":   ".gitignore",
		"dot-go-version":  ".go-version",
		"air.toml":        ".air.toml",
	}
	assert.Equal(t, expected, dotfileRenames)
}

func TestGraphqlOnlyPaths(t *testing.T) {
	assert.NotEmpty(t, graphqlOnlyPaths)
	// Should contain GraphQL-related paths
	found := false
	for _, p := range graphqlOnlyPaths {
		if p == "app/graphql/" {
			found = true
			break
		}
	}
	assert.True(t, found, "graphqlOnlyPaths should contain 'app/graphql/'")
}

func TestRunNew_DirectoryAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	os.Mkdir("myapp", 0755)
	err := runNew("myapp", false, "postgres")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestNewCmd_HasFlags(t *testing.T) {
	f := newCmd.Flags().Lookup("graphql")
	assert.NotNil(t, f, "newCmd should have --graphql flag")
	f2 := newCmd.Flags().Lookup("gql")
	assert.NotNil(t, f2, "newCmd should have --gql flag")
}

func TestNewCmd_RequiresArgs(t *testing.T) {
	rootCmd.SetArgs([]string{"new"})
	err := rootCmd.Execute()
	assert.Error(t, err)
	rootCmd.SetArgs(nil)
}

// envVarSafeUpper strips illegal shell-var characters from a project name
// so dashes, dots, and other punctuation don't produce invalid env var
// prefixes like MY-APP_DATABASE_HOST. Covers the "default" branch of the
// character-class switch which maps illegal runes to -1 (dropped).
func TestEnvVarSafeUpper(t *testing.T) {
	cases := map[string]string{
		"myapp":      "MYAPP",
		"MYAPP":      "MYAPP",
		"my-app":     "MYAPP",
		"my.app":     "MYAPP",
		"my app":     "MYAPP",
		"my-app.v2":  "MYAPPV2",
		"my_app":     "MY_APP", // underscore IS legal in env var names
		"acme_co-v3": "ACME_COV3",
		"":           "",
		"123app":     "123APP",
		"---":        "",
		"a@b#c$":     "ABC",
	}
	for in, want := range cases {
		got := envVarSafeUpper(in)
		if got != want {
			t.Errorf("envVarSafeUpper(%q) = %q, want %q", in, got, want)
		}
	}
}

// runNew MkdirAll error — the target project directory's parent is a
// regular file, not a dir. os.MkdirAll fails with ENOTDIR. Uses an
// absolute path so resolveProjectPaths preserves the full path (a
// relative path would be collapsed to just the base segment).
func TestRunNew_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	// Make "parent" a regular file so MkdirAll("<dir>/parent/proj") fails
	// because one of its path components isn't a directory.
	parentFile := filepath.Join(dir, "parent")
	require.NoError(t, os.WriteFile(parentFile, []byte("x"), 0o644))

	err := runNew(filepath.Join(parentFile, "proj"), false, "postgres")
	assert.Error(t, err)
}

// runNew Chdir error — create a parent directory with no-execute
// permission so MkdirAll succeeds for the parent (it already exists) but
// Chdir into a child path inside it fails with EACCES. The target child
// path doesn't exist yet (so runNew's "directory already exists" check
// passes), and MkdirAll creates it with mode 0o755 — but then the
// subsequent Chdir needs execute permission on the parent to enter the
// child, which it doesn't have.
func TestRunNew_ChdirError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod-based access denial")
	}
	parent := t.TempDir()
	// Drop execute permission on the parent.
	require.NoError(t, os.Chmod(parent, 0o600))
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	err := runNew(filepath.Join(parent, "proj"), false, "postgres")
	assert.Error(t, err)
}

// chdirTemp is a lightweight helper that pins the test to a fresh temp
// dir and restores the original cwd on cleanup. It's already defined in
// commands_exec_test.go but re-declaring it in this file is a compile
// error — tests that need it rely on the one in commands_exec_test.go.
// No function here — this comment exists so future readers don't
// accidentally add a duplicate.

func TestProjectData_Fields(t *testing.T) {
	data := ProjectData{
		ProjectName:      "MyApp",
		ProjectNameLower: "myapp",
		ProjectNameUpper: "MYAPP",
		ModulePath:       "github.com/org/myapp",
		GraphQL:          true,
	}
	assert.Equal(t, "MyApp", data.ProjectName)
	assert.Equal(t, "myapp", data.ProjectNameLower)
	assert.Equal(t, "MYAPP", data.ProjectNameUpper)
	assert.Equal(t, "github.com/org/myapp", data.ModulePath)
	assert.True(t, data.GraphQL)
}

// ─────────────────────────────────────────────────────────────────────
// Coverage for new.go walk-error / template-error branches. Uses the
// projectFSOverride seam to inject synthetic filesystems that trigger
// specific failure modes.
// ─────────────────────────────────────────────────────────────────────

// TestRunNew_ChdirFails — projectDir is created but Chdir fails via
// the osChdir seam.
func TestRunNew_ChdirFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	chdirTemp(t)
	origOS := osChdir
	osChdir = func(path string) error { return os.ErrPermission }
	t.Cleanup(func() { osChdir = origOS })
	withFakeExec(t, 0)
	err := runNew("chdir-fail-app", false, "postgres")
	require.Error(t, err)
}

// TestRunNew_BadTemplate — inject a synthetic FS containing a .tmpl
// file whose body is malformed → template.Parse fails.
func TestRunNew_BadTemplate(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	// Build a minimal FS that WalkDir can traverse. The walk expects
	// "project" at the root.
	fsys := fstest.MapFS{
		"project":             {Mode: fs.ModeDir},
		"project/broken.tmpl": {Data: []byte("{{.MissingClose")},
	}
	projectFSOverride = fsys
	t.Cleanup(func() { projectFSOverride = nil })
	err := runNew("bad-tmpl-app", false, "postgres")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing template")
}

// TestRunNew_TemplateExecFails — template parses but Execute fails
// because the template references a missing field.
func TestRunNew_TemplateExecFails(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	fsys := fstest.MapFS{
		"project":             {Mode: fs.ModeDir},
		"project/bad.go.tmpl": {Data: []byte("{{.NoSuchField.Sub}}")},
	}
	projectFSOverride = fsys
	t.Cleanup(func() { projectFSOverride = nil })
	err := runNew("bad-exec-app", false, "postgres")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "executing template")
}

// errFS is a small fs.FS implementation that returns an error on
// ReadFile for a specific path but lets WalkDir pass.
type errFS struct{ base fs.FS }

func (e errFS) Open(name string) (fs.File, error)    { return e.base.Open(name) }
func (e errFS) ReadFile(name string) ([]byte, error) { return nil, fs.ErrPermission }
func (e errFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if rd, ok := e.base.(fs.ReadDirFS); ok {
		return rd.ReadDir(name)
	}
	return nil, fs.ErrInvalid
}

// TestRunNew_ReadFileFails — fs.ReadFile returns an error for a
// specific file during the walk.
func TestRunNew_ReadFileFails(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	base := fstest.MapFS{
		"project":       {Mode: fs.ModeDir},
		"project/a.txt": {Data: []byte("x")},
	}
	projectFSOverride = errFS{base: base}
	t.Cleanup(func() { projectFSOverride = nil })
	err := runNew("read-fail-app", false, "postgres")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading")
}

// TestRunNew_WalkCallbackReceivesError — root "project" doesn't exist
// in our override FS → WalkDir's callback is invoked with an err
// describing the missing path, covering the err-param branch.
func TestRunNew_WalkCallbackReceivesError(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	// Empty FS — "project" does not exist → WalkDir invokes callback
	// with an fs.PathError → the first branch in the callback fires.
	projectFSOverride = fstest.MapFS{}
	t.Cleanup(func() { projectFSOverride = nil })
	err := runNew("walkerr-app", false, "postgres")
	require.Error(t, err)
}

// TestRunNew_UnreadableDir — projectDir collides with an existing
// regular file named "conflict".
func TestRunNew_UnreadableDir(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	require.NoError(t, os.WriteFile("conflict", []byte{}, 0o644))
	err := runNew("conflict", false, "postgres")
	require.Error(t, err)
}

// TestRunNew_JSON_EmitsResultOnEarlyReturn — JSON mode emits a single
// newResult document even on the early-return path (directory already
// exists). Verifies that the deferred restore-stdout + Print runs
// regardless of where runNew exits.
func TestRunNew_JSON_EmitsResultOnEarlyReturn(t *testing.T) {
	chdirTemp(t)
	withJSONMode(t)

	projectFSOverride = fstest.MapFS{
		"project":       {Mode: fs.ModeDir},
		"project/a.txt": {Data: []byte("x")},
	}
	t.Cleanup(func() { projectFSOverride = nil })

	// Pre-create the target dir so runNew bails at the existence check.
	require.NoError(t, os.MkdirAll("collision-app", 0o755))

	out := captureStdout(t, func() {
		err := runNew("collision-app", false, "postgres")
		require.Error(t, err)
	})

	var got newResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.Equal(t, "new", got.Action)
	assert.Equal(t, "collision-app", got.Project)
	assert.False(t, got.Success)
	assert.Contains(t, got.Error, "already exists")
}

// TestNewCmd_DriverFlag_AcceptsAllSupported — every supported driver
// passes validation; the flag is wired through to runNew via the
// cobra RunE. Smoke-runs the cmd against an --invalid value to lock
// the rejection error code.
func TestNewCmd_DriverFlag_AcceptsAllSupported(t *testing.T) {
	for _, d := range supportedDrivers {
		assert.True(t, isSupportedDriver(d), "driver %q should be supported", d)
	}
	assert.False(t, isSupportedDriver("oracle"))
	assert.False(t, isSupportedDriver(""))
}

// TestNewCmd_DriverFlag_RegisteredOnCmd — the cobra flag exists and
// defaults to postgres. Regression: a typo'd flag name would silently
// fall back to postgres for every project, killing the multi-driver
// scaffold.
func TestNewCmd_DriverFlag_RegisteredOnCmd(t *testing.T) {
	f := newCmd.Flags().Lookup("driver")
	assert.NotNil(t, f, "newCmd should have a --driver flag")
	assert.Equal(t, "postgres", f.DefValue)
}

// TestRunNew_PerDriverMigrationsCopied — each supported driver's
// foundational migrations land in db/migrations/. Postgres gets 5 up
// + 5 down (citext + 3 functions + users); the other drivers get 1 up
// + 1 down (users only, with inlined triggers).
func TestRunNew_PerDriverMigrationsCopied(t *testing.T) {
	cases := map[string]struct{ wantUp, wantDown int }{
		"postgres":   {5, 5},
		"mysql":      {1, 1},
		"sqlite":     {1, 1},
		"sqlserver":  {1, 1},
		"clickhouse": {1, 1},
	}
	for driver, want := range cases {
		t.Run(driver, func(t *testing.T) {
			parent := t.TempDir()
			orig, _ := os.Getwd()
			t.Cleanup(func() { _ = os.Chdir(orig) })
			require.NoError(t, os.Chdir(parent))

			// Use a synthetic minimal projectFS to skip the heavy go-get
			// and wire generation in runNew — we're only testing the
			// migrations-copy step.
			projectFSOverride = synthMinimalProjectFS(t)
			t.Cleanup(func() { projectFSOverride = nil })

			projectName := driver + "app"
			_ = captureStdout(t, func() {
				err := runNew(projectName, false, driver)
				// runNew may fail later (no real go mod tidy possible
				// against the synthetic FS), but the migrations copy
				// happens BEFORE any of that. Tolerate the trailing
				// error.
				_ = err
			})

			ups, downs := 0, 0
			migDir := filepath.Join(parent, projectName, "db", "migrations")
			entries, err := os.ReadDir(migDir)
			require.NoError(t, err, "db/migrations should exist for driver %s", driver)
			for _, e := range entries {
				switch {
				case filepath.Ext(e.Name()) == ".sql" && len(e.Name()) >= 7 &&
					e.Name()[len(e.Name())-7:] == ".up.sql":
					ups++
				case filepath.Ext(e.Name()) == ".sql" && len(e.Name()) >= 9 &&
					e.Name()[len(e.Name())-9:] == ".down.sql":
					downs++
				}
			}
			assert.Equal(t, want.wantUp, ups, "up.sql count for %s", driver)
			assert.Equal(t, want.wantDown, downs, "down.sql count for %s", driver)
		})
	}
}

// synthMinimalProjectFS returns a fake project FS that contains just
// enough to keep runNew happy through the early walk + .env step. The
// migrations-copy step runs AFTER the walk and pulls from the real
// embedded MigrationsFS regardless of this override.
func synthMinimalProjectFS(t *testing.T) fs.FS {
	t.Helper()
	return fstest.MapFS{
		"project":             {Mode: fs.ModeDir},
		"project/go.mod.tmpl": {Data: []byte("module {{.ModulePath}}\n\ngo 1.25.0\n")},
	}
}

// TestNewCmd_DriverFlag_RejectsUnknown — the cobra RunE wrapper
// validates --driver before reaching runNew. Covers the
// `if !isSupportedDriver(driver) { return clierr.Newf(...) }` body
// that the direct runNew tests bypass.
func TestNewCmd_DriverFlag_RejectsUnknown(t *testing.T) {
	chdirTemp(t)
	require.NoError(t, newCmd.Flags().Set("driver", "oracle"))
	t.Cleanup(func() { _ = newCmd.Flags().Set("driver", "postgres") })
	err := newCmd.RunE(newCmd, []string{"badapp"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
	assert.Contains(t, err.Error(), "oracle")
}

// TestRunNew_DriverEmptyDefaultsToPostgres — defensive: a caller that
// passes driver="" gets postgres semantics, not a crash or a lookup
// failure later. Covers the `if driver == "" { driver = "postgres" }`
// branch in runNew.
func TestRunNew_DriverEmptyDefaultsToPostgres(t *testing.T) {
	chdirTemp(t)
	projectFSOverride = synthMinimalProjectFS(t)
	t.Cleanup(func() { projectFSOverride = nil })
	withFakeExec(t, 0)

	_ = captureStdout(t, func() {
		// Best-effort: runNew may fail later because the synthetic FS
		// doesn't carry a full project tree, but the empty-driver
		// branch executes BEFORE any of that.
		_ = runNew("emptydrivertest", false, "")
	})

	// db/migrations should contain the postgres set (5 up + 5 down)
	// because the empty driver defaulted to "postgres".
	entries, err := os.ReadDir(filepath.Join("emptydrivertest", "db", "migrations"))
	require.NoError(t, err)
	assert.Len(t, entries, 10, "postgres has 5 up + 5 down foundational migrations")
}

// TestRunNew_CopyMigrationsErrorPropagates — runNew wraps any
// copyMigrationsForDriver failure with the "copying %s foundational
// migrations" prefix. Forced via the migrationsFSOverride seam:
// inject an empty FS so the per-driver subdirectory doesn't exist
// and ReadDir errors.
func TestRunNew_CopyMigrationsErrorPropagates(t *testing.T) {
	chdirTemp(t)
	projectFSOverride = synthMinimalProjectFS(t)
	migrationsFSOverride = fstest.MapFS{} // no migrations/ at all
	t.Cleanup(func() {
		projectFSOverride = nil
		migrationsFSOverride = nil
	})
	withFakeExec(t, 0)

	_ = captureStdout(t, func() {
		err := runNew("copyfailapp", false, "postgres")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "copying postgres foundational migrations")
	})
}

// TestCopyMigrationsForDriver_UnknownDriver — ReadDir returns
// fs.ErrNotExist for an unsupported driver name. The function must
// surface that as a "no foundational migrations for driver" error.
func TestCopyMigrationsForDriver_UnknownDriver(t *testing.T) {
	chdirTemp(t)
	err := copyMigrationsForDriver("nosuchdriver", ProjectData{
		ProjectName:      "X",
		ProjectNameLower: "x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no foundational migrations for driver")
	assert.Contains(t, err.Error(), "nosuchdriver")
}

// TestCopyMigrationsForDriver_SkipsDirectoryEntries — a directory
// entry inside migrations/<driver>/ must be skipped (the loop's
// `if e.IsDir() { continue }` branch). Inject a synthetic FS that
// contains a subdirectory alongside the .sql files.
func TestCopyMigrationsForDriver_SkipsDirectoryEntries(t *testing.T) {
	chdirTemp(t)
	migrationsFSOverride = fstest.MapFS{
		"migrations":                                {Mode: fs.ModeDir},
		"migrations/postgres":                       {Mode: fs.ModeDir},
		"migrations/postgres/subdir":                {Mode: fs.ModeDir}, // should be skipped
		"migrations/postgres/000001_users.up.sql":   {Data: []byte("-- {{.ProjectNameLower}}\n")},
		"migrations/postgres/000001_users.down.sql": {Data: []byte("DROP TABLE users;\n")},
	}
	t.Cleanup(func() { migrationsFSOverride = nil })

	_ = captureStdout(t, func() {
		require.NoError(t, copyMigrationsForDriver("postgres", ProjectData{
			ProjectName:      "MyApp",
			ProjectNameLower: "myapp",
		}))
	})

	// Two files written, no directory propagated.
	got, err := os.ReadDir("db/migrations")
	require.NoError(t, err)
	assert.Len(t, got, 2)
	for _, e := range got {
		assert.False(t, e.IsDir(), "should not have copied a directory")
	}
}

// TestCopyMigrationsForDriver_BadTemplate — malformed template
// content surfaces a "parsing migration" error.
func TestCopyMigrationsForDriver_BadTemplate(t *testing.T) {
	chdirTemp(t)
	migrationsFSOverride = fstest.MapFS{
		"migrations":          {Mode: fs.ModeDir},
		"migrations/postgres": {Mode: fs.ModeDir},
		"migrations/postgres/000001_broken.up.sql": {Data: []byte("{{.UnclosedAction")},
	}
	t.Cleanup(func() { migrationsFSOverride = nil })

	err := copyMigrationsForDriver("postgres", ProjectData{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing migration")
}

// TestCopyMigrationsForDriver_TemplateExecuteFails — template parses
// but Execute fails because the body references a missing field.
func TestCopyMigrationsForDriver_TemplateExecuteFails(t *testing.T) {
	chdirTemp(t)
	migrationsFSOverride = fstest.MapFS{
		"migrations":          {Mode: fs.ModeDir},
		"migrations/postgres": {Mode: fs.ModeDir},
		"migrations/postgres/000001_bad_exec.up.sql": {Data: []byte("{{.DoesNotExist.Nested}}")},
	}
	t.Cleanup(func() { migrationsFSOverride = nil })

	err := copyMigrationsForDriver("postgres", ProjectData{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "executing migration template")
}

// TestCopyMigrationsForDriver_MkdirAllError — if MkdirAll fails on
// db/migrations the error surfaces unwrapped (defensive branch).
// Force the failure by making `db` a regular file in the cwd before
// calling — MkdirAll then can't create db/migrations under it.
func TestCopyMigrationsForDriver_MkdirAllError(t *testing.T) {
	chdirTemp(t)
	// Create a regular file at the "db" path. MkdirAll("db/migrations")
	// will fail with ENOTDIR because "db" is not a directory.
	require.NoError(t, os.WriteFile("db", []byte("x"), 0o644))

	err := copyMigrationsForDriver("postgres", ProjectData{
		ProjectName:      "X",
		ProjectNameLower: "x",
	})
	require.Error(t, err)
}

// TestCopyMigrationsForDriver_WriteFileError — chmod the
// db/migrations dir read-only so os.WriteFile inside fails with
// EACCES. Triggers the "writing %s" defensive branch.
func TestCopyMigrationsForDriver_WriteFileError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod denial")
	}
	chdirTemp(t)
	require.NoError(t, os.MkdirAll("db/migrations", 0o755))
	require.NoError(t, os.Chmod("db/migrations", 0o555))
	t.Cleanup(func() { _ = os.Chmod("db/migrations", 0o755) })

	migrationsFSOverride = fstest.MapFS{
		"migrations":          {Mode: fs.ModeDir},
		"migrations/postgres": {Mode: fs.ModeDir},
		"migrations/postgres/000001_users.up.sql": {Data: []byte("CREATE TABLE u();\n")},
	}
	t.Cleanup(func() { migrationsFSOverride = nil })

	err := copyMigrationsForDriver("postgres", ProjectData{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing")
}

// errReadFS wraps an fs.FS and returns an error on ReadFile for a
// specific path. Used by TestCopyMigrationsForDriver_ReadFileError to
// hit the defensive "reading %s" branch which a normal embed.FS
// cannot trigger.
type errReadFS struct {
	base       fs.FS
	failOnPath string
}

func (e errReadFS) Open(name string) (fs.File, error) { return e.base.Open(name) }
func (e errReadFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if rd, ok := e.base.(fs.ReadDirFS); ok {
		return rd.ReadDir(name)
	}
	return fs.ReadDir(e.base, name)
}
func (e errReadFS) ReadFile(name string) ([]byte, error) {
	if name == e.failOnPath {
		return nil, fs.ErrPermission
	}
	return fs.ReadFile(e.base, name)
}

// TestCopyMigrationsForDriver_ReadFileError — inject a synthetic FS
// whose ReadFile returns an error for a specific file. Covers the
// `if err != nil { return fmt.Errorf("reading %s: %w") }` branch.
func TestCopyMigrationsForDriver_ReadFileError(t *testing.T) {
	chdirTemp(t)
	base := fstest.MapFS{
		"migrations":          {Mode: fs.ModeDir},
		"migrations/postgres": {Mode: fs.ModeDir},
		"migrations/postgres/000001_users.up.sql": {Data: []byte("CREATE TABLE u();\n")},
	}
	migrationsFSOverride = errReadFS{base: base, failOnPath: "migrations/postgres/000001_users.up.sql"}
	t.Cleanup(func() { migrationsFSOverride = nil })

	err := copyMigrationsForDriver("postgres", ProjectData{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading")
}
