package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenModel_CreatesCorrectFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := GenModel(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/models/product.model.go")
	assert.Contains(t, content, "Product")
}

func TestGenModel_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	writeTestFile(t, "app/models/product.model.go", "original")

	err := GenModel(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/models/product.model.go")
	assert.Equal(t, "original", content)
}
