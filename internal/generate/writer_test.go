package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
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
	// `.go` outputs are formatted through go/format.Source, which
	// guarantees the trailing-newline convention `gofmt -s -l` enforces.
	assert.Equal(t, "package deep\n", content)
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

// TestWriteTemplate_RejectsAbsolutePath — the planner's
// ensureWithinProject guard refuses to write an output file to an
// absolute path. Defense-in-depth against a name-derived path that
// escapes the project tree; every production generator writes to a
// relative path under the project root.
func TestWriteTemplate_RejectsAbsolutePath(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	path := filepath.Join(t.TempDir(), "sub", "file.go")

	err := WriteTemplate(path, "test", "package sub", d)
	require.Error(t, err)
	var ce *clierr.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, string(clierr.CodeInvalidName), ce.Code)
}

// TestWriteTemplate_RejectsTraversalPath — a relative output path that
// climbs out of the project root with `..` is rejected by the same
// guard.
func TestWriteTemplate_RejectsTraversalPath(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := WriteTemplate("../escape/file.go", "test", "package escape", d)
	require.Error(t, err)
	var ce *clierr.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, string(clierr.CodeInvalidName), ce.Code)
}

// TestWriteTemplate_UsesTimestamp — a template that calls
// {{timestamp}} resolves and writes the file. Previously uncovered
// the `timestamp` function inside the FuncMap because no shipped
// template references it.
func TestWriteTemplate_UsesTimestamp(t *testing.T) {
	setupTempProject(t)
	path := "out.txt"
	data := sampleScaffoldData()
	err := WriteTemplate(path, "t", `{{timestamp}} {{lbrace}} x {{rbrace}}`, data)
	require.NoError(t, err)
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	// RFC3339 timestamp starts with 4-digit year. Just check the closing brace.
	assert.Contains(t, string(body), "{ x }")
}
