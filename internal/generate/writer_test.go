package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteTemplate_CreatesFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	path := "output/test.go"
	tmpl := "package {{.SnakeName}}\n\ntype {{.Name}} struct{}\n"

	err := WriteTemplate(path, "test", tmpl, d)
	require.NoError(t, err)

	content := readTestFile(t, path)
	assert.Contains(t, content, "package product")
	assert.Contains(t, content, "type Product struct{}")
}

func TestWriteTemplate_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	path := "output/test.go"

	writeTestFile(t, path, "original content")

	err := WriteTemplate(path, "test", "new content {{.Name}}", d)
	require.NoError(t, err)

	content := readTestFile(t, path)
	assert.Equal(t, "original content", content)
}

func TestWriteTemplate_CreatesParentDirs(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	path := "a/b/c/deep.go"
	tmpl := "package deep"

	err := WriteTemplate(path, "test", tmpl, d)
	require.NoError(t, err)

	content := readTestFile(t, path)
	assert.Equal(t, "package deep", content)
}

func TestWriteTemplate_InvalidTemplate(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	path := "output/bad.go"

	err := WriteTemplate(path, "bad", "{{.InvalidSyntax", d)
	assert.Error(t, err)

	// File should not exist
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestWriteTemplate_FuncMap(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	path := "output/funcmap.go"
	tmpl := "braces: {{lbrace}} {{rbrace}}"

	err := WriteTemplate(path, "test", tmpl, d)
	require.NoError(t, err)

	content := readTestFile(t, path)
	assert.Contains(t, content, "braces: { }")
}

func TestWriteTemplate_TemplateExecutionError(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	path := "output/exec_error.go"
	// Template references a method that doesn't exist on ScaffoldData
	tmpl := "{{.NonExistentMethod}}"

	err := WriteTemplate(path, "test", tmpl, d)
	assert.Error(t, err)
}

func TestWriteTemplate_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	d := sampleScaffoldData()
	path := filepath.Join(dir, "sub", "file.go")
	tmpl := "package sub"

	err := WriteTemplate(path, "test", tmpl, d)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "package sub", string(data))
}
