package skeleton

import (
	"io/fs"
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectFS_NotEmpty(t *testing.T) {
	count := 0
	err := fs.WalkDir(ProjectFS, "project", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	require.NoError(t, err)
	assert.Greater(t, count, 0, "ProjectFS should contain files")
}

func TestProjectFS_ContainsExpectedFiles(t *testing.T) {
	expectedFiles := []string{
		"project/config.yaml.tmpl",
		"project/cmd/serve.go.tmpl",
		"project/cmd/root.go.tmpl",
		"project/Makefile.tmpl",
		"project/Dockerfile",
		"project/dot-gitignore",
		"project/dot-go-version",
	}

	files := make(map[string]bool)
	fs.WalkDir(ProjectFS, "project", func(path string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			files[path] = true
		}
		return nil
	})

	for _, expected := range expectedFiles {
		assert.True(t, files[expected], "expected file %s not found in ProjectFS", expected)
	}
}

func TestProjectFS_TemplatesAreParseable(t *testing.T) {
	err := fs.WalkDir(ProjectFS, "project", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".tmpl") {
			return nil
		}

		data, readErr := fs.ReadFile(ProjectFS, path)
		require.NoError(t, readErr, "failed to read %s", path)

		_, parseErr := template.New(path).Parse(string(data))
		assert.NoError(t, parseErr, "template parse error in %s", path)
		return nil
	})
	require.NoError(t, err)
}
