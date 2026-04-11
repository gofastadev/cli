package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenGraphQL writes a GraphQL schema fragment for the scaffolded resource.
func GenGraphQL(d ScaffoldData) error {
	return WriteTemplate(fmt.Sprintf("app/graphql/schema/%s.gql", d.SnakeName), "graphql", templates.GraphQL, d)
}
