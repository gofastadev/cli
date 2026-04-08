package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenWireProvider_CreatesFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := GenWireProvider(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/di/providers/product.go")
	assert.Contains(t, content, "Product")
}

func TestGenWireProvider_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	writeTestFile(t, "app/di/providers/product.go", "original")

	err := GenWireProvider(d)
	require.NoError(t, err)
	assert.Equal(t, "original", readTestFile(t, "app/di/providers/product.go"))
}
