package commands

import (
	"os"
	"path/filepath"
	"testing"

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
	err := runNew("myapp", false)
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

	err := runNew(filepath.Join(parentFile, "proj"), false)
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

	err := runNew(filepath.Join(parent, "proj"), false)
	assert.Error(t, err)
}

// installGofastaFromLocal needs an absolute-or-relative path that exists
// and contains a go.mod. Every error branch (missing path, file instead of
// dir, dir without go.mod) must produce a clear error so mis-set
// GOFASTA_REPLACE values are caught at scaffold time instead of failing
// deep inside `go mod tidy`.

func TestInstallGofastaFromLocal_PathDoesNotExist(t *testing.T) {
	chdirTemp(t)
	setupGoMod(t)
	err := installGofastaFromLocal("/nonexistent/path/xyz")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stat")
}

func TestInstallGofastaFromLocal_PathIsFile(t *testing.T) {
	chdirTemp(t)
	setupGoMod(t)
	fakeFile := filepath.Join(t.TempDir(), "notadir")
	require.NoError(t, os.WriteFile(fakeFile, []byte("x"), 0o644))
	err := installGofastaFromLocal(fakeFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestInstallGofastaFromLocal_DirWithoutGoMod(t *testing.T) {
	chdirTemp(t)
	setupGoMod(t)
	dir := t.TempDir()
	err := installGofastaFromLocal(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "go.mod")
}

func TestInstallGofastaFromLocal_HappyPath(t *testing.T) {
	chdirTemp(t)
	setupGoMod(t)
	// Create a fake "gofasta checkout" — a directory with a go.mod inside.
	fakeFramework := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fakeFramework, "go.mod"),
		[]byte("module github.com/gofastadev/gofasta\n\ngo 1.25.8\n"), 0o644))
	// Mock execCommand so the `go mod edit` calls "succeed" without
	// actually hitting the real go binary.
	withFakeExec(t, 0)

	err := installGofastaFromLocal(fakeFramework)
	assert.NoError(t, err)
}

func TestInstallGofastaFromLocal_EditRequireFails(t *testing.T) {
	chdirTemp(t)
	setupGoMod(t)
	fakeFramework := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fakeFramework, "go.mod"),
		[]byte("module github.com/gofastadev/gofasta\n\ngo 1.25.8\n"), 0o644))
	withFakeExec(t, 1) // every exec fails

	err := installGofastaFromLocal(fakeFramework)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "go mod edit")
}

// Covers the case where the initial `go mod edit -require` succeeds but
// the subsequent `-replace` call fails. Staged fake exec returns 0 then 1.
func TestInstallGofastaFromLocal_EditReplaceFails(t *testing.T) {
	chdirTemp(t)
	setupGoMod(t)
	fakeFramework := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fakeFramework, "go.mod"),
		[]byte("module github.com/gofastadev/gofasta\n\ngo 1.25.8\n"), 0o644))
	stagedFakeExec(t, 0, 1) // require ok, replace fails

	err := installGofastaFromLocal(fakeFramework)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "-replace")
}

// Ensures runNew surfaces a clear error when GOFASTA_REPLACE points at a
// bogus path. The scaffold should abort at the install step rather than
// producing a broken project.
func TestRunNew_GofastaReplaceBadPath(t *testing.T) {
	chdirTemp(t)
	t.Setenv("GOFASTA_REPLACE", "/definitely/not/a/real/gofasta/checkout")
	withFakeExec(t, 0) // go mod init still succeeds
	err := runNew("badreplace", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "link gofasta from")
}

// setupGoMod writes a minimal go.mod in the current directory so
// subsequent `go mod edit` calls have something to operate on. Used by
// the installGofastaFromLocal tests which need a project-like working
// directory to run against.
func setupGoMod(t *testing.T) {
	t.Helper()
	require.NoError(t, os.WriteFile("go.mod",
		[]byte("module testproject\n\ngo 1.25.8\n"), 0o644))
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
