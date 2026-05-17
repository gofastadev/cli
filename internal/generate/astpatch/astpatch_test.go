package astpatch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/require"
)

// writeTemp puts a temp file with the given source on disk and returns
// the path. Saves boilerplate across the table-driven cases.
func writeTemp(t *testing.T, src string) string {
	t.Helper()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "input.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	return path
}

func TestParse_HappyPath(t *testing.T) {
	path := writeTemp(t, "package x\n\nfunc F() {}\n")
	f, err := Parse(path)
	require.NoError(t, err)
	require.NotNil(t, f.Dst)
	require.Equal(t, "x", f.Dst.Name.Name)
}

func TestParse_SyntaxErrorReturnsClierr(t *testing.T) {
	path := writeTemp(t, "package x\n\nfunc F(\n") // unterminated
	_, err := Parse(path)
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeASTParseFailed), ce.Code)
}

func TestAppendInterfaceMethod_PreservesExistingMethodsAndComments(t *testing.T) {
	src := `package interfaces

// OrderService is the order business-logic contract.
type OrderService interface {
	// Create persists a new order.
	Create(name string) error
}
`
	path := writeTemp(t, src)
	f, err := Parse(path)
	require.NoError(t, err)

	iface, err := FindInterface(f, "OrderService")
	require.NoError(t, err)
	require.False(t, InterfaceHasMethod(iface, "Archive"))

	require.NoError(t, AppendInterfaceMethod(iface, "Archive(id string) error"))

	body, err := Render(f)
	require.NoError(t, err)

	s := string(body)
	// Doc comments preserved.
	require.Contains(t, s, "// OrderService is the order business-logic contract.")
	require.Contains(t, s, "// Create persists a new order.")
	// Existing method retained.
	require.Contains(t, s, "Create(name string) error")
	// New method appended.
	require.Contains(t, s, "Archive(id string) error")
}

func TestAppendInterfaceMethod_IdempotencyCheck(t *testing.T) {
	src := `package i
type S interface {
	Already() error
}
`
	path := writeTemp(t, src)
	f, err := Parse(path)
	require.NoError(t, err)
	iface, err := FindInterface(f, "S")
	require.NoError(t, err)
	require.True(t, InterfaceHasMethod(iface, "Already"))
	require.False(t, InterfaceHasMethod(iface, "Missing"))
}

func TestAppendStructField_AppendsCorrectly(t *testing.T) {
	src := `package m

type User struct {
	ID   string
	Name string ` + "`json:\"name\"`" + `
}
`
	path := writeTemp(t, src)
	f, err := Parse(path)
	require.NoError(t, err)

	st, err := FindStruct(f, "User")
	require.NoError(t, err)
	require.True(t, StructHasField(st, "ID"))
	require.False(t, StructHasField(st, "DeletedAt"))

	require.NoError(t, AppendStructField(st, "DeletedAt *time.Time `gorm:\"index\"`"))

	body, err := Render(f)
	require.NoError(t, err)
	s := string(body)
	require.Contains(t, s, "DeletedAt")
	require.Contains(t, s, "gorm:\"index\"")
}

func TestFindInterface_MissingReturnsClierr(t *testing.T) {
	path := writeTemp(t, "package x\n\ntype A struct{}\n")
	f, _ := Parse(path)
	_, err := FindInterface(f, "Nope")
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeASTPatchFailed), ce.Code)
}

func TestEnsureImport_AddsAndIsIdempotent(t *testing.T) {
	src := `package x

import "fmt"

func F() { fmt.Println() }
`
	path := writeTemp(t, src)
	f, err := Parse(path)
	require.NoError(t, err)

	added := EnsureImport(f, "context")
	require.True(t, added, "context should be added")

	added2 := EnsureImport(f, "context")
	require.False(t, added2, "context should not be added a second time")

	body, err := Render(f)
	require.NoError(t, err)
	require.True(t, strings.Contains(string(body), `"context"`))
}

func TestWriteBack_OverwritesFile(t *testing.T) {
	path := writeTemp(t, "package x\n\ntype S struct{ A string }\n")
	f, err := Parse(path)
	require.NoError(t, err)
	st, _ := FindStruct(f, "S")
	require.NoError(t, AppendStructField(st, "B int"))

	body, err := WriteBack(f)
	require.NoError(t, err)

	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(body), string(onDisk))
	require.Contains(t, string(onDisk), "B int")
}
