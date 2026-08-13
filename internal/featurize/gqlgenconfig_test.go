package featurize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// gqlgenYamlSrc mirrors the skeleton's gqlgen.yml.tmpl rendered for
// testMod — including the one-space autobind list indentation and the
// comment lines the rewrite must preserve.
func gqlgenYamlSrc() string {
	return `# Where are all the schema files located? globs are supported eg  src/**/*.graphqls
schema:
  - app/graphql/schema/*.gql

# Where should the generated server code go?
exec:
  filename: app/generated.go
  package: app

# Where should any generated models go?
model:
  filename: app/dtos/generated-types.dtos.go
  package: dtos

# Where should the resolver implementations go?
resolver:
  layout: follow-schema
  dir: app/graphql/resolvers
  package: resolvers
  filename_template: "{name}.resolvers.go"

# gqlgen will search for any type names in the schema in these go packages
# if they match it will use them, otherwise it will generate them.
autobind:
 - "` + testMod + `/app/dtos"

models:
  ID:
    model:
      - github.com/99designs/gqlgen/graphql.UUID
`
}

func TestRewriteGqlgenConfig_Forward(t *testing.T) {
	s := string(RewriteGqlgenConfig([]byte(gqlgenYamlSrc()), testMod, []Resource{userResource(), orderResource()}))

	assert.Contains(t, s, "filename: app/shared/dtos/generated-types.dtos.go")
	assert.NotContains(t, s, "filename: app/dtos/generated-types.dtos.go")
	// The autobind entries keep the template's one-space indentation.
	assert.Contains(t, s, "\n - \""+testMod+"/app/shared/dtos\"\n")
	assert.Contains(t, s, "\n - \""+testMod+"/app/user\"\n")
	assert.Contains(t, s, "\n - \""+testMod+"/app/order\"\n")
	assert.NotContains(t, s, `- "`+testMod+`/app/dtos"`)
	// The exec filename and comments are untouched.
	assert.Contains(t, s, "filename: app/generated.go")
	assert.Contains(t, s, "# Where should any generated models go?")
	assert.Contains(t, s, `filename_template: "{name}.resolvers.go"`)
}

func TestRewriteGqlgenConfig_Idempotent(t *testing.T) {
	resources := []Resource{userResource()}
	once := RewriteGqlgenConfig([]byte(gqlgenYamlSrc()), testMod, resources)
	twice := RewriteGqlgenConfig(once, testMod, resources)
	assert.Equal(t, string(once), string(twice))
}

func TestRewriteGqlgenConfig_ReverseIsByteExactInverse(t *testing.T) {
	resources := []Resource{userResource(), orderResource()}
	forward := RewriteGqlgenConfig([]byte(gqlgenYamlSrc()), testMod, resources)
	back := RewriteGqlgenConfigReverse(forward, testMod, resources)
	assert.Equal(t, gqlgenYamlSrc(), string(back))
}

func TestRewriteGqlgenConfigReverse_Idempotent(t *testing.T) {
	resources := []Resource{userResource()}
	once := RewriteGqlgenConfigReverse([]byte(gqlgenYamlSrc()), testMod, resources)
	// A layered-shaped file reverses to itself.
	assert.Equal(t, gqlgenYamlSrc(), string(once))
}

func TestEnsureGqlgenAutobind(t *testing.T) {
	feature := RewriteGqlgenConfig([]byte(gqlgenYamlSrc()), testMod, []Resource{userResource()})

	// Insert a new resource entry after the shared anchor.
	out := EnsureGqlgenAutobind(feature, testMod, "widget")
	s := string(out)
	assert.Contains(t, s, " - \""+testMod+"/app/widget\"\n")
	sharedIdx := strings.Index(s, testMod+"/app/shared/dtos")
	widgetIdx := strings.Index(s, testMod+"/app/widget")
	assert.Greater(t, widgetIdx, sharedIdx, "widget entry must come after the shared anchor")

	// Already present → unchanged.
	again := EnsureGqlgenAutobind(out, testMod, "widget")
	assert.Equal(t, s, string(again))

	// Missing anchor (layered-shaped file) → unchanged.
	unchanged := EnsureGqlgenAutobind([]byte(gqlgenYamlSrc()), testMod, "widget")
	assert.Equal(t, gqlgenYamlSrc(), string(unchanged))
}

// TestRewriteGqlgenConfig_SkipsPreexistingResourceEntry covers the forward
// dedupe: a hand-edited config may already carry a feature package entry
// while the layered dtos anchor is still present — the rewrite must not
// duplicate it.
func TestRewriteGqlgenConfig_SkipsPreexistingResourceEntry(t *testing.T) {
	src := strings.Replace(gqlgenYamlSrc(),
		"autobind:\n - \""+testMod+"/app/dtos\"\n",
		"autobind:\n - \""+testMod+"/app/dtos\"\n - \""+testMod+"/app/user\"\n", 1)
	s := string(RewriteGqlgenConfig([]byte(src), testMod, []Resource{userResource()}))

	assert.Equal(t, 1, countOccurrences(s, `- "`+testMod+`/app/user"`),
		"pre-existing feature entry must not be duplicated")
	assert.Contains(t, s, `- "`+testMod+`/app/shared/dtos"`)
	assert.NotContains(t, s, `- "`+testMod+`/app/dtos"`)
}

// TestRewriteGqlgenConfig_AddsNewResourceToFlippedConfig covers the
// already-flipped anchor branch actually appending: re-running the rewrite
// with an extra resource must add its entry after the shared anchor
// without duplicating the ones already there.
func TestRewriteGqlgenConfig_AddsNewResourceToFlippedConfig(t *testing.T) {
	once := RewriteGqlgenConfig([]byte(gqlgenYamlSrc()), testMod, []Resource{userResource()})
	again := string(RewriteGqlgenConfig(once, testMod, []Resource{userResource(), orderResource()}))

	assert.Contains(t, again, `- "`+testMod+`/app/order"`)
	assert.Equal(t, 1, countOccurrences(again, `- "`+testMod+`/app/user"`))
	sharedIdx := strings.Index(again, testMod+"/app/shared/dtos")
	orderIdx := strings.Index(again, testMod+"/app/order")
	assert.Greater(t, orderIdx, sharedIdx, "new entry must come after the shared anchor")
}
