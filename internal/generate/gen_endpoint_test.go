package generate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/stretchr/testify/require"
)

// setupEndpointResource lays out the minimal scaffold layout that
// GenEndpoint expects: controller + routes + service-interface for one
// resource. Returns the project root.
func setupEndpointResource(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/m\n\ngo 1.25\n"), 0o644))

	mustWriteFile(t, filepath.Join(tmp, "app", "rest", "controllers", "order.controller.go"), `package controllers

import "net/http"

// OrderController is the order REST surface.
type OrderController struct{}

// List handles GET /orders.
func (c *OrderController) List(w http.ResponseWriter, r *http.Request) error { return nil }
`)
	mustWriteFile(t, filepath.Join(tmp, "app", "rest", "routes", "order.routes.go"), `package routes

import (
	"github.com/go-chi/chi/v5"
)

func OrderRoutes(r chi.Router) {
	r.Get("/orders", nil)
}
`)
	mustWriteFile(t, filepath.Join(tmp, "app", "services", "interfaces", "order_service.go"), `package interfaces

import "context"

// OrderServiceInterface is the order business-logic contract.
type OrderServiceInterface interface {
	List(ctx context.Context) error
}
`)
	return tmp
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
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
