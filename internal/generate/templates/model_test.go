package templates

import (
	"html/template"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelTemplate_Content(t *testing.T) {
	data := sampleData()
	parsed, err := template.New("model").Funcs(funcMap).Parse(Model)
	require.NoError(t, err)
	var buf strings.Builder
	require.NoError(t, parsed.Execute(&buf, data))
	output := buf.String()
	assert.Contains(t, output, "package models")
	assert.Contains(t, output, "Product")
	assert.Contains(t, output, "Name")
	assert.Contains(t, output, "Price")
}
