package generate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/generate/astpatch"
	"github.com/stretchr/testify/require"
)

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
	// no-op on both halves (InterfaceHasMethod + FindFunc hit).
	require.NoError(t, patchEndpointService(EndpointData{
		Resource:        "Order",
		HandlerName:     "ArchiveOrder",
		ServiceFile:     filepath.Join("app", "services", "interfaces", "order_service.go"),
		ServiceImplFile: filepath.Join("app", "services", "order.service.go"),
	}))
}

// TestGenEndpoint_WithServicePatchesImplAndHandler — the compile-safety
// contract: --with-service must leave the service satisfying its
// interface (impl stub added) and the handler delegating to it.
func TestGenEndpoint_WithServicePatchesImplAndHandler(t *testing.T) {
	tmp := setupEndpointResource(t)
	chdirTest(t, tmp)

	require.NoError(t, GenEndpoint(EndpointData{
		Resource: "Order", HTTPMethod: "POST",
		Path: "/orders/{id}/archive", WithService: true,
	}))

	impl, err := os.ReadFile(filepath.Join(tmp, "app", "services", "order.service.go"))
	require.NoError(t, err)
	require.Contains(t, string(impl), "func (s *OrderService) ArchiveOrder(ctx context.Context) error")
	require.Contains(t, string(impl), `fmt.Errorf("OrderServiceInterface.ArchiveOrder: not implemented")`)

	ctrl, err := os.ReadFile(filepath.Join(tmp, "app", "rest", "controllers", "order.controller.go"))
	require.NoError(t, err)
	require.Contains(t, string(ctrl), "if err := c.svc.ArchiveOrder(r.Context()); err != nil {")
	require.Contains(t, string(ctrl), "w.WriteHeader(http.StatusNoContent)")
	require.NotContains(t, string(ctrl), "TODO: implement")
}

// TestGenEndpoint_NoServiceKeepsTODOHandler — without a service method
// there is nothing to call; the handler body stays a TODO.
func TestGenEndpoint_NoServiceKeepsTODOHandler(t *testing.T) {
	tmp := setupEndpointResource(t)
	chdirTest(t, tmp)

	require.NoError(t, GenEndpoint(EndpointData{
		Resource: "Order", HTTPMethod: "POST",
		Path: "/orders/{id}/refund", WithService: false,
	}))

	ctrl, err := os.ReadFile(filepath.Join(tmp, "app", "rest", "controllers", "order.controller.go"))
	require.NoError(t, err)
	require.Contains(t, string(ctrl), "TODO: implement")
	require.NotContains(t, string(ctrl), "c.svc.RefundOrder")

	// And the service files stay untouched.
	impl, err := os.ReadFile(filepath.Join(tmp, "app", "services", "order.service.go"))
	require.NoError(t, err)
	require.NotContains(t, string(impl), "RefundOrder")
}

func TestDeriveHandlerName_EdgeCases(t *testing.T) {
	cases := []struct {
		method, path, want string
	}{
		{"POST", "/orders", "OrdersOrder"},  // single-segment POST → action = "orders"
		{"GET", "/orders", "ListOrder"},     // collection GET
		{"GET", "/orders/{id}", "GetOrder"}, // item GET
		{"PUT", "/orders/{id}", "UpdateOrder"},
		{"PATCH", "/orders/{id}", "UpdateOrder"},
		{"DELETE", "/orders/{id}", "DeleteOrder"},
		{"OPTIONS", "/orders/{id}", "OptionsOrder"}, // fallback to method
		{"POST", "/{id}", "CreateOrder"},            // all-placeholder
		{"GET", "/orders/{id}/items", "ItemsOrder"},
	}
	for _, c := range cases {
		got := deriveHandlerName(c.method, c.path, "Order")
		require.Equal(t, c.want, got, "%s %s", c.method, c.path)
	}
}

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

func TestWriteBytesOrRecord_WriteError(t *testing.T) {
	dir := t.TempDir()
	readonly := filepath.Join(dir, "ro")
	require.NoError(t, os.Mkdir(readonly, 0o555))
	t.Cleanup(func() { _ = os.Chmod(readonly, 0o755) })

	err := writeBytesOrRecord(filepath.Join(readonly, "x.go"), []byte("x"), "")
	require.Error(t, err)
}

