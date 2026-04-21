package generate

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for internal helpers — scaffoldStepsWithoutRegeneration,
// jsonEncoder.WriteTo. Keeps scope narrow: the big integration
// suites (RunSteps end-to-end) live elsewhere; these tests just
// cover the small pure functions.
// ─────────────────────────────────────────────────────────────────────

// TestScaffoldStepsWithoutRegeneration_DropsRegenSteps — the helper
// filters out the "regenerate Wire" and "regenerate gqlgen" steps
// that can't run meaningfully in dry-run mode.
func TestScaffoldStepsWithoutRegeneration_DropsRegenSteps(t *testing.T) {
	full := scaffoldSteps(ScaffoldData{Name: "Product", IncludeGraphQL: true})
	slim := scaffoldStepsWithoutRegeneration(ScaffoldData{
		Name: "Product", IncludeGraphQL: true,
	})

	assert.Less(t, len(slim), len(full),
		"expected fewer steps after filtering")
	for _, s := range slim {
		assert.NotEqual(t, "regenerate Wire", s.Label)
		assert.NotEqual(t, "regenerate gqlgen", s.Label)
	}
}

// TestJSONEncoder_WriteTo — encodes a value as a single-line JSON
// document with no HTML escaping.
func TestJSONEncoder_WriteTo(t *testing.T) {
	var buf bytes.Buffer
	jsonEncoder{}.WriteTo(&buf, map[string]string{"key": "<value>"})
	out := buf.String()
	// Should NOT contain the HTML-escaped form of `<` / `>`.
	assert.Contains(t, out, "<value>")
	assert.NotContains(t, out, `\u003c`)
}

// TestJSONEncoder_WriteTo_NilValue — nil encodes to the JSON literal
// "null\n" without a write error.
func TestJSONEncoder_WriteTo_NilValue(t *testing.T) {
	var buf bytes.Buffer
	jsonEncoder{}.WriteTo(&buf, nil)
	assert.Equal(t, "null\n", buf.String())
}

// TestJSONEncoder_WriteTo_Array — round-trips a slice.
func TestJSONEncoder_WriteTo_Array(t *testing.T) {
	var buf bytes.Buffer
	jsonEncoder{}.WriteTo(&buf, []int{1, 2, 3})
	require.Contains(t, buf.String(), "[1,2,3]")
}
