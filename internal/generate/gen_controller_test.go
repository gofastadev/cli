package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenController_CreatesFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := GenController(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/rest/controllers/product.controller.go")
	assert.Contains(t, content, "Product")
}

func TestGenController_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	writeTestFile(t, "app/rest/controllers/product.controller.go", "original")

	err := GenController(d)
	require.NoError(t, err)
	assert.Equal(t, "original", readTestFile(t, "app/rest/controllers/product.controller.go"))
}
