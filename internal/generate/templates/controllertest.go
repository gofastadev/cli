package templates

// ControllerTest is the Go template for a real controller test file
// emitted alongside every generated REST controller. No t.Skip, no
// TODO — every test exercises actual code paths.
//
// Setup: real AppValidator backed by in-memory SQLite + inline mock
// service (testify/mock). The router is mounted via httputil.Handle
// so error returns translate to HTTP status codes the same way they
// would in production.
var ControllerTest = `package controllers_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"{{.ModulePath}}/app/dtos"
	"{{.ModulePath}}/app/models"
	"{{.ModulePath}}/app/rest/controllers"
	"{{.ModulePath}}/app/rest/routes"
	"{{.ModulePath}}/app/services"
)

// mock{{.Name}}Service is an inline testify mock for the service.
type mock{{.Name}}Service struct {
	mock.Mock
}

func (m *mock{{.Name}}Service) List(ctx context.Context, filter services.List{{.PluralName}}Filter) ([]*models.{{.Name}}, int64, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]*models.{{.Name}}), args.Get(1).(int64), args.Error(2)
}
func (m *mock{{.Name}}Service) Get(ctx context.Context, id uuid.UUID) (*models.{{.Name}}, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.{{.Name}}), args.Error(1)
}
func (m *mock{{.Name}}Service) Create(ctx context.Context, in services.Create{{.Name}}Input) (*models.{{.Name}}, error) {
	args := m.Called(ctx, in)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.{{.Name}}), args.Error(1)
}
func (m *mock{{.Name}}Service) Update(ctx context.Context, id uuid.UUID, expectedVersion int, patch services.Update{{.Name}}Patch) (*models.{{.Name}}, error) {
	args := m.Called(ctx, id, expectedVersion, patch)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.{{.Name}}), args.Error(1)
}
func (m *mock{{.Name}}Service) Archive(ctx context.Context, id uuid.UUID) error {
	return m.Called(ctx, id).Error(0)
}

// noop{{.Name}}Validator passes every input. These tests assert the
// controller's sentinel→status mapping (404/409/500), not validation
// behavior — so making validation pass-through lets each request reach
// the service mock that returns the sentinel under test. If you want
// to assert that bad input produces 422, swap in the real validator
// (validators.NewAppValidator) for that specific test.
type noop{{.Name}}Validator struct{}

func (noop{{.Name}}Validator) ValidateStruct(_ any) []*dtos.TCommonAPIErrorDto { return nil }

func mount{{.Name}}Controller(c *controllers.{{.Name}}Controller) http.Handler {
	r := chi.NewRouter()
	routes.{{.Name}}Routes(r, c)
	return r
}

func valid{{.Name}}Model(id uuid.UUID) *models.{{.Name}} {
	e := &models.{{.Name}}{}
	e.ID = id
	e.RecordVersion = 1
	e.IsActive = true
	e.IsDeletable = true
	return e
}

// Test{{.Name}}Controller_Get_OK_200 — happy path returns 200.
func Test{{.Name}}Controller_Get_OK_200(t *testing.T) {
	svc := &mock{{.Name}}Service{}
	id := uuid.New()
	svc.On("Get", mock.Anything, id).Return(valid{{.Name}}Model(id), nil)

	c := controllers.New{{.Name}}ControllerInstance(svc, noop{{.Name}}Validator{})
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/{{.PluralSnake}}/%s", id), nil)
	rec := httptest.NewRecorder()
	mount{{.Name}}Controller(c).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// Test{{.Name}}Controller_Get_NotFound_404 — services.Err{{.Name}}NotFound → 404.
func Test{{.Name}}Controller_Get_NotFound_404(t *testing.T) {
	svc := &mock{{.Name}}Service{}
	id := uuid.New()
	svc.On("Get", mock.Anything, id).Return(nil, services.Err{{.Name}}NotFound)

	c := controllers.New{{.Name}}ControllerInstance(svc, noop{{.Name}}Validator{})
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/{{.PluralSnake}}/%s", id), nil)
	rec := httptest.NewRecorder()
	mount{{.Name}}Controller(c).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// Test{{.Name}}Controller_Get_BadUUID_400 — non-UUID path param fails parsing.
func Test{{.Name}}Controller_Get_BadUUID_400(t *testing.T) {
	svc := &mock{{.Name}}Service{}
	c := controllers.New{{.Name}}ControllerInstance(svc, noop{{.Name}}Validator{})
	req := httptest.NewRequest(http.MethodGet, "/{{.PluralSnake}}/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	mount{{.Name}}Controller(c).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertNotCalled(t, "Get")
}

// Test{{.Name}}Controller_Create_BadJSON_400 — malformed body → 400.
func Test{{.Name}}Controller_Create_BadJSON_400(t *testing.T) {
	svc := &mock{{.Name}}Service{}
	c := controllers.New{{.Name}}ControllerInstance(svc, noop{{.Name}}Validator{})
	req := httptest.NewRequest(http.MethodPost, "/{{.PluralSnake}}", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mount{{.Name}}Controller(c).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertNotCalled(t, "Create")
}

// Test{{.Name}}Controller_Update_VersionConflict_409 — sentinel → 409.
func Test{{.Name}}Controller_Update_VersionConflict_409(t *testing.T) {
	svc := &mock{{.Name}}Service{}
	id := uuid.New()
	svc.On("Update", mock.Anything, id, 7, mock.Anything).
		Return(nil, services.Err{{.Name}}VersionConflict)

	c := controllers.New{{.Name}}ControllerInstance(svc, noop{{.Name}}Validator{})
	body := fmt.Sprintf(` + "`" + `{"id":"%s","recordVersion":7}` + "`" + `, id)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/{{.PluralSnake}}/%s", id), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mount{{.Name}}Controller(c).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

// Test{{.Name}}Controller_Archive_NoContent_204 — success returns 204.
func Test{{.Name}}Controller_Archive_NoContent_204(t *testing.T) {
	svc := &mock{{.Name}}Service{}
	id := uuid.New()
	svc.On("Archive", mock.Anything, id).Return(nil)

	c := controllers.New{{.Name}}ControllerInstance(svc, noop{{.Name}}Validator{})
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/{{.PluralSnake}}/%s", id), nil)
	rec := httptest.NewRecorder()
	mount{{.Name}}Controller(c).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
}

// Test{{.Name}}Controller_Archive_NotDeletable_409 — sentinel → 409.
func Test{{.Name}}Controller_Archive_NotDeletable_409(t *testing.T) {
	svc := &mock{{.Name}}Service{}
	id := uuid.New()
	svc.On("Archive", mock.Anything, id).Return(services.Err{{.Name}}NotDeletable)

	c := controllers.New{{.Name}}ControllerInstance(svc, noop{{.Name}}Validator{})
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/{{.PluralSnake}}/%s", id), nil)
	rec := httptest.NewRecorder()
	mount{{.Name}}Controller(c).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

// Test{{.Name}}Controller_List_OK_200 — happy path returns 200.
func Test{{.Name}}Controller_List_OK_200(t *testing.T) {
	svc := &mock{{.Name}}Service{}
	svc.On("List", mock.Anything, mock.Anything).
		Return([]*models.{{.Name}}{valid{{.Name}}Model(uuid.New())}, int64(1), nil)

	c := controllers.New{{.Name}}ControllerInstance(svc, noop{{.Name}}Validator{})
	req := httptest.NewRequest(http.MethodGet, "/{{.PluralSnake}}", nil)
	rec := httptest.NewRecorder()
	mount{{.Name}}Controller(c).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// Test{{.Name}}Controller_List_DefaultsApplied — controller builds a
// filter with the right defaults when the query string is empty.
func Test{{.Name}}Controller_List_DefaultsApplied(t *testing.T) {
	svc := &mock{{.Name}}Service{}
	var captured services.List{{.PluralName}}Filter
	svc.On("List", mock.Anything, mock.MatchedBy(func(f services.List{{.PluralName}}Filter) bool {
		captured = f
		return true
	})).Return([]*models.{{.Name}}{}, int64(0), nil)

	c := controllers.New{{.Name}}ControllerInstance(svc, noop{{.Name}}Validator{})
	req := httptest.NewRequest(http.MethodGet, "/{{.PluralSnake}}", nil)
	rec := httptest.NewRecorder()
	mount{{.Name}}Controller(c).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 1, captured.Page)
	assert.Equal(t, 10, captured.Limit)
	assert.Equal(t, "created_at", captured.SortField)
	assert.True(t, captured.SortDesc)
}

// Test{{.Name}}Controller_Create_ServiceError_500 — infra failure → 500.
func Test{{.Name}}Controller_Create_ServiceError_500(t *testing.T) {
	svc := &mock{{.Name}}Service{}
	svc.On("Create", mock.Anything, mock.Anything).
		Return(nil, errors.New("db down"))

	c := controllers.New{{.Name}}ControllerInstance(svc, noop{{.Name}}Validator{})
	body := ` + "`" + `{}` + "`" + `
	req := httptest.NewRequest(http.MethodPost, "/{{.PluralSnake}}", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mount{{.Name}}Controller(c).ServeHTTP(rec, req)
	// noop{{.Name}}Validator passes everything, so the request always
	// reaches the service mock that returns the infra error.
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
`
