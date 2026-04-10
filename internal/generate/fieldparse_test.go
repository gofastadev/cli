package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFields_StringType(t *testing.T) {
	fields := ParseFields([]string{"name:string"})
	require.Len(t, fields, 1)
	f := fields[0]
	assert.Equal(t, "Name", f.Name)
	assert.Equal(t, "name", f.JSONName)
	assert.Equal(t, "name", f.SnakeName)
	assert.Equal(t, "string", f.GoType)
	assert.Equal(t, "String", f.GQLType)
	assert.Equal(t, "VARCHAR(255) NOT NULL", f.SQLTypePostgres)
	assert.Equal(t, "VARCHAR(255) NOT NULL", f.SQLTypeMySQL)
	assert.Equal(t, "TEXT NOT NULL", f.SQLTypeSQLite)
	assert.Equal(t, "NVARCHAR(255) NOT NULL", f.SQLTypeSQLServer)
	assert.Equal(t, "String", f.SQLTypeClickHouse)
}

func TestParseFields_AllTypes(t *testing.T) {
	tests := []struct {
		typeStr string
		goType  string
		gqlType string
	}{
		{"string", "string", "String"},
		{"text", "string", "String"},
		{"int", "int", "Int"},
		{"float", "float64", "Float"},
		{"bool", "bool", "Boolean"},
		{"uuid", "uuid.UUID", "ID"},
		{"time", "time.Time", "DateTime"},
		{"datetime", "time.Time", "DateTime"},
	}
	for _, tc := range tests {
		t.Run(tc.typeStr, func(t *testing.T) {
			fields := ParseFields([]string{"field:" + tc.typeStr})
			require.Len(t, fields, 1)
			assert.Equal(t, tc.goType, fields[0].GoType)
			assert.Equal(t, tc.gqlType, fields[0].GQLType)
		})
	}
}

func TestParseFields_DefaultType(t *testing.T) {
	fields := ParseFields([]string{"name:unknown"})
	require.Len(t, fields, 1)
	assert.Equal(t, "string", fields[0].GoType)
	assert.Equal(t, "String", fields[0].GQLType)
}

func TestParseFields_InvalidFormat(t *testing.T) {
	fields := ParseFields([]string{"nocolon"})
	assert.Empty(t, fields)
}

func TestParseFields_MultipleFields(t *testing.T) {
	fields := ParseFields([]string{"name:string", "price:float"})
	require.Len(t, fields, 2)
	assert.Equal(t, "Name", fields[0].Name)
	assert.Equal(t, "string", fields[0].GoType)
	assert.Equal(t, "Price", fields[1].Name)
	assert.Equal(t, "float64", fields[1].GoType)
}

func TestParseFields_Empty(t *testing.T) {
	fields := ParseFields([]string{})
	assert.Nil(t, fields)
}

func TestParseFields_CaseConversion(t *testing.T) {
	fields := ParseFields([]string{"product_name:string"})
	require.Len(t, fields, 1)
	assert.Equal(t, "ProductName", fields[0].Name)
	assert.Equal(t, "productName", fields[0].JSONName)
	assert.Equal(t, "product_name", fields[0].SnakeName)
}
