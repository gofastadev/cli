package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenTask_CreatesFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := GenTask(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/tasks/product.task.go")
	assert.Contains(t, content, "ProductPayload")
	assert.Contains(t, content, "HandleProduct")
	assert.Contains(t, content, "package tasks")
}

func TestGenTask_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	writeTestFile(t, "app/tasks/product.task.go", "original")

	err := GenTask(d)
	require.NoError(t, err)
	assert.Equal(t, "original", readTestFile(t, "app/tasks/product.task.go"))
}
