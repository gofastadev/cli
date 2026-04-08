package commands

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
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
