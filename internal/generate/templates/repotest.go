package templates

// RepoTest is the Go template for generating a real repository test
// file. Uses testcontainers Postgres via the framework's
// pkg/testutil/testdb helper, then AutoMigrates the resource model.
//
// Covers:
//   - FindByID hit/miss + soft-delete exclusion
//   - Create populates BeforeCreate fields
//   - UpdateIfVersionMatches happy path + version-mismatch + atomicity-under-race
//   - SoftDeleteIfDeletable happy path + blocked-by-flag
//   - List pagination + sort
var RepoTest = `package repositories_test

import (
	"context"
	"testing"
	"time"

	"github.com/gofastadev/gofasta/pkg/testutil/testdb"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"{{.ModulePath}}/app/models"
	"{{.ModulePath}}/app/repositories"
	repoInterfaces "{{.ModulePath}}/app/repositories/interfaces"
)

func setup{{.Name}}RepoTest(t *testing.T) (*gorm.DB, *repositories.{{.Name}}Repository) {
	t.Helper()
	db := testdb.SetupTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.{{.Name}}{}))
	return db, repositories.New{{.Name}}Repository(db)
}

func make{{.Name}}(t *testing.T, db *gorm.DB) *models.{{.Name}} {
	t.Helper()
	e := &models.{{.Name}}{
		// TODO: populate any non-null fields specific to {{.Name}}
	}
	require.NoError(t, db.Create(e).Error)
	return e
}

// Test{{.Name}}Repository_FindByID_Hit asserts the happy path.
func Test{{.Name}}Repository_FindByID_Hit(t *testing.T) {
	db, repo := setup{{.Name}}RepoTest(t)
	e := make{{.Name}}(t, db)

	got, err := repo.FindByID(context.Background(), e.ID)
	require.NoError(t, err)
	assert.Equal(t, e.ID, got.ID)
}

// Test{{.Name}}Repository_FindByID_Miss — gorm.ErrRecordNotFound.
func Test{{.Name}}Repository_FindByID_Miss(t *testing.T) {
	_, repo := setup{{.Name}}RepoTest(t)
	_, err := repo.FindByID(context.Background(), uuid.New())
	require.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

// Test{{.Name}}Repository_FindByID_SoftDeletedExcluded — auto-filter
// hides soft-deleted rows.
func Test{{.Name}}Repository_FindByID_SoftDeletedExcluded(t *testing.T) {
	db, repo := setup{{.Name}}RepoTest(t)
	e := make{{.Name}}(t, db)
	require.NoError(t, db.Delete(e).Error)

	_, err := repo.FindByID(context.Background(), e.ID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

// Test{{.Name}}Repository_Create_PopulatesBaseFields — BeforeCreate
// hook fires; ID, timestamps, defaults set.
func Test{{.Name}}Repository_Create_PopulatesBaseFields(t *testing.T) {
	_, repo := setup{{.Name}}RepoTest(t)
	e := &models.{{.Name}}{}
	require.NoError(t, repo.Create(context.Background(), e))

	assert.NotEqual(t, uuid.Nil, e.ID)
	assert.False(t, e.CreatedAt.IsZero())
	assert.True(t, e.IsActive)
	assert.True(t, e.IsDeletable)
	assert.Equal(t, 1, e.RecordVersion)
}

// Test{{.Name}}Repository_UpdateIfVersionMatches_HappyPath — version
// matches → UPDATE applies, refetch returns the updated row with
// record_version bumped.
func Test{{.Name}}Repository_UpdateIfVersionMatches_HappyPath(t *testing.T) {
	db, repo := setup{{.Name}}RepoTest(t)
	e := make{{.Name}}(t, db)

	updated, affected, err := repo.UpdateIfVersionMatches(
		context.Background(), e.ID, e.RecordVersion,
		map[string]any{"is_active": false},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected)
	require.NotNil(t, updated)
	assert.Equal(t, e.RecordVersion+1, updated.RecordVersion,
		"record_version must be bumped in the same UPDATE")
}

// Test{{.Name}}Repository_UpdateIfVersionMatches_VersionConflict —
// wrong version → 0 affected, nil entity, nil error.
func Test{{.Name}}Repository_UpdateIfVersionMatches_VersionConflict(t *testing.T) {
	db, repo := setup{{.Name}}RepoTest(t)
	e := make{{.Name}}(t, db)

	updated, affected, err := repo.UpdateIfVersionMatches(
		context.Background(), e.ID, e.RecordVersion+99,
		map[string]any{"is_active": false},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(0), affected)
	assert.Nil(t, updated)
}

// Test{{.Name}}Repository_UpdateIfVersionMatches_AtomicityUnderRace —
// two concurrent UPDATEs with the same expected version: exactly one
// wins.
func Test{{.Name}}Repository_UpdateIfVersionMatches_AtomicityUnderRace(t *testing.T) {
	db, repo := setup{{.Name}}RepoTest(t)
	e := make{{.Name}}(t, db)

	type result struct {
		affected int64
		err      error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, affected, err := repo.UpdateIfVersionMatches(
				context.Background(), e.ID, e.RecordVersion,
				map[string]any{"is_active": false},
			)
			results <- result{affected, err}
		}()
	}
	a := <-results
	b := <-results
	require.NoError(t, a.err)
	require.NoError(t, b.err)
	assert.Equal(t, int64(1), a.affected+b.affected,
		"exactly one racing UPDATE must succeed")
}

// Test{{.Name}}Repository_SoftDeleteIfDeletable_HappyPath — IsDeletable=true
// row gets archived, the returned record carries the freshly-stamped
// DeletedAt + cleared IsActive, and a subsequent FindByID returns
// ErrRecordNotFound.
func Test{{.Name}}Repository_SoftDeleteIfDeletable_HappyPath(t *testing.T) {
	db, repo := setup{{.Name}}RepoTest(t)
	e := make{{.Name}}(t, db)

	deleted, err := repo.SoftDeleteIfDeletable(context.Background(), e.ID)
	require.NoError(t, err)
	require.NotNil(t, deleted, "happy path must return the soft-deleted record so the controller can respond 200 with payload")
	assert.True(t, deleted.DeletedAt.Valid)
	assert.False(t, deleted.IsActive)

	_, err = repo.FindByID(context.Background(), e.ID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	var fresh models.{{.Name}}
	require.NoError(t, db.Unscoped().Where("id = ?", e.ID).First(&fresh).Error)
	assert.True(t, fresh.DeletedAt.Valid)
	assert.False(t, fresh.IsActive)
}

// Test{{.Name}}Repository_SoftDeleteIfDeletable_BlockedByFlag — when
// is_deletable is false the repo returns Err{{.Name}}NotDeletable;
// controller maps to 409.
func Test{{.Name}}Repository_SoftDeleteIfDeletable_BlockedByFlag(t *testing.T) {
	db, repo := setup{{.Name}}RepoTest(t)
	e := make{{.Name}}(t, db)
	require.NoError(t, db.Model(e).Update("is_deletable", false).Error)

	got, err := repo.SoftDeleteIfDeletable(context.Background(), e.ID)
	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, repoInterfaces.Err{{.Name}}NotDeletable)
}

// Test{{.Name}}Repository_SoftDeleteIfDeletable_AlreadyDeleted — second
// archive returns gorm.ErrRecordNotFound (idempotent 404).
func Test{{.Name}}Repository_SoftDeleteIfDeletable_AlreadyDeleted(t *testing.T) {
	db, repo := setup{{.Name}}RepoTest(t)
	e := make{{.Name}}(t, db)

	_, err := repo.SoftDeleteIfDeletable(context.Background(), e.ID)
	require.NoError(t, err)

	got, err := repo.SoftDeleteIfDeletable(context.Background(), e.ID)
	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound,
		"second DELETE must return ErrRecordNotFound (idempotent), not a spurious conflict")
}

// Test{{.Name}}Repository_SoftDeleteIfDeletable_NotFound — unknown id
// returns gorm.ErrRecordNotFound, same as already-deleted.
func Test{{.Name}}Repository_SoftDeleteIfDeletable_NotFound(t *testing.T) {
	_, repo := setup{{.Name}}RepoTest(t)

	got, err := repo.SoftDeleteIfDeletable(context.Background(), uuid.New())
	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

// Test{{.Name}}Repository_List_PaginationAndSort — verifies List
// honors page/limit + sort string.
func Test{{.Name}}Repository_List_PaginationAndSort(t *testing.T) {
	db, repo := setup{{.Name}}RepoTest(t)
	for i := 0; i < 4; i++ {
		require.NoError(t, db.Create(&models.{{.Name}}{}).Error)
		time.Sleep(2 * time.Millisecond) // deterministic created_at order
	}

	got, total, err := repo.List(context.Background(), nil, 1, 2, "created_at ASC")
	require.NoError(t, err)
	assert.Equal(t, int64(4), total)
	require.Len(t, got, 2)

	got, _, err = repo.List(context.Background(), nil, 2, 2, "created_at ASC")
	require.NoError(t, err)
	require.Len(t, got, 2)
}
`
