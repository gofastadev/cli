package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ─────────────────────────────────────────────────────────────────────
// Coverage for internal helpers — scaffoldStepsWithoutRegeneration.
// Keeps scope narrow: the big integration suites (RunSteps end-to-end)
// live elsewhere; these tests just cover the small pure functions.
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
