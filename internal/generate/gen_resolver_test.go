package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenResolver_SucceedsWithResolverFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	resolverContent := `package resolvers

import (
	svcInterfaces "github.com/testorg/testapp/app/services/interfaces"
)

type Resolver struct {
	UserService svcInterfaces.UserServiceInterface
	// gofasta:scaffold:resolver-fields
}

// NewResolver creates a new resolver.
func NewResolver(userService svcInterfaces.UserServiceInterface) *Resolver {
	return &Resolver{UserService: userService}
}
`
	writeTestFile(t, "app/graphql/resolvers/resolver.go", resolverContent)

	err := GenResolver(d)
	assert.NoError(t, err)

	content := readTestFile(t, "app/graphql/resolvers/resolver.go")
	assert.Contains(t, content, "ProductService")
}
