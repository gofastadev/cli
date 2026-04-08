package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenRoutes_CreatesFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := GenRoutes(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/rest/routes/product.routes.go")
	assert.Contains(t, content, "Product")
}

func TestGenRoutes_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	writeTestFile(t, "app/rest/routes/product.routes.go", "original")

	err := GenRoutes(d)
	require.NoError(t, err)
	assert.Equal(t, "original", readTestFile(t, "app/rest/routes/product.routes.go"))
}
