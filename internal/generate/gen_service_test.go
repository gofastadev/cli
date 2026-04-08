package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenSvcInterface_CreatesFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := GenSvcInterface(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/services/interfaces/product_service.go")
	assert.Contains(t, content, "Product")
}

func TestGenSvcInterface_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	writeTestFile(t, "app/services/interfaces/product_service.go", "original")

	err := GenSvcInterface(d)
	require.NoError(t, err)
	assert.Equal(t, "original", readTestFile(t, "app/services/interfaces/product_service.go"))
}

func TestGenSvc_CreatesFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := GenSvc(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/services/product.service.go")
	assert.Contains(t, content, "Product")
}

func TestGenSvc_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	writeTestFile(t, "app/services/product.service.go", "original")

	err := GenSvc(d)
	require.NoError(t, err)
	assert.Equal(t, "original", readTestFile(t, "app/services/product.service.go"))
}
