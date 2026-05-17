package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// — methodCmd ────────────────────────────────────────────────────────

func TestMethodCmd_RunE_HappyPath(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)
	methodDryRun = false
	require.NoError(t, methodCmd.RunE(methodCmd, []string{"Order", "Archive"}))
}

func TestMethodCmd_RunE_DryRun(t *testing.T) {
	tmp := setupScaffoldedResource(t)
	chdirTest(t, tmp)
	methodDryRun = true
	t.Cleanup(func() { methodDryRun = false })
	require.NoError(t, methodCmd.RunE(methodCmd, []string{"Order", "DryArchive"}))
}

func TestMethodCmd_RunE_DryRunError(t *testing.T) {
	chdirTest(t, t.TempDir())
	methodDryRun = true
	t.Cleanup(func() { methodDryRun = false })
	require.Error(t, methodCmd.RunE(methodCmd, []string{"Ghost", "Vanish"}))
}

// — fieldCmd ─────────────────────────────────────────────────────────

func TestFieldCmd_RunE_HappyPath(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)
	fieldDryRun = false
	require.NoError(t, fieldCmd.RunE(fieldCmd, []string{"Order", "reason:string"}))
}

func TestFieldCmd_RunE_DryRun(t *testing.T) {
	tmp := setupModelOnlyProject(t)
	chdirTest(t, tmp)
	fieldDryRun = true
	t.Cleanup(func() { fieldDryRun = false })
	require.NoError(t, fieldCmd.RunE(fieldCmd, []string{"Order", "notes:text"}))
}

func TestFieldCmd_RunE_BadFieldArg(t *testing.T) {
	chdirTest(t, t.TempDir())
	require.Error(t, fieldCmd.RunE(fieldCmd, []string{"Order", "bad-without-colon"}))
}

func TestFieldCmd_RunE_DryRunError(t *testing.T) {
	chdirTest(t, t.TempDir())
	fieldDryRun = true
	t.Cleanup(func() { fieldDryRun = false })
	require.Error(t, fieldCmd.RunE(fieldCmd, []string{"Order", "reason:string"}))
}

// — endpointCmd ──────────────────────────────────────────────────────

func TestEndpointCmd_RunE_HappyPath(t *testing.T) {
	tmp := setupEndpointResource(t)
	chdirTest(t, tmp)
	endpointDryRun = false
	endpointNoService = false
	require.NoError(t, endpointCmd.RunE(endpointCmd,
		[]string{"Order", "POST", "/orders/{id}/archive"}))
}

func TestEndpointCmd_RunE_DryRun(t *testing.T) {
	tmp := setupEndpointResource(t)
	chdirTest(t, tmp)
	endpointDryRun = true
	endpointNoService = true
	t.Cleanup(func() { endpointDryRun = false; endpointNoService = false })
	require.NoError(t, endpointCmd.RunE(endpointCmd,
		[]string{"Order", "POST", "/orders/{id}/refund"}))
}

func TestEndpointCmd_RunE_DryRunError(t *testing.T) {
	chdirTest(t, t.TempDir())
	endpointDryRun = true
	t.Cleanup(func() { endpointDryRun = false })
	require.Error(t, endpointCmd.RunE(endpointCmd,
		[]string{"Ghost", "POST", "/x"}))
}

// — repoMethodCmd ─────────────────────────────────────────────────────

func TestRepoMethodCmd_RunE_HappyPath(t *testing.T) {
	tmp := setupScaffoldedRepo(t)
	chdirTest(t, tmp)
	repoMethodDryRun = false
	require.NoError(t, repoMethodCmd.RunE(repoMethodCmd, []string{"Order", "Archive"}))
}

func TestRepoMethodCmd_RunE_DryRun(t *testing.T) {
	tmp := setupScaffoldedRepo(t)
	chdirTest(t, tmp)
	repoMethodDryRun = true
	t.Cleanup(func() { repoMethodDryRun = false })
	require.NoError(t, repoMethodCmd.RunE(repoMethodCmd, []string{"Order", "DryArchive"}))
}

func TestRepoMethodCmd_RunE_DryRunError(t *testing.T) {
	chdirTest(t, t.TempDir())
	repoMethodDryRun = true
	t.Cleanup(func() { repoMethodDryRun = false })
	require.Error(t, repoMethodCmd.RunE(repoMethodCmd, []string{"Ghost", "Vanish"}))
}

// — middlewareCmd ────────────────────────────────────────────────────