func TestReadFile_Error(t *testing.T) {
	_, err := readFile("/nope/missing.go")
	require.Error(t, err)
}

func TestEndpointRouteRegistered_WithMiddleware(t *testing.T) {
	body := []byte(`r.With(auth).Post("/orders", h)`)
	require.True(t, endpointRouteRegistered(body, "POST", "/orders"))
}

func TestValidateEndpoint_EmptyResource(t *testing.T) {
	err := validateEndpoint(EndpointData{HTTPMethod: "POST", Path: "/x"})
	require.Error(t, err)
}

func TestGenEndpoint_RoutesPatchErrorPropagates(t *testing.T) {
	// Set up controller + routes such that controller patch succeeds
	// but routes patch fails (routes file has no <Resource>Routes func).
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	mustWriteFile(t, filepath.Join(tmp, "app", "rest", "controllers", "order.controller.go"),
		`package controllers
import "net/http"
type OrderController struct{}
var _ = http.MethodGet
`)
	mustWriteFile(t, filepath.Join(tmp, "app", "rest", "routes", "order.routes.go"),
		"package routes\n// no OrderRoutes\n")
	chdirTest(t, tmp)
	err := GenEndpoint(EndpointData{
		Resource: "Order", HTTPMethod: "POST", Path: "/orders/{id}/archive",
	})
	require.Error(t, err)
}

func TestPatchEndpointController_AppendFuncDeclError(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "ctrl.go"),
		"package controllers\ntype OrderController struct{}\n")
	chdirTest(t, tmp)

	saved := astpatchAppendFuncDeclFn
	astpatchAppendFuncDeclFn = func(_ *astpatch.File, _ string) error {
		return errStubGenerate
	}
	t.Cleanup(func() { astpatchAppendFuncDeclFn = saved })

	err := patchEndpointController(EndpointData{
		Resource: "Order", HandlerName: "X", ControllerFile: "ctrl.go",
	})
	require.Error(t, err)
}

func TestPatchEndpointRoutes_DuplicateRouteRejected(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "routes.go"), `package routes
func OrderRoutes() { r.Post("/orders/{id}/archive", h) }
`)
	chdirTest(t, tmp)
	err := patchEndpointRoutes(EndpointData{
		Resource: "Order", HTTPMethod: "POST", Path: "/orders/{id}/archive",
		HandlerName: "Archive", RoutesFile: "routes.go",
	})
	require.Error(t, err)
}

func TestPatchEndpointService_AppendInterfaceMethodError(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "svc.go"), `package interfaces
import "context"
type OrderServiceInterface interface { F(ctx context.Context) error }
`)
	chdirTest(t, tmp)
	err := patchEndpointService(EndpointData{
		Resource: "Order", HandlerName: "Bad{", ServiceFile: "svc.go",
	})
	require.Error(t, err)
}

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

// TestInjectIntoRoutesFunc_NestedBraces — nested braces inside the
// function body exercise the `case '{': depth++` increment branch.
func TestInjectIntoRoutesFunc_NestedBraces(t *testing.T) {
	body := []byte(`package routes

func OrderRoutes(r chi.Router) {
	if x := 1; x > 0 {
		r.Get("/orders", nil)
	}
}
`)
	patched, ok := injectIntoRoutesFunc(body, "Order", "\tr.Post(\"/x\", nil)")
	require.True(t, ok)
	require.Contains(t, string(patched), `r.Post("/x", nil)`)
}

// TestInjectIntoRoutesFunc_InsertAtStartOfFile — patched file's closing
// brace is on the first character (insertAt walks back to 0). Trigger
// by constructing a one-line file with no preceding newline.
func TestInjectIntoRoutesFunc_InsertAtStartOfFile(t *testing.T) {
	body := []byte("func OrderRoutes(){}")
	patched, ok := injectIntoRoutesFunc(body, "Order", "X")
	require.True(t, ok)
	require.Contains(t, string(patched), "X")
}

