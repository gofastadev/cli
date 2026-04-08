package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenRepoInterface_CreatesFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := GenRepoInterface(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/repositories/interfaces/product_repository.go")
	assert.Contains(t, content, "Product")
}

func TestGenRepoInterface_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	writeTestFile(t, "app/repositories/interfaces/product_repository.go", "original")

	err := GenRepoInterface(d)
	require.NoError(t, err)
	assert.Equal(t, "original", readTestFile(t, "app/repositories/interfaces/product_repository.go"))
}

func TestGenRepo_CreatesFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := GenRepo(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/repositories/product.repository.go")
	assert.Contains(t, content, "Product")
}

func TestGenRepo_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	writeTestFile(t, "app/repositories/product.repository.go", "original")

	err := GenRepo(d)
	require.NoError(t, err)
	assert.Equal(t, "original", readTestFile(t, "app/repositories/product.repository.go"))
}
