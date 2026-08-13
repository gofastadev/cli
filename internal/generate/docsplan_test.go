package generate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofastadev/cli/internal/layout"
)

// planFilesPerStep maps every file-producing scaffoldSteps label to the
// number of files it writes. The parity test below walks the REAL step
// chain against this map, so adding/removing/renaming a step without
// updating ScaffoldPlan (and this map) fails the build — that lockstep is
// what lets `gofasta facts` publish scaffold file lists truthfully.
var planFilesPerStep = map[string]int{
	"model":                1,
	"migration":            2, // up + down pair
	"repository interface": 1,
	"repository":           1,
	"repository test":      1,
	"sentinel errors":      1,
	"domain inputs":        1,
	"domain inputs test":   1,
	"service interface":    1,
	"service":              1,
	"service test":         1,
	"DTOs":                 1,
	"DTOs test":            1,
	"Wire provider":        1,
	"controller":           1,
	"controller test":      1,
	"routes":               1,
	"GraphQL schema":       1,
	"GraphQL resolvers":    1,
}

func TestScaffoldPlan_MatchesScaffoldSteps(t *testing.T) {
	for _, tc := range []struct {
		name    string
		graphql bool
	}{
		{"rest-only", false},
		{"with-graphql", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := scaffoldSteps(ScaffoldData{IncludeGraphQL: tc.graphql})

			wantCreated := 0
			wantPatched := 0
			for _, s := range steps {
				switch {
				case strings.HasPrefix(s.Label, "auto-wire: "):
					wantPatched++
				case strings.HasPrefix(s.Label, "regenerate "):
					// derived artifacts, not scaffold outputs
				default:
					n, ok := planFilesPerStep[s.Label]
					require.True(t, ok,
						"scaffoldSteps has step %q that ScaffoldPlan doesn't account for — update docsplan.go and planFilesPerStep", s.Label)
					wantCreated += n
				}
			}

			created, patched := ScaffoldPlan(layout.For(layout.Layered), tc.graphql)
			assert.Len(t, created, wantCreated, "created files")
			assert.Len(t, patched, wantPatched, "patched files")
		})
	}
}

func TestScaffoldPlan_Counts(t *testing.T) {
	created, patched := ScaffoldPlan(layout.For(layout.Layered), false)
	assert.Len(t, created, 18)
	assert.Len(t, patched, 4)

	createdGQL, patchedGQL := ScaffoldPlan(layout.For(layout.Layered), true)
	assert.Len(t, createdGQL, 20)
	assert.Len(t, patchedGQL, 6)

	// The graphql lists strictly extend the REST-only lists.
	assert.Equal(t, created, createdGQL[:len(created)])
	assert.Equal(t, patched, patchedGQL[:len(patched)])
}

func TestScaffoldPlan_LayeredPaths(t *testing.T) {
	created, patched := ScaffoldPlan(layout.For(layout.Layered), false)
	assert.Contains(t, created, "app/models/{resource}.model.go")
	assert.Contains(t, created, "db/migrations/<seq>_create_{resources}.up.sql")
	assert.Contains(t, created, "app/repositories/{resource}.repository_test.go")
	assert.Contains(t, patched, "cmd/serve.go")
}

func TestScaffoldPlan_FeatureLayoutDiffers(t *testing.T) {
	layered, _ := ScaffoldPlan(layout.For(layout.Layered), false)
	feature, _ := ScaffoldPlan(layout.For(layout.Feature), false)
	require.Len(t, feature, len(layered))
	assert.NotEqual(t, layered, feature)
}
