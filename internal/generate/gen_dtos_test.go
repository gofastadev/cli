package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenDTOs_CreatesCorrectFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := GenDTOs(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/dtos/product.dtos.go")
	assert.Contains(t, content, "Product")
}

func TestGenDTOs_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	writeTestFile(t, "app/dtos/product.dtos.go", "original")

	err := GenDTOs(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/dtos/product.dtos.go")
	assert.Equal(t, "original", content)
}
