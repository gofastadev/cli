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

// TestModelTemplate_ImportGating — the time/uuid imports must appear
// exactly when a field of that type exists (regression: a uuid: field
// used to render `uuid.UUID` with no import → undefined: uuid).
func TestModelTemplate_ImportGating(t *testing.T) {
	t.Run("uuid and time fields pull their imports", func(t *testing.T) {
		out := renderTemplate(t, "model", Model, sampleData())
		assert.Contains(t, out, `"github.com/google/uuid"`)
		assert.Contains(t, out, `"time"`)
		assert.Contains(t, out, `"github.com/gofastadev/gofasta/pkg/models"`)
	})

	t.Run("plain fields keep the single-line import", func(t *testing.T) {
		data := sampleData()
		data.Fields = data.Fields[:2] // Name (string) + Price (float64)
		out := renderTemplate(t, "model", Model, data)
		assert.Contains(t, out, `import "github.com/gofastadev/gofasta/pkg/models"`)
		assert.NotContains(t, out, `"github.com/google/uuid"`)
		assert.NotContains(t, out, `"time"`)
	})
}
