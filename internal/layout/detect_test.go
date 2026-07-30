// fsutil_test.go — coverage for the filesystem-dependent half of the layout
// package. Unlike the pure path builders in layout_test.go, these functions
// answer questions about a project on disk, so each test builds the smallest
// tree that makes the answer meaningful and runs from inside it.

package layout

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDetect_ConfigIsAuthoritative pins the priority order: config.yaml wins
// over whatever the filesystem looks like, because `gofasta new` writes the
// key at scaffold time and that value is the project's declared intent.
func TestDetect_ConfigIsAuthoritative(t *testing.T) {
	cases := map[string]struct {
		config string
		want   Kind
	}{
		"feature": {"project:\n  layout: feature\n", Feature},
		"layered": {"project:\n  layout: layered\n", Layered},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			inProjectTree(t, map[string]string{
				"config.yaml": tc.config,
				// A layered-looking tree that must not override the config.
				"app/models/user.model.go": "package models",
			})
			assert.Equal(t, tc.want, Detect().Kind())
		})
	}
}

// TestHasGraphQLArtifacts pins the project-state signal the generators
// and refactor share to decide whether GraphQL applies.
func TestHasGraphQLArtifacts(t *testing.T) {
	t.Run("gqlgen.yml is sufficient", func(t *testing.T) {
		inProjectTree(t, map[string]string{"gqlgen.yml": "schema:\n"})
		assert.True(t, HasGraphQLArtifacts())
	})

	t.Run("resolvers directory is sufficient", func(t *testing.T) {
		inProjectTree(t, map[string]string{"app/graphql/resolvers/": ""})
		assert.True(t, HasGraphQLArtifacts())
	})

	t.Run("a resolvers FILE is not a directory signal", func(t *testing.T) {
		inProjectTree(t, map[string]string{"app/graphql/resolvers": "not a dir"})
		assert.False(t, HasGraphQLArtifacts())
	})

	t.Run("REST-only project has neither", func(t *testing.T) {
		inProjectTree(t, map[string]string{"app/models/user.model.go": "package models"})
		assert.False(t, HasGraphQLArtifacts())
	})
}