func TestGenEndpoint_HappyPath_PatchesAllThreeLayers(t *testing.T) {
	tmp := setupEndpointResource(t)
	chdirTest(t, tmp)

	require.NoError(t, GenEndpoint(EndpointData{
		Resource:    "Order",
		HTTPMethod:  "POST",
		Path:        "/orders/{id}/archive",
		WithService: true,
	}))

	// Controller now has the handler.
	cc, _ := os.ReadFile(filepath.Join(tmp, "app", "rest", "controllers", "order.controller.go"))
	require.Contains(t, string(cc), "ArchiveOrder")
	require.Contains(t, string(cc), "@Router")

	// Routes file has the new route.
	rr, _ := os.ReadFile(filepath.Join(tmp, "app", "rest", "routes", "order.routes.go"))
	require.Contains(t, string(rr), `r.Post("/orders/{id}/archive"`)

	// Service interface gained the method.
	ss, _ := os.ReadFile(filepath.Join(tmp, "app", "services", "interfaces", "order_service.go"))
	require.Contains(t, string(ss), "ArchiveOrder(ctx context.Context) error")
}

func TestGenEndpoint_AutoDerivesHandlerName(t *testing.T) {
	// deriveHandlerName picks the last non-placeholder path segment as
	// the action verb, then suffixes with the resource. When the path
	// is just `/<resource>`, the segment IS the resource so we get
	// `<Resource><Resource>`-style names for POST (CRUD on collection).
	// For trailing-placeholder paths it falls through to verb-based
	// defaults (Get/Update/Delete).
	cases := []struct {
		method, path, want string
	}{
		{"POST", "/orders/{id}/archive", "ArchiveOrder"},
		{"POST", "/orders/{id}/refund", "RefundOrder"},
		{"GET", "/orders/{id}", "GetOrder"},
		{"PUT", "/orders/{id}", "UpdateOrder"},
		{"DELETE", "/orders/{id}", "DeleteOrder"},
	}
	for _, tc := range cases {
		got := deriveHandlerName(tc.method, tc.path, "Order")
		require.Equal(t, tc.want, got,
			"deriveHandlerName(%s, %s) = %s", tc.method, tc.path, got)
	}
}

func TestGenEndpoint_ExplicitHandlerNameWins(t *testing.T) {
	tmp := setupEndpointResource(t)
	chdirTest(t, tmp)

	require.NoError(t, GenEndpoint(EndpointData{
		Resource:    "Order",
		HTTPMethod:  "POST",
		Path:        "/orders/{id}/refund",
		HandlerName: "ProcessRefund",
		WithService: true,
	}))

	cc, _ := os.ReadFile(filepath.Join(tmp, "app", "rest", "controllers", "order.controller.go"))
	require.Contains(t, string(cc), "func (c *OrderController) ProcessRefund(")
}

func TestGenEndpoint_NoServiceSkipsInterfacePatch(t *testing.T) {
	tmp := setupEndpointResource(t)
	chdirTest(t, tmp)

	require.NoError(t, GenEndpoint(EndpointData{
		Resource:    "Order",
		HTTPMethod:  "POST",
		Path:        "/orders/{id}/archive",
		WithService: false,
	}))

	ss, _ := os.ReadFile(filepath.Join(tmp, "app", "services", "interfaces", "order_service.go"))
	require.NotContains(t, string(ss), "ArchiveOrder",
		"service file must be untouched when WithService=false")
}

func TestGenEndpoint_RejectsDuplicateRoute(t *testing.T) {
	tmp := setupEndpointResource(t)
	chdirTest(t, tmp)

	// First call lands the route.
	require.NoError(t, GenEndpoint(EndpointData{
		Resource:    "Order",
		HTTPMethod:  "POST",
		Path:        "/orders/{id}/archive",
		HandlerName: "ArchiveOrder",
	}))

	// Second call with same METHOD+path must error on the handler
	// (controller idempotency hit) — METHOD_ALREADY_EXISTS code.
	err := GenEndpoint(EndpointData{
		Resource:    "Order",
		HTTPMethod:  "POST",
		Path:        "/orders/{id}/archive",
		HandlerName: "ArchiveOrder",
	})
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeMethodAlreadyExists), ce.Code)
}

