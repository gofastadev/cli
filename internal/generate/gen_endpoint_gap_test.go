package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// — GenEndpoint top-level early returns ─────────────────────────────────

func TestGenEndpoint_ValidationErrorPropagates(t *testing.T) {
	// Empty path with valid method — passes endpointDataDefaults
	// without crashing in deriveHandlerName, then validateEndpoint
	// flags the missing path.
	err := GenEndpoint(EndpointData{Resource: "Order", HTTPMethod: "POST"})
	require.Error(t, err)
}

func TestGenEndpoint_MissingRoutesFileErrors(t *testing.T) {
	// Controller exists but routes file is missing — second ensureExists fails.
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	mustWriteFile(t, filepath.Join(tmp, "app", "rest", "controllers", "order.controller.go"),
		"package controllers\n\ntype OrderController struct{}\n")
	chdirTest(t, tmp)
	err := GenEndpoint(EndpointData{Resource: "Order", HTTPMethod: "POST", Path: "/orders"})
	require.Error(t, err)
}

// — patchEndpointController: missing struct branch ───────────────────────

func TestPatchEndpointController_StructMissing(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "ctrl.go"),
		"package controllers\n// no OrderController\n")
	chdirTest(t, tmp)
	err := patchEndpointController(EndpointData{
		Resource: "Order", HTTPMethod: "POST", Path: "/x",
		HandlerName: "X", ControllerFile: "ctrl.go",
	})
	require.Error(t, err)
}

func TestPatchEndpointController_ParseError(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "broken.go"), "package x\nfunc {\n")
	chdirTest(t, tmp)
	err := patchEndpointController(EndpointData{
		Resource: "X", HandlerName: "Y", ControllerFile: "broken.go",
	})
	require.Error(t, err)
}

// — patchEndpointRoutes: read failure branch + routes patch failure ────

func TestPatchEndpointRoutes_ReadFailure(t *testing.T) {
	err := patchEndpointRoutes(EndpointData{RoutesFile: "/nope/nonexistent.go"})
	require.Error(t, err)
}

func TestPatchEndpointRoutes_FuncNotFound(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "routes.go"),
		"package routes\nfunc SomethingElse() {}\n")
	chdirTest(t, tmp)
	err := patchEndpointRoutes(EndpointData{
		Resource: "Order", HTTPMethod: "POST", Path: "/x",
		HandlerName: "Y", RoutesFile: "routes.go",
	})
	require.Error(t, err)
}

// — patchEndpointService: parse failure + missing interface ────────────

func TestPatchEndpointService_ParseError(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "svc.go"), "package x\nfunc {\n")
	chdirTest(t, tmp)
	err := patchEndpointService(EndpointData{Resource: "X", ServiceFile: "svc.go"})
	require.Error(t, err)
}

func TestPatchEndpointService_InterfaceMissing(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "svc.go"),
		"package interfaces\n// no XServiceInterface\n")
	chdirTest(t, tmp)
	err := patchEndpointService(EndpointData{Resource: "X", ServiceFile: "svc.go"})
	require.Error(t, err)
}

func TestPatchEndpointService_AlreadyHasMethodIsNoop(t *testing.T) {
	tmp := setupEndpointResource(t)
	chdirTest(t, tmp)
	// First add ArchiveOrder.
	require.NoError(t, GenEndpoint(EndpointData{
		Resource: "Order", HTTPMethod: "POST",
		Path: "/orders/{id}/archive", WithService: true,
	}))
	// Direct call to patchEndpointService with the same method should
	// no-op (InterfaceHasMethod returns true).
	require.NoError(t, patchEndpointService(EndpointData{
		Resource:    "Order",
		HandlerName: "ArchiveOrder",
		ServiceFile: filepath.Join("app", "services", "interfaces", "order_service.go"),
	}))
}

// — deriveHandlerName edge cases ──────────────────────────────────────

func TestDeriveHandlerName_EdgeCases(t *testing.T) {
	cases := []struct {
		method, path, want string
	}{
		{"POST", "/orders", "OrdersOrder"},  // single-segment POST → action = "orders"
		{"GET", "/orders", "ListOrder"},      // collection GET
		{"GET", "/orders/{id}", "GetOrder"},  // item GET
		{"PUT", "/orders/{id}", "UpdateOrder"},
		{"PATCH", "/orders/{id}", "UpdateOrder"},
		{"DELETE", "/orders/{id}", "DeleteOrder"},
		{"OPTIONS", "/orders/{id}", "OptionsOrder"}, // fallback to method
		{"POST", "/{id}", "CreateOrder"},     // all-placeholder
		{"GET", "/orders/{id}/items", "ItemsOrder"},
	}
	for _, c := range cases {
		got := deriveHandlerName(c.method, c.path, "Order")
		require.Equal(t, c.want, got, "%s %s", c.method, c.path)
	}
}

// — Dry-run paths ──────────────────────────────────────────────────────

func TestGenEndpoint_DryRunRecordsPatches(t *testing.T) {
	tmp := setupEndpointResource(t)
	chdirTest(t, tmp)
	SetDryRun(true)
	defer SetDryRun(false)
	require.NoError(t, GenEndpoint(EndpointData{
		Resource: "Order", HTTPMethod: "POST",
		Path: "/orders/{id}/archive", WithService: true,
	}))
	plan := Plan()
	require.GreaterOrEqual(t, len(plan), 1)
}

// — writeBytesOrRecord os.WriteFile error ──────────────────────────────

func TestWriteBytesOrRecord_WriteError(t *testing.T) {
	dir := t.TempDir()
	readonly := filepath.Join(dir, "ro")
	require.NoError(t, os.Mkdir(readonly, 0o555))
	t.Cleanup(func() { _ = os.Chmod(readonly, 0o755) })

	err := writeBytesOrRecord(filepath.Join(readonly, "x.go"), []byte("x"), "")
	require.Error(t, err)
}

// — readFile error wrapping ────────────────────────────────────────────

func TestReadFile_Error(t *testing.T) {
	_, err := readFile("/nope/missing.go")
	require.Error(t, err)
}

// — endpointRouteRegistered uses .With wrap ───────────────────────────

func TestEndpointRouteRegistered_WithMiddleware(t *testing.T) {
	body := []byte(`r.With(auth).Post("/orders", h)`)
	require.True(t, endpointRouteRegistered(body, "POST", "/orders"))
}

// — injectIntoRoutesFunc bracket counting edge cases ──────────────────

func TestInjectIntoRoutesFunc_NoOpenBrace(t *testing.T) {
	body := []byte("func OrderRoutes(") // no `{`
	_, ok := injectIntoRoutesFunc(body, "Order", "x")
	require.False(t, ok)
}

func TestInjectIntoRoutesFunc_UnbalancedBraces(t *testing.T) {
	body := []byte("func OrderRoutes(r chi.Router) {")
	_, ok := injectIntoRoutesFunc(body, "Order", "x")
	require.False(t, ok)
}
