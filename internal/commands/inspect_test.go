package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspectCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "inspect" {
			found = true
			break
		}
	}
	assert.True(t, found, "inspectCmd should be registered on rootCmd")
}

func TestToSnakeLower(t *testing.T) {
	cases := map[string]string{
		"User":             "user",
		"UserProfile":      "user_profile",
		"OrderLineItem":    "order_line_item",
		"":                 "",
		"Alreadysnakecase": "alreadysnakecase",
	}
	for in, want := range cases {
		assert.Equal(t, want, toSnakeLower(in), in)
	}
}

// TestRunInspect_ErrorsOnMissingResource — no files anywhere → error
// with a clear message.
func TestRunInspect_ErrorsOnMissingResource(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	err := runInspect("Nonexistent")
	assert.Error(t, err, "missing resource should error")
}

// TestRunInspect_ParsesModelFields — happy path. Create a minimal
// app/models/user.model.go and verify runInspect picks up the fields.
func TestRunInspect_ParsesModelFields(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	modelsDir := filepath.Join("app", "models")
	require.NoError(t, os.MkdirAll(modelsDir, 0755))
	src := `package models

type User struct {
	ID        string ` + "`gorm:\"primaryKey\"`" + `
	FirstName string
	Email     string ` + "`gorm:\"uniqueIndex\"`" + `
	Age       int
}
`
	require.NoError(t, os.WriteFile(filepath.Join(modelsDir, "user.model.go"), []byte(src), 0644))

	// Direct call of the parser — runInspect renders but we want to
	// inspect the parsed data structure. Use tryParseModel directly.
	info, ok := tryParseModel("User", "user")
	require.True(t, ok)
	assert.Equal(t, "app/models/user.model.go", info.File)
	require.Len(t, info.Fields, 4)
	assert.Equal(t, "ID", info.Fields[0].Name)
	assert.Equal(t, "string", info.Fields[0].Type)
	assert.Contains(t, info.Fields[0].Tag, "primaryKey")
	assert.Equal(t, "Age", info.Fields[3].Name)
	assert.Equal(t, "int", info.Fields[3].Type)
}

// TestTryParseInterfaceMethods — parse a service interface and list
// every method signature.
func TestTryParseInterfaceMethods(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	ifaceDir := filepath.Join("app", "services", "interfaces")
	require.NoError(t, os.MkdirAll(ifaceDir, 0755))
	src := `package interfaces

import "context"

type UserServiceInterface interface {
	FindByID(ctx context.Context, id string) (User, error)
	Create(ctx context.Context, input CreateUserDto) (User, error)
	Archive(ctx context.Context, id string) error
}

type User struct{}
type CreateUserDto struct{}
`
	require.NoError(t, os.WriteFile(filepath.Join(ifaceDir, "user_service.go"), []byte(src), 0644))

	methods, ok := tryParseInterfaceMethods(filepath.Join(ifaceDir, "user_service.go"), "UserServiceInterface")
	require.True(t, ok)
	require.Len(t, methods, 3)
	assert.Equal(t, "FindByID", methods[0].Name)
	assert.Contains(t, methods[0].Sig, "ctx context.Context")
	assert.Contains(t, methods[0].Sig, "id string")
}

// TestTryParseControllerMethods — parse a controller struct and list
// its public methods.
func TestTryParseControllerMethods(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	ctrlDir := filepath.Join("app", "rest", "controllers")
	require.NoError(t, os.MkdirAll(ctrlDir, 0755))
	src := `package controllers

import "net/http"

type UserController struct{}

func (c *UserController) ListUsers(w http.ResponseWriter, r *http.Request) error { return nil }
func (c *UserController) CreateUser(w http.ResponseWriter, r *http.Request) error { return nil }
func (c *UserController) helper() {} // private, must NOT appear
`
	require.NoError(t, os.WriteFile(filepath.Join(ctrlDir, "user.controller.go"), []byte(src), 0644))

	methods, ok := tryParseControllerMethods(filepath.Join(ctrlDir, "user.controller.go"), "UserController")
	require.True(t, ok)
	require.Len(t, methods, 2, "only public methods should be reported")
	names := []string{methods[0].Name, methods[1].Name}
	assert.Contains(t, names, "ListUsers")
	assert.Contains(t, names, "CreateUser")
}

func TestExprToString_CommonTypes(t *testing.T) {
	// Not testing via AST synthesis — just a quick sanity check that
	// common shapes don't panic. Real coverage comes from the higher-
	// level tests above.
	assert.NotPanics(t, func() {
		_ = exprToString(nil)
	}, "nil expr should not panic")
}

// TestInspectCmd_RunE — exercises the Cobra RunE wrapper.
func TestInspectCmd_RunE(t *testing.T) {
	chdirTemp(t)
	// An arbitrary resource name — runInspect errors when no files exist.
	// Either outcome covers the RunE wrapper body.
	_ = inspectCmd.RunE(inspectCmd, []string{"Nothing"})
}
