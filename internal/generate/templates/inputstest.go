package templates

// InputsTest is the Go template for generating real Update*Patch +
// List*Filter tests. Pure-function tests — no mocks, no infra. The
// AsMap test uses any single-field patch (the first scaffolded
// resource field) to avoid template explosion; the negative-space
// assertion (only set fields appear) is more important than coverage
// of every field.
var InputsTest = `package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"{{.ModulePath}}/app/services"
)

// TestUpdate{{.Name}}Patch_AsMap_EmptyPatchIsEmptyMap — a patch with
// no fields set produces an empty map (which GORM treats as a no-op).
// This is the central correctness property of pointer-field patches.
func TestUpdate{{.Name}}Patch_AsMap_EmptyPatchIsEmptyMap(t *testing.T) {
	got := services.Update{{.Name}}Patch{}.AsMap()
	assert.Empty(t, got, "an empty patch must produce an empty UPDATE map")
}

// TestUpdate{{.Name}}Patch_AsMap_OnlyIncludesSetFields — set
// is_active and assert ONLY is_active appears in the SET clause.
// Critical: the columns the caller didn't touch must NOT appear,
// even with their zero values, otherwise GORM would write zeros.
func TestUpdate{{.Name}}Patch_AsMap_OnlyIncludesSetFields(t *testing.T) {
	active := false
	got := services.Update{{.Name}}Patch{IsActive: &active}.AsMap()

	assert.Equal(t, false, got["is_active"])
	assert.Len(t, got, 1, "only the one set field should appear")
{{- range .Fields}}
	assert.NotContains(t, got, "{{.SnakeName}}", "untouched field {{.SnakeName}} must not appear")
{{- end}}
	assert.NotContains(t, got, "is_deletable", "untouched field is_deletable must not appear")
}

// TestList{{.PluralName}}Filter_AsRepoFilter_EmptyMap — no filters → empty map.
func TestList{{.PluralName}}Filter_AsRepoFilter_EmptyMap(t *testing.T) {
	got := services.List{{.PluralName}}Filter{}.AsRepoFilter()
	assert.Empty(t, got)
}
`
