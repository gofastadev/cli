package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

func GenGraphQL(d ScaffoldData) error {
	return WriteTemplate(fmt.Sprintf("app/graphql/schema/%s.gql", d.SnakeName), "graphql", templates.GraphQL, d)
}
