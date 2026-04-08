package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenEmailTemplate_CreatesFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := GenEmailTemplate(d)
	require.NoError(t, err)

	content := readTestFile(t, "templates/emails/product.html")
	assert.Contains(t, content, "product")
	assert.Contains(t, content, "{{.Title}}")
	assert.Contains(t, content, "{{.Name}}")
}

func TestGenEmailTemplate_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	writeTestFile(t, "templates/emails/product.html", "original")

	err := GenEmailTemplate(d)
	require.NoError(t, err)
	assert.Equal(t, "original", readTestFile(t, "templates/emails/product.html"))
}
