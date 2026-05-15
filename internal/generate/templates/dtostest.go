package templates

// DTOsTest is the Go template for generating real DTO mapper tests.
// Pure-function tests — no mocks, no fixtures, no skips. Cover
// FromModel (incl. deletedAt nil-safety), the slice form, and the
// ToFilter defaults so every scaffolded resource gets executable
// behavior contracts the moment it lands.
var DTOsTest = `package dtos_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"{{.ModulePath}}/app/dtos"
	"{{.ModulePath}}/app/models"
)

// Test{{.Name}}FromModel_OmitsDeletedAtForLiveRows pins the soft-
// delete nil-safety: a row whose gorm.DeletedAt has Valid==false (not
// soft-deleted) must produce a DTO with DeletedAt == nil so the
// ` + "`omitempty`" + ` JSON tag elides the field instead of rendering
// "0001-01-01T00:00:00Z".
func Test{{.Name}}FromModel_OmitsDeletedAtForLiveRows(t *testing.T) {
	id := uuid.New()
	now := time.Now()
	m := &models.{{.Name}}{}
	m.ID = id
	m.RecordVersion = 3
	m.CreatedAt = now
	m.UpdatedAt = now
	m.IsActive = true
	m.IsDeletable = true

	dto := dtos.{{.Name}}FromModel(m)
	require.NotNil(t, dto)
	assert.Equal(t, id, dto.ID)
	assert.Equal(t, 3, dto.RecordVersion)
	assert.True(t, dto.IsActive)
	assert.Nil(t, dto.DeletedAt, "live rows must not have a non-nil DeletedAt pointer in the DTO")
}

// Test{{.Name}}FromModel_SurfaceDeletedAtForArchivedRows — when the
// row HAS been soft-deleted (Valid==true), the DTO carries the
// timestamp.
func Test{{.Name}}FromModel_SurfaceDeletedAtForArchivedRows(t *testing.T) {
	when := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)
	m := &models.{{.Name}}{}
	m.ID = uuid.New()
	m.DeletedAt = gorm.DeletedAt{Time: when, Valid: true}

	dto := dtos.{{.Name}}FromModel(m)
	require.NotNil(t, dto.DeletedAt)
	assert.True(t, dto.DeletedAt.Equal(when))
}

// Test{{.PluralName}}FromModels_PreservesOrder — slice helper maps
// each model in input order without mutation.
func Test{{.PluralName}}FromModels_PreservesOrder(t *testing.T) {
	a := &models.{{.Name}}{}
	a.ID = uuid.New()
	b := &models.{{.Name}}{}
	b.ID = uuid.New()

	out := dtos.{{.PluralName}}FromModels([]*models.{{.Name}}{a, b})
	require.Len(t, out, 2)
	assert.Equal(t, a.ID, out[0].ID)
	assert.Equal(t, b.ID, out[1].ID)
}

// TestT{{.Name}}FiltersQueryParamsDto_ToFilter_AppliesDefaults — empty
// query → page=1, limit=10, SortField=created_at, SortDesc=true.
func TestT{{.Name}}FiltersQueryParamsDto_ToFilter_AppliesDefaults(t *testing.T) {
	q := dtos.T{{.Name}}FiltersQueryParamsDto{}
	f := q.ToFilter()
	assert.Equal(t, 1, f.Page)
	assert.Equal(t, 10, f.Limit)
	assert.Equal(t, "created_at", f.SortField)
	assert.True(t, f.SortDesc)
}

// TestT{{.Name}}FiltersQueryParamsDto_ToFilter_HonorsExplicitValues —
// when the caller specifies values, they override defaults.
func TestT{{.Name}}FiltersQueryParamsDto_ToFilter_HonorsExplicitValues(t *testing.T) {
	page, limit := 3, 25
	field := "id"
	asc := dtos.SortOrientationAsc
	q := dtos.T{{.Name}}FiltersQueryParamsDto{
		Page:            &page,
		Limit:           &limit,
		SortByField:     &field,
		SortOrientation: &asc,
	}
	f := q.ToFilter()
	assert.Equal(t, 3, f.Page)
	assert.Equal(t, 25, f.Limit)
	assert.Equal(t, "id", f.SortField)
	assert.False(t, f.SortDesc)
}

// TestT{{.Name}}FiltersQueryParamsDto_ToFilter_RejectsInvalidOrientation —
// an unmarshaled string that isn't ASC/DESC must fall back to default DESC.
func TestT{{.Name}}FiltersQueryParamsDto_ToFilter_RejectsInvalidOrientation(t *testing.T) {
	bogus := dtos.SortOrientation("DROP TABLE")
	q := dtos.T{{.Name}}FiltersQueryParamsDto{SortOrientation: &bogus}
	f := q.ToFilter()
	assert.True(t, f.SortDesc)
}
`
