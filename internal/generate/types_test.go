package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestField_ZeroValue(t *testing.T) {
	var f Field
	assert.Empty(t, f.Name)
	assert.Empty(t, f.GoType)
	assert.Empty(t, f.GQLType)
	assert.Empty(t, f.SQLTypePostgres)
}

func TestScaffoldData_ZeroValue(t *testing.T) {
	var d ScaffoldData
	assert.Empty(t, d.Name)
	assert.Empty(t, d.Fields)
	assert.Empty(t, d.MigrationNum)
	assert.Empty(t, d.ModulePath)
	assert.False(t, d.IncludeController)
	assert.False(t, d.IncludeGraphQL)
}

func TestStep_ZeroValue(t *testing.T) {
	var s Step
	assert.Empty(t, s.Label)
	assert.Nil(t, s.Fn)
}

func TestField_SampleLiteral(t *testing.T) {
	cases := []struct {
		name string
		f    Field
		want string
	}{
		{"string", Field{SnakeName: "title", GoType: "string", GormType: `gorm:"not null"`}, `"sample-title"`},
		{"text", Field{SnakeName: "body", GoType: "string", GormType: `gorm:"type:text;not null"`}, `"sample text"`},
		{"int", Field{GoType: "int"}, "1"},
		{"float", Field{GoType: "float64"}, "1.5"},
		{"bool", Field{GoType: "bool"}, "true"},
		{"uuid", Field{GoType: "uuid.UUID"}, "uuid.New()"},
		{"time", Field{GoType: "time.Time"}, "time.Now().UTC()"},
		{"unknown", Field{GoType: "somepkg.Custom"}, `""`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.f.SampleLiteral())
		})
	}
}

func TestScaffoldData_HasUUIDField(t *testing.T) {
	withUUID := ScaffoldData{Fields: []Field{
		{Name: "OwnerID", GoType: "uuid.UUID"},
	}}
	assert.True(t, withUUID.HasUUIDField())

	withoutUUID := ScaffoldData{Fields: []Field{
		{Name: "Name", GoType: "string"},
	}}
	assert.False(t, withoutUUID.HasUUIDField())
}

func TestScaffoldData_HasTimeField(t *testing.T) {
	withTime := ScaffoldData{Fields: []Field{
		{Name: "Name", GoType: "string"},
		{Name: "CreatedAt", GoType: "time.Time"},
	}}
	assert.True(t, withTime.HasTimeField())

	withoutTime := ScaffoldData{Fields: []Field{
		{Name: "Name", GoType: "string"},
		{Name: "Count", GoType: "int"},
	}}
	assert.False(t, withoutTime.HasTimeField())
}

func TestField_SampleJSON(t *testing.T) {
	cases := []struct {
		name string
		f    Field
		want string
	}{
		{"string", Field{SnakeName: "title", GoType: "string", GormType: `gorm:"not null"`}, `"sample-title"`},
		{"text", Field{SnakeName: "body", GoType: "string", GormType: `gorm:"type:text;not null"`}, `"sample text"`},
		{"int", Field{GoType: "int"}, "1"},
		{"float", Field{GoType: "float64"}, "1.5"},
		{"bool", Field{GoType: "bool"}, "true"},
		{"uuid", Field{GoType: "uuid.UUID"}, `"123e4567-e89b-12d3-a456-426614174000"`},
		{"time", Field{GoType: "time.Time"}, `"2026-01-15T12:00:00Z"`},
		{"unknown", Field{GoType: "somepkg.Custom"}, `""`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.f.SampleJSON())
		})
	}
}
