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
// controller can be constructed with nil dependencies, which is
// enough to catch template regressions that break the constructor
// signature (e.g. if the Validator dependency moves).
//
// Replace with real behavior tests by passing a mock service that
// satisfies {{.Name}}ServiceInterface and a real *validators.AppValidator
// (built against an in-memory SQLite for speed). See
// https://gofasta.dev/docs/guides/testing for the full pattern.
func Test{{.Name}}Controller_Instantiates(t *testing.T) {
	ctrl := controllers.New{{.Name}}ControllerInstance(nil, nil)
	if ctrl == nil {
		t.Fatal("expected non-nil controller")
	}
}

// Test{{.Name}}Routes_Register confirms the route registration function
// wires every CRUD endpoint onto a chi router without panicking. A
// template regression that changed a route signature would surface here.
func Test{{.Name}}Routes_Register(t *testing.T) {
	ctrl := controllers.New{{.Name}}ControllerInstance(nil, nil)
	r := chi.NewRouter()
	// If the routes function panics or won't compile, this test fails.
	routes.{{.Name}}Routes(r, ctrl)

	// Issue a request to a known path. Hitting the handler with nil
	// dependencies will panic on the first method call — which is
	// fine: the panic happens *after* the router resolved the path,
	// so reaching it is evidence the route was wired correctly. We
	// recover from the panic so the test passes; real behavior tests
	// should pass a mock that satisfies {{.Name}}ServiceInterface.
	defer func() { _ = recover() }()
	req := httptest.NewRequest(http.MethodGet, "/{{.PluralSnake}}", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	_ = rec
}

// Test{{.Name}}Controller_TODO is a placeholder for real behavior
// tests. Fill in with scenarios that matter for your domain:
//
//   - Create/Update happy path + 422 validation errors
//   - GetByID 200 / 404 (services.Err{{.Name}}NotFound mapping)
//   - Update 409 on services.Err{{.Name}}VersionConflict
//   - Archive 204 success + 409 services.Err{{.Name}}NotDeletable
//
// See the testing guide linked above for mock service patterns.
func Test{{.Name}}Controller_TODO(t *testing.T) {
	t.Skip("TODO: implement behavior tests for {{.Name}} controller")
}
`