func TestGenEndpoint_MissingControllerErrors(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))
	chdirTest(t, tmp)

	err := GenEndpoint(EndpointData{
		Resource:   "Ghost",
		HTTPMethod: "POST",
		Path:       "/ghosts",
	})
	require.Error(t, err)
	var ce *clierr.Error
	require.True(t, errors.As(err, &ce))
	require.Equal(t, string(clierr.CodeResourceNotFound), ce.Code)
}

func TestGenEndpoint_ValidationErrors(t *testing.T) {
	t.Run("missing-method", func(t *testing.T) {
		err := validateEndpoint(EndpointData{Resource: "Order", Path: "/x"})
		require.Error(t, err)
	})
	t.Run("missing-path", func(t *testing.T) {
		err := validateEndpoint(EndpointData{Resource: "Order", HTTPMethod: "GET"})
		require.Error(t, err)
	})
	t.Run("invalid-method", func(t *testing.T) {
		err := validateEndpoint(EndpointData{
			Resource: "Order", HTTPMethod: "BOGUS", Path: "/x",
		})
		require.Error(t, err)
	})
	t.Run("path-without-slash", func(t *testing.T) {
		err := validateEndpoint(EndpointData{
			Resource: "Order", HTTPMethod: "GET", Path: "orders",
		})
		require.Error(t, err)
	})
	t.Run("happy", func(t *testing.T) {
		err := validateEndpoint(EndpointData{
			Resource: "Order", HTTPMethod: "GET", Path: "/orders",
		})
		require.NoError(t, err)
	})
}

func TestToChiVerb(t *testing.T) {
	cases := map[string]string{
		"GET": "Get", "POST": "Post", "PUT": "Put",
		"DELETE": "Delete", "PATCH": "Patch",
		"HEAD": "Head", "OPTIONS": "Options",
		"weird": "Weird",
	}
	for in, want := range cases {
		require.Equal(t, want, toChiVerb(in))
	}
}

func TestInjectIntoRoutesFunc_BalancedBraces(t *testing.T) {
	body := []byte(`package routes

func OrderRoutes(r chi.Router) {
	r.Get("/orders", nil)
}
`)
	patched, ok := injectIntoRoutesFunc(body, "Order", "\tr.Post(\"/x\", nil)")
	require.True(t, ok)
	require.Contains(t, string(patched), `r.Post("/x", nil)`)
}

func TestInjectIntoRoutesFunc_MissingFuncReturnsFalse(t *testing.T) {
	body := []byte("package routes\n\nfunc SomethingElse(r chi.Router) {}\n")
	patched, ok := injectIntoRoutesFunc(body, "Order", "x")
	require.False(t, ok)
	require.Equal(t, body, patched)
}

func TestEndpointRouteRegistered(t *testing.T) {
	body := []byte(`r.Get("/orders", nil)
r.Post("/orders/{id}/archive", nil)`)
	require.True(t, endpointRouteRegistered(body, "GET", "/orders"))
	require.True(t, endpointRouteRegistered(body, "POST", "/orders/{id}/archive"))
	require.False(t, endpointRouteRegistered(body, "POST", "/orders"))
	require.False(t, endpointRouteRegistered(body, "DELETE", "/orders"))
}

func TestBuildEndpointHandlerStub_IncludesSwaggerAndSignature(t *testing.T) {
	stub := buildEndpointHandlerStub(EndpointData{
		Resource:    "Order",
		HTTPMethod:  "POST",
		Path:        "/orders/{id}/archive",
		HandlerName: "ArchiveOrder",
	}, "OrderController")

	require.True(t, strings.Contains(stub, "@Router   /orders/{id}/archive [post]"))
	require.True(t, strings.Contains(stub, "func (c *OrderController) ArchiveOrder(w http.ResponseWriter, r *http.Request) error"))
}
