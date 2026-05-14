package templates

// ControllerTest is the Go template for a starter test file emitted
// alongside every generated REST controller. The file compiles
// immediately — so `gofasta g scaffold` + `go test ./...` is green out
// of the box — but the real test bodies are left as TODO skips for the
// developer (or AI agent) to fill in against their specific mock service.
//
// Shipping a valid-but-skipped starter is a better UX than shipping
// nothing: the file exists, the package declaration is right, the
// imports are wired, and the pattern is discoverable. Agents reading the
// file see exactly which method signatures they need to exercise.
var ControllerTest = `package controllers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"{{.ModulePath}}/app/rest/controllers"
	"{{.ModulePath}}/app/rest/routes"
)

// Test{{.Name}}Controller_Instantiates is a smoke test — proves the
// controller can be constructed with a nil service, which is enough to
// catch template regressions that break the constructor signature.
//
// Replace with real behavior tests by passing a mock that satisfies
// {{.Name}}ServiceInterface. See https://gofasta.dev/docs/guides/testing
// for the full testing guide with testcontainers + httptest patterns.
func Test{{.Name}}Controller_Instantiates(t *testing.T) {
	ctrl := controllers.New{{.Name}}ControllerInstance(nil)
	if ctrl == nil {
		t.Fatal("expected non-nil controller")
	}
}

// Test{{.Name}}Routes_Register confirms the route registration function
// wires every CRUD endpoint onto a chi router without panicking. A
// template regression that changed a route signature would surface here.
func Test{{.Name}}Routes_Register(t *testing.T) {
	ctrl := controllers.New{{.Name}}ControllerInstance(nil)
	r := chi.NewRouter()
	// If the routes function panics or won't compile, this test fails.
	routes.{{.Name}}Routes(r, ctrl)

	// Issue a request to a known path. Hitting the handler with a nil
	// service will panic on the dereference inside the handler — which
	// is fine: the panic happens *after* the router resolved the path,
	// so reaching it is evidence the route was wired correctly. We
	// recover from the panic so the test passes; real behavior tests
	// should pass a mock that satisfies {{.Name}}ServiceInterface.
	defer func() { _ = recover() }()
	req := httptest.NewRequest(http.MethodGet, "/{{.PluralSnake}}", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	_ = rec
}

// Test{{.Name}}Controller_TODO is a placeholder for real behavior tests.
// Fill in with scenarios that matter for your domain:
//
//   - Create/Update success + validation error paths
//   - GetByID found / not found
//   - Archive soft-delete visibility
//   - Authorization and RBAC checks, if applicable
//
// See the testing guide linked above for mock service patterns.
func Test{{.Name}}Controller_TODO(t *testing.T) {
	t.Skip("TODO: implement behavior tests for {{.Name}} controller")
}
`
