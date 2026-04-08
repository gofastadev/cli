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
