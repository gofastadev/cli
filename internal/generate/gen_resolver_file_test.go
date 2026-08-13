package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenResolverFile_CreatesImplementedResolvers(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := GenResolverFile(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/graphql/resolvers/product.resolvers.go")
	assert.Contains(t, content, "func (r *mutationResolver) CreateProduct")
	assert.Contains(t, content, "func (r *queryResolver) FindAllProducts")
	assert.Contains(t, content, "r.ProductService.Create(ctx, input.ToCreateInput())")
	assert.NotContains(t, content, "panic(")
	// Layered qualifiers.
	assert.Contains(t, content, `"github.com/testorg/testapp/app/services"`)
}

func TestGenResolverFile_FeatureLayoutQualifiers(t *testing.T) {
	setupTempProject(t)
	d := featureScaffoldData()

	err := GenResolverFile(d)
	require.NoError(t, err)

	// Same path in feature layout — gqlgen owns the resolvers dir.
	content := readTestFile(t, "app/graphql/resolvers/product.resolvers.go")
	assert.Contains(t, content, `productpkg "github.com/testorg/testapp/app/product"`)
	assert.Contains(t, content, `"github.com/testorg/testapp/app/shared/dtos"`)
	assert.Contains(t, content, "productpkg.ProductFromModel(entity)")
	assert.Contains(t, content, "&dtos.TPaginationObjectDto{")
}

func TestGenResolverFile_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	writeTestFile(t, "app/graphql/resolvers/product.resolvers.go", "original")

	err := GenResolverFile(d)
	require.NoError(t, err)
	assert.Equal(t, "original", readTestFile(t, "app/graphql/resolvers/product.resolvers.go"))
}

func TestGenResolverFile_DryRunRecordsCreate(t *testing.T) {
	setupTempProject(t)
	resetPlannerState(t)
	SetDryRun(true)
	t.Cleanup(func() { SetDryRun(false) })

	err := GenResolverFile(sampleScaffoldData())
	require.NoError(t, err)

	actions := Plan()
	require.Len(t, actions, 1)
	assert.Equal(t, "create", actions[0].Kind)
	assert.Equal(t, "app/graphql/resolvers/product.resolvers.go", actions[0].Path)
}
