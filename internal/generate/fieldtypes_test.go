package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupportedFieldTypes_OrderAndCompleteness(t *testing.T) {
	types := SupportedFieldTypes()
	keys := make([]string, len(types))
	for i, ft := range types {
		keys[i] = ft.Key
	}
	// Display order is part of the contract — generated docs tables render
	// rows in exactly this order.
	assert.Equal(t, []string{"string", "text", "int", "float", "bool", "uuid", "time"}, keys)

	for _, ft := range types {
		assert.NotEmpty(t, ft.GoType, "GoType for %s", ft.Key)
		assert.NotEmpty(t, ft.GormType, "GormType for %s", ft.Key)
		assert.NotEmpty(t, ft.GQLType, "GQLType for %s", ft.Key)
		assert.NotEmpty(t, ft.SQLTypePostgres, "SQLTypePostgres for %s", ft.Key)
		assert.NotEmpty(t, ft.SQLTypeMySQL, "SQLTypeMySQL for %s", ft.Key)
		assert.NotEmpty(t, ft.SQLTypeSQLite, "SQLTypeSQLite for %s", ft.Key)
		assert.NotEmpty(t, ft.SQLTypeSQLServer, "SQLTypeSQLServer for %s", ft.Key)
		assert.NotEmpty(t, ft.SQLTypeClickHouse, "SQLTypeClickHouse for %s", ft.Key)
	}
}

func TestSupportedFieldTypes_ReturnsCopy(t *testing.T) {
	first := SupportedFieldTypes()
	first[0].GoType = "mutated"
	second := SupportedFieldTypes()
	assert.Equal(t, "string", second[0].GoType)
}

func TestLookupFieldType(t *testing.T) {
	byKey, ok := lookupFieldType("uuid")
	require.True(t, ok)
	assert.Equal(t, "uuid.UUID", byKey.GoType)

	byAlias, ok := lookupFieldType("datetime")
	require.True(t, ok)
	assert.Equal(t, "time", byAlias.Key)
	assert.Equal(t, "time.Time", byAlias.GoType)

	_, ok = lookupFieldType("nope")
	assert.False(t, ok)
}

// TestFieldTypeRegistry_ParseFieldsParity locks the registry refactor to the
// pre-refactor ParseFields behavior: every registry entry, applied through
// ParseFields, must produce exactly the Field the old switch produced.
func TestFieldTypeRegistry_ParseFieldsParity(t *testing.T) {
	for _, ft := range SupportedFieldTypes() {
		names := append([]string{ft.Key}, ft.Aliases...)
		for _, name := range names {
			t.Run(name, func(t *testing.T) {
				fields := ParseFields([]string{"sample:" + name})
				require.Len(t, fields, 1)
				f := fields[0]
				assert.Equal(t, ft.GoType, f.GoType)
				assert.Equal(t, ft.GormType, f.GormType)
				assert.Equal(t, ft.GQLType, f.GQLType)
				assert.Equal(t, ft.SQLTypePostgres, f.SQLTypePostgres)
				assert.Equal(t, ft.SQLTypeMySQL, f.SQLTypeMySQL)
				assert.Equal(t, ft.SQLTypeSQLite, f.SQLTypeSQLite)
				assert.Equal(t, ft.SQLTypeSQLServer, f.SQLTypeSQLServer)
				assert.Equal(t, ft.SQLTypeClickHouse, f.SQLTypeClickHouse)
			})
		}
	}
}

// TestParseFields_UnknownFallsBackToString — unknown type names resolve to
// the registry's first (string) entry, preserving the old default branch.
func TestParseFields_UnknownFallsBackToString(t *testing.T) {
	fields := ParseFields([]string{"x:definitely-not-a-type"})
	require.Len(t, fields, 1)
	assert.Equal(t, `gorm:"not null"`, fields[0].GormType)
	assert.Equal(t, "VARCHAR(255) NOT NULL", fields[0].SQLTypePostgres)
}
