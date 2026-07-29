package skeleton_test

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/gofastadev/cli/internal/skeleton"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gqlgen owns every `{name}.resolvers.go` file in the scaffold (gqlgen.yml →
// resolver.layout: follow-schema). On each `go tool gqlgen generate` it
// rewrites those files from the schema: bodies of recognized resolver METHODS
// are preserved, and everything else — plain functions, package-level vars,
// types — is relocated into a commented-out `/* ... */` block under a
// "!!! WARNING !!!" banner.
//
// Three shared error helpers used to live in user.resolvers.go. `gofasta new
// --graphql` runs gqlgen as part of scaffolding, so those helpers were
// commented out before the developer ever saw the project, and it failed to
// compile with "undefined: gqlError". They now live in gql_errors.go, which
// gqlgen does not own.
//
// This test stops that from regressing without needing a scaffold or a network
// round-trip: it reads the embedded templates directly.

// funcDeclPattern matches any top-level func declaration and captures whether
// it has a receiver.
var funcDeclPattern = regexp.MustCompile(`(?m)^func\s+(\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)`)

// resolverTemplates returns the embedded *.resolvers.go.tmpl paths.
func resolverTemplates(t *testing.T) []string {
	t.Helper()
	var out []string
	require.NoError(t, fs.WalkDir(skeleton.ProjectFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".resolvers.go.tmpl") {
			out = append(out, p)
		}
		return nil
	}))
	return out
}

// TestResolverTemplates_DeclareOnlyResolverMethods is the guard: a gqlgen-owned
// template must contain nothing but methods, because anything else will be
// commented out the moment gqlgen runs.
func TestResolverTemplates_DeclareOnlyResolverMethods(t *testing.T) {
	templates := resolverTemplates(t)
	require.NotEmpty(t, templates, "expected at least one *.resolvers.go.tmpl in the skeleton")

	for _, tmpl := range templates {
		t.Run(tmpl, func(t *testing.T) {
			body, err := fs.ReadFile(skeleton.ProjectFS, tmpl)
			require.NoError(t, err)

			for _, m := range funcDeclPattern.FindAllStringSubmatch(string(body), -1) {
				receiver, name := m[1], m[2]
				assert.NotEmpty(t, receiver,
					"%s declares plain function %q — gqlgen will comment it out when it "+
						"regenerates this file. Move it to a file gqlgen does not own "+
						"(e.g. app/graphql/resolvers/gql_errors.go.tmpl).", tmpl, name)
			}
		})
	}
}

// TestResolverHelpers_LiveOutsideGqlgenOwnedFiles pins where the helpers
// actually are, so a future move back into a *.resolvers.go file fails here
// with a message explaining why that does not work.
func TestResolverHelpers_LiveOutsideGqlgenOwnedFiles(t *testing.T) {
	const helpersFile = "project/app/graphql/resolvers/gql_errors.go.tmpl"

	body, err := fs.ReadFile(skeleton.ProjectFS, helpersFile)
	require.NoError(t, err, "the shared GraphQL error helpers must ship in a gqlgen-safe file")

	for _, fn := range []string{"gqlError", "validationGqlError", "internalGqlError"} {
		assert.Contains(t, string(body), "func "+fn+"(",
			"%s must declare %s", helpersFile, fn)
	}

	// The filename must not collide with a generated resolver file, which is
	// what would put it back under gqlgen's control.
	assert.False(t, strings.HasSuffix(path.Base(helpersFile), ".resolvers.go.tmpl"),
		"the helpers file must not be named like a gqlgen-generated resolver")
}

// TestResolverTemplates_DoNotImportGqlerror keeps the import in step with the
// declarations: once the helpers moved out, a lingering gqlerror import in a
// resolvers template would be unused and fail to compile.
func TestResolverTemplates_DoNotImportGqlerror(t *testing.T) {
	for _, tmpl := range resolverTemplates(t) {
		t.Run(tmpl, func(t *testing.T) {
			body, err := fs.ReadFile(skeleton.ProjectFS, tmpl)
			require.NoError(t, err)
			assert.NotContains(t, string(body), "gqlparser/v2/gqlerror",
				"%s imports gqlerror but declares no helpers that use it", tmpl)
		})
	}
}
