package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInputs_ImportGating — the generated inputs file references field
// GoTypes directly, so uuid/time fields must pull their imports
// (regression: the template had no import block at all and a uuid: or
// time: field rendered an undefined identifier).
func TestInputs_ImportGating(t *testing.T) {
	t.Run("uuid and time fields pull their imports", func(t *testing.T) {
		out := renderTemplate(t, "inputs", Inputs, sampleData())
		assert.Contains(t, out, `"github.com/google/uuid"`)
		assert.Contains(t, out, `"time"`)
	})

	t.Run("plain fields emit no import block", func(t *testing.T) {
		data := sampleData()
		data.Fields = data.Fields[:2] // Name (string) + Price (float64)
		out := renderTemplate(t, "inputs", Inputs, data)
		assert.NotContains(t, out, "import")
	})
}
