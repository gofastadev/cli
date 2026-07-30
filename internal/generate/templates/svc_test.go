package templates

import (
	"html"
	"html/template"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderTemplate parses + executes a template string with sampleData
// and returns the unescaped output (tests use html/template, which
// escapes quotes inside the rendered Go source).
func renderTemplate(t *testing.T, name, tmpl string, data testScaffoldData) string {
	t.Helper()
	parsed, err := template.New(name).Funcs(funcMap).Parse(tmpl)
	require.NoError(t, err)
	var buf strings.Builder
	require.NoError(t, parsed.Execute(&buf, data))
	return html.UnescapeString(buf.String())
}

func TestSvc_CreateMapsAllInputFields(t *testing.T) {
	out := renderTemplate(t, "svc", Svc, sampleData())

	for _, line := range []string{
		"Name: in.Name,",
		"Price: in.Price,",
		"Summary: in.Summary,",
		"OwnerID: in.OwnerID,",
		"ReleasedAt: in.ReleasedAt,",
	} {
		assert.Contains(t, out, line)
	}
	assert.NotContains(t, out, "TODO")
}

func TestSvc_CreateRendersValidBlockWithZeroFields(t *testing.T) {
	data := sampleData()
	data.Fields = nil
	out := renderTemplate(t, "svc", Svc, data)

	// The composite literal must degrade to an empty (still valid) body.
	assert.Contains(t, out, "entity := &models.Product{\n\t}")
}
