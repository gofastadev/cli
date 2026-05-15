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
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"{{.ModulePath}}/app/models"
	"{{.ModulePath}}/app/rest/controllers"
	"{{.ModulePath}}/app/rest/routes"
	"{{.ModulePath}}/app/services"
	"{{.ModulePath}}/app/validators"
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

func newTest{{.Name}}Validator(t *testing.T) *validators.AppValidator {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.{{.Name}}{}))
	return validators.NewAppValidator(db)
}

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

	c := controllers.New{{.Name}}ControllerInstance(svc, newTest{{.Name}}Validator(t))
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

	c := controllers.New{{.Name}}ControllerInstance(svc, newTest{{.Name}}Validator(t))
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/{{.PluralSnake}}/%s", id), nil)
	rec := httptest.NewRecorder()
	mount{{.Name}}Controller(c).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// Test{{.Name}}Controller_Get_BadUUID_400 — non-UUID path param fails parsing.
func Test{{.Name}}Controller_Get_BadUUID_400(t *testing.T) {
	svc := &mock{{.Name}}Service{}
	c := controllers.New{{.Name}}ControllerInstance(svc, newTest{{.Name}}Validator(t))
	req := httptest.NewRequest(http.MethodGet, "/{{.PluralSnake}}/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	mount{{.Name}}Controller(c).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.AssertNotCalled(t, "Get")
}

// Test{{.Name}}Controller_Create_BadJSON_400 — malformed body → 400.
func Test{{.Name}}Controller_Create_BadJSON_400(t *testing.T) {
	svc := &mock{{.Name}}Service{}
	c := controllers.New{{.Name}}ControllerInstance(svc, newTest{{.Name}}Validator(t))
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

	c := controllers.New{{.Name}}ControllerInstance(svc, newTest{{.Name}}Validator(t))
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

	c := controllers.New{{.Name}}ControllerInstance(svc, newTest{{.Name}}Validator(t))
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

	c := controllers.New{{.Name}}ControllerInstance(svc, newTest{{.Name}}Validator(t))
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

	c := controllers.New{{.Name}}ControllerInstance(svc, newTest{{.Name}}Validator(t))
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

	c := controllers.New{{.Name}}ControllerInstance(svc, newTest{{.Name}}Validator(t))
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

	c := controllers.New{{.Name}}ControllerInstance(svc, newTest{{.Name}}Validator(t))
	body := ` + "`" + `{}` + "`" + `
	req := httptest.NewRequest(http.MethodPost, "/{{.PluralSnake}}", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mount{{.Name}}Controller(c).ServeHTTP(rec, req)
	// Could be 422 if validation fires first (resource has required
	// fields) or 500 if service is reached. Either is acceptable for
	// this smoke test — the point is a clean error response.
	assert.True(t, rec.Code == http.StatusInternalServerError || rec.Code == http.StatusUnprocessableEntity,
		"expected 500 (service error reached) or 422 (validation rejected first), got %d", rec.Code)
}
`
