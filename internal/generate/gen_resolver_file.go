package generate

import (
	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenResolverFile writes a fully-implemented per-resource resolver
// file at app/graphql/resolvers/<snake>.resolvers.go.
//
// Runs right after GenGraphQL (the schema fragment) and BEFORE
// RunGqlgen: gqlgen's follow-schema layout maps the fragment 1:1 to
// this file and preserves any method bodies it finds, so pre-writing
// the implementations is what prevents gqlgen's stock
// panic("not implemented") stubs from ever landing in the project.
//
// WriteTemplate skips existing files, so re-running a generator never
// clobbers user edits. The template itself branches on layout — the
// file path is the same in both layouts (gqlgen owns the resolvers
// dir) but the symbol qualifiers differ; WriteTemplate's auto-featurize
// gate only covers app/<snake>/ paths, so it does not apply here.
func GenResolverFile(d ScaffoldData) error {
	return WriteTemplate(d.L().ResolverResourceFile(d.SnakeName), "resolvers", templates.Resolvers, d)
}
