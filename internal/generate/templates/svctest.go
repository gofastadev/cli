package templates

// SvcTest is the Go template for generating a real service test
// file. Uses testify/mock for the repository — the mock is defined
// inline (no separate testutil/mocks file per scaffolded resource)
// so the generated test stands alone with no extra moving parts.
//
// Covers:
//   - Get: happy path, gorm.ErrRecordNotFound → ErrXNotFound, infra wrap
//   - Update: happy path, RowsAffected==0 → ErrXVersionConflict, infra wrap
//   - Archive: happy path, RowsAffected==0 → ErrXNotDeletable, infra wrap
var SvcTest = `package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"{{.ModulePath}}/app/models"
	repoInterfaces "{{.ModulePath}}/app/repositories/interfaces"
	"{{.ModulePath}}/app/services"
)

// mock{{.Name}}Repository is an inline testify mock for the repo.
// Defined here (not in testutil/mocks) so scaffolded resources don't
// pile mock files into a shared dir; the service-test scope is
// limited and each test file owns its mock.
type mock{{.Name}}Repository struct {
	mock.Mock
}

func (m *mock{{.Name}}Repository) List(ctx context.Context, filter map[string]any, page, limit int, sort string) ([]*models.{{.Name}}, int64, error) {
	args := m.Called(ctx, filter, page, limit, sort)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]*models.{{.Name}}), args.Get(1).(int64), args.Error(2)
}
func (m *mock{{.Name}}Repository) FindByID(ctx context.Context, id uuid.UUID) (*models.{{.Name}}, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.{{.Name}}), args.Error(1)
}
func (m *mock{{.Name}}Repository) Create(ctx context.Context, e *models.{{.Name}}) error {
	args := m.Called(ctx, e)
	return args.Error(0)
}
func (m *mock{{.Name}}Repository) UpdateIfVersionMatches(ctx context.Context, id uuid.UUID, expectedVersion int, fields map[string]any) (*models.{{.Name}}, int64, error) {
	args := m.Called(ctx, id, expectedVersion, fields)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).(*models.{{.Name}}), args.Get(1).(int64), args.Error(2)
}
func (m *mock{{.Name}}Repository) SoftDeleteIfDeletable(ctx context.Context, id uuid.UUID) (*models.{{.Name}}, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.{{.Name}}), args.Error(1)
}

// Test{{.Name}}Service_Get covers the three branches of Get:
// happy path, ErrRecordNotFound → Err{{.Name}}NotFound, infra wrap.
func Test{{.Name}}Service_Get(t *testing.T) {
	id := uuid.New()

	t.Run("happy path returns the model", func(t *testing.T) {
		repo := &mock{{.Name}}Repository{}
		svc := services.New{{.Name}}Service(repo)
		expected := &models.{{.Name}}{}
		expected.ID = id
		repo.On("FindByID", mock.Anything, id).Return(expected, nil)
		got, err := svc.Get(context.Background(), id)
		require.NoError(t, err)
		assert.Same(t, expected, got)
	})

	t.Run("gorm.ErrRecordNotFound becomes Err{{.Name}}NotFound", func(t *testing.T) {
		repo := &mock{{.Name}}Repository{}
		svc := services.New{{.Name}}Service(repo)
		repo.On("FindByID", mock.Anything, id).Return(nil, gorm.ErrRecordNotFound)
		_, err := svc.Get(context.Background(), id)
		require.Error(t, err)
		assert.True(t, errors.Is(err, services.Err{{.Name}}NotFound))
	})

	t.Run("infrastructure error is wrapped", func(t *testing.T) {
		repo := &mock{{.Name}}Repository{}
		svc := services.New{{.Name}}Service(repo)
		repo.On("FindByID", mock.Anything, id).Return(nil, errors.New("connection refused"))
		_, err := svc.Get(context.Background(), id)
		require.Error(t, err)
		assert.False(t, errors.Is(err, services.Err{{.Name}}NotFound))
		assert.Contains(t, err.Error(), "{{.Name}}Service.Get")
	})
}

// Test{{.Name}}Service_Update covers the version-conflict branch.
func Test{{.Name}}Service_Update(t *testing.T) {
	id := uuid.New()
	patch := services.Update{{.Name}}Patch{}

	t.Run("happy path returns the refetched model", func(t *testing.T) {
		repo := &mock{{.Name}}Repository{}
		svc := services.New{{.Name}}Service(repo)
		expected := &models.{{.Name}}{}
		expected.ID = id
		repo.On("UpdateIfVersionMatches", mock.Anything, id, 3, mock.Anything).
			Return(expected, int64(1), nil)
		got, err := svc.Update(context.Background(), id, 3, patch)
		require.NoError(t, err)
		assert.Same(t, expected, got)
	})

	t.Run("RowsAffected==0 becomes Err{{.Name}}VersionConflict", func(t *testing.T) {
		repo := &mock{{.Name}}Repository{}
		svc := services.New{{.Name}}Service(repo)
		repo.On("UpdateIfVersionMatches", mock.Anything, id, 3, mock.Anything).
			Return(nil, int64(0), nil)
		_, err := svc.Update(context.Background(), id, 3, patch)
		require.Error(t, err)
		assert.True(t, errors.Is(err, services.Err{{.Name}}VersionConflict))
	})

	t.Run("infrastructure error is wrapped", func(t *testing.T) {
		repo := &mock{{.Name}}Repository{}
		svc := services.New{{.Name}}Service(repo)
		repo.On("UpdateIfVersionMatches", mock.Anything, id, 3, mock.Anything).
			Return(nil, int64(0), errors.New("deadlock"))
		_, err := svc.Update(context.Background(), id, 3, patch)
		require.Error(t, err)
		assert.False(t, errors.Is(err, services.Err{{.Name}}VersionConflict))
	})
}

// Test{{.Name}}Service_Archive covers the three-way classification the
// repo surfaces and the service translates to domain sentinels.
func Test{{.Name}}Service_Archive(t *testing.T) {
	id := uuid.New()

	t.Run("happy path returns the soft-deleted record", func(t *testing.T) {
		repo := &mock{{.Name}}Repository{}
		svc := services.New{{.Name}}Service(repo)
		deleted := &models.{{.Name}}{}
		deleted.ID = id
		deleted.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true}
		deleted.IsActive = false
		repo.On("SoftDeleteIfDeletable", mock.Anything, id).Return(deleted, nil)

		got, err := svc.Archive(context.Background(), id)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.True(t, got.DeletedAt.Valid)
		assert.False(t, got.IsActive)
	})

	t.Run("gorm.ErrRecordNotFound becomes Err{{.Name}}NotFound", func(t *testing.T) {
		repo := &mock{{.Name}}Repository{}
		svc := services.New{{.Name}}Service(repo)
		repo.On("SoftDeleteIfDeletable", mock.Anything, id).Return(nil, gorm.ErrRecordNotFound)

		_, err := svc.Archive(context.Background(), id)
		require.Error(t, err)
		assert.True(t, errors.Is(err, services.Err{{.Name}}NotFound),
			"missing OR already deleted must translate to Err{{.Name}}NotFound (404), not 409")
	})

	t.Run("repoInterfaces.Err{{.Name}}NotDeletable becomes Err{{.Name}}NotDeletable", func(t *testing.T) {
		repo := &mock{{.Name}}Repository{}
		svc := services.New{{.Name}}Service(repo)
		repo.On("SoftDeleteIfDeletable", mock.Anything, id).Return(nil, repoInterfaces.Err{{.Name}}NotDeletable)

		_, err := svc.Archive(context.Background(), id)
		require.Error(t, err)
		assert.True(t, errors.Is(err, services.Err{{.Name}}NotDeletable))
	})
}
`