func TestMiddlewareCmd_RunE_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "app", "rest", "routes", "order.routes.go"),
		`package routes
func OrderRoutes(r interface{}) { r.Get("/orders", nil) }`)
	chdirTest(t, tmp)
	middlewareDryRun = false
	require.NoError(t, middlewareCmd.RunE(middlewareCmd,
		[]string{"GET", "/orders", "auth.Middleware"}))
}

func TestMiddlewareCmd_RunE_DryRun(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "app", "rest", "routes", "order.routes.go"),
		`package routes
func OrderRoutes(r interface{}) { r.Get("/orders", nil) }`)
	chdirTest(t, tmp)
	middlewareDryRun = true
	t.Cleanup(func() { middlewareDryRun = false })
	require.NoError(t, middlewareCmd.RunE(middlewareCmd,
		[]string{"GET", "/orders", "auth.Middleware"}))
}

func TestMiddlewareCmd_RunE_DryRunError(t *testing.T) {
	chdirTest(t, t.TempDir())
	middlewareDryRun = true
	t.Cleanup(func() { middlewareDryRun = false })
	require.Error(t, middlewareCmd.RunE(middlewareCmd,
		[]string{"GET", "/missing", "auth"}))
}

// — relationCmd ──────────────────────────────────────────────────────

func TestRelationCmd_RunE_HappyPath(t *testing.T) {
	tmp := setupRelationProject(t)
	chdirTest(t, tmp)
	relationDryRun = false
	require.NoError(t, relationCmd.RunE(relationCmd, []string{"Order", "belongs_to", "Customer"}))
}

func TestRelationCmd_RunE_DryRun(t *testing.T) {
	tmp := setupRelationProject(t)
	chdirTest(t, tmp)
	relationDryRun = true
	t.Cleanup(func() { relationDryRun = false })
	require.NoError(t, relationCmd.RunE(relationCmd, []string{"Order", "has_many", "LineItem"}))
}

func TestRelationCmd_RunE_DryRunError(t *testing.T) {
	chdirTest(t, t.TempDir())
	relationDryRun = true
	t.Cleanup(func() { relationDryRun = false })
	require.Error(t, relationCmd.RunE(relationCmd, []string{"Ghost", "belongs_to", "Other"}))
}

// — renameCmd ────────────────────────────────────────────────────────

func TestRenameCmd_RunE_PreviewMode(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"),
		[]byte("package models\ntype Order struct{ Total int }\n"), 0o644))
	chdirTest(t, tmp)
	renameApply = false
	require.NoError(t, renameCmd.RunE(renameCmd, []string{"Order.Total", "AmountCents"}))
}

func TestRenameCmd_RunE_ApplyMode(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	models := filepath.Join(tmp, "app", "models")
	require.NoError(t, os.MkdirAll(models, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(models, "order.model.go"),
		[]byte("package models\ntype Order struct{ Total int }\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "db", "migrations"), 0o755))
	chdirTest(t, tmp)
	renameApply = true
	t.Cleanup(func() { renameApply = false })
	require.NoError(t, renameCmd.RunE(renameCmd, []string{"Order.Total", "AmountCents"}))
}

func TestRenameCmd_RunE_MalformedFirstArg(t *testing.T) {
	chdirTest(t, t.TempDir())
	require.Error(t, renameCmd.RunE(renameCmd, []string{"NoSeparator", "NewField"}))
}

func TestRenameCmd_RunE_PreviewError(t *testing.T) {
	chdirTest(t, t.TempDir())
	renameApply = false
	// OldField == NewField → validateRename error inside GenRename.
	require.Error(t, renameCmd.RunE(renameCmd, []string{"Order.Same", "Same"}))
}

// — mockCmd ──────────────────────────────────────────────────────────

func TestMockCmd_RunE_OneInterface(t *testing.T) {
	src := `package interfaces
type Thing interface { F() error }
`
	tmp := setupMockProject(t, src)
	chdirTest(t, tmp)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "app", "services", "interfaces", "thing_service.go"),
		[]byte(src), 0o644))
	mockAll = false
	mockCheck = false
	require.NoError(t, mockCmd.RunE(mockCmd, []string{"Thing"}))
}

func TestMockCmd_RunE_AllNoArg(t *testing.T) {
	tmp := setupMockProject(t, `package interfaces
type One interface{ A() error }
`)
	chdirTest(t, tmp)
	mockAll = true
	mockCheck = false
	t.Cleanup(func() { mockAll = false })
	require.NoError(t, mockCmd.RunE(mockCmd, []string{}))
}
