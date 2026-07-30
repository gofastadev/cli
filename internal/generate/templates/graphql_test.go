package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGraphQL_InputNamesMatchDTOTypes(t *testing.T) {
	out := renderTemplate(t, "graphql", GraphQL, sampleData())

	// Input names must equal the hand-written DTO type names so gqlgen
	// autobind reuses them (validate tags + ToCreateInput/ToPatch/
	// ToFilter) instead of minting helperless models.
	assert.Contains(t, out, "input TCreateProductDto {")
	assert.Contains(t, out, "input TUpdateProductGraphQLInput {")
	assert.Contains(t, out, "input TArchiveProductDto {")
	assert.Contains(t, out, "input TFindProductByIdDto {")
	assert.Contains(t, out, "input TProductFiltersQueryParamsDto {")

	// Query/Mutation signatures reference the renamed inputs.
	assert.Contains(t, out, "findAllProducts(filters: TProductFiltersQueryParamsDto!): TProductsResponseDto!")
	assert.Contains(t, out, "findProductById(input: TFindProductByIdDto!): Product!")
	assert.Contains(t, out, "createProduct(input: TCreateProductDto!): Product!")
	assert.Contains(t, out, "updateProduct(input: TUpdateProductGraphQLInput!): Product!")
	assert.Contains(t, out, "archiveProduct(input: TArchiveProductDto!): Product!")

	// The old non-binding names must be gone.
	assert.NotContains(t, out, "TCreateProductInput")
	assert.NotContains(t, out, "TUpdateProductInput")
	assert.NotContains(t, out, "TArchiveProductInput")
	assert.NotContains(t, out, "TFindProductByIdInput")
	assert.NotContains(t, out, "TProductFiltersInput")
}
