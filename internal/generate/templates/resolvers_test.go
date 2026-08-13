package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolvers_LayeredQualifiers(t *testing.T) {
	out := renderTemplate(t, "resolvers", Resolvers, sampleData())

	// Imports.
	assert.Contains(t, out, `"github.com/testorg/testapp/app/dtos"`)
	assert.Contains(t, out, `"github.com/testorg/testapp/app/services"`)
	assert.NotContains(t, out, "shared/dtos")
	assert.NotContains(t, out, "productpkg")

	// All five resolver methods, implemented — never a panic stub.
	assert.Contains(t, out, "func (r *mutationResolver) CreateProduct(ctx context.Context, input dtos.TCreateProductDto) (*dtos.Product, error)")
	assert.Contains(t, out, "func (r *mutationResolver) UpdateProduct(ctx context.Context, input dtos.TUpdateProductGraphQLInput) (*dtos.Product, error)")
	assert.Contains(t, out, "func (r *mutationResolver) ArchiveProduct(ctx context.Context, input dtos.TArchiveProductDto) (*dtos.Product, error)")
	assert.Contains(t, out, "func (r *queryResolver) FindAllProducts(ctx context.Context, filters dtos.TProductFiltersQueryParamsDto) (*dtos.TProductsResponseDto, error)")
	assert.Contains(t, out, "func (r *queryResolver) FindProductByID(ctx context.Context, input dtos.TFindProductByIDDto) (*dtos.Product, error)")
	assert.NotContains(t, out, "panic(")
	assert.NotContains(t, out, "not implemented")

	// Body shape: validation, service calls, sentinel mapping, DTO helpers.
	assert.Contains(t, out, "r.Validator.ValidateStruct(input)")
	assert.Contains(t, out, "r.ProductService.Create(ctx, input.ToCreateInput())")
	assert.Contains(t, out, "r.ProductService.Update(ctx, input.ID, input.RecordVersion, input.ToPatch())")
	assert.Contains(t, out, "errors.Is(err, services.ErrProductVersionConflict)")
	assert.Contains(t, out, "errors.Is(err, services.ErrProductNotDeletable)")
	assert.Contains(t, out, "errors.Is(err, gorm.ErrRecordNotFound)")
	assert.Contains(t, out, "f := filters.ToFilter()")
	assert.Contains(t, out, "dtos.ProductsFromModels(entities)")
	assert.Contains(t, out, "&dtos.TPaginationObjectDto{")
}

func TestResolvers_FeatureQualifiers(t *testing.T) {
	data := sampleData()
	data.FeatureLayout = true
	out := renderTemplate(t, "resolvers", Resolvers, data)

	// Imports: per-feature package + shared dtos (unaliased, matching
	// what featurize.TransformGraphQL produces so refactor round-trips).
	assert.Contains(t, out, `productpkg "github.com/testorg/testapp/app/product"`)
	assert.Contains(t, out, `"github.com/testorg/testapp/app/shared/dtos"`)
	assert.NotContains(t, out, `"github.com/testorg/testapp/app/dtos"`)
	assert.NotContains(t, out, `"github.com/testorg/testapp/app/services"`)

	// Per-resource symbols re-qualify; the shared pagination type keeps
	// its dtos. qualifier.
	assert.Contains(t, out, "input productpkg.TCreateProductDto")
	assert.Contains(t, out, "productpkg.ProductFromModel(entity)")
	assert.Contains(t, out, "errors.Is(err, productpkg.ErrProductNotFound)")
	assert.Contains(t, out, "&productpkg.TProductsResponseDto{")
	assert.Contains(t, out, "&dtos.TPaginationObjectDto{")
}

func TestResolvers_RendersValidWithZeroFields(t *testing.T) {
	data := sampleData()
	data.Fields = nil
	out := renderTemplate(t, "resolvers", Resolvers, data)

	// Resolver bodies don't range over fields — rendering must be
	// unaffected by a zero-field resource.
	assert.Contains(t, out, "func (r *mutationResolver) CreateProduct")
	assert.Contains(t, out, "func (r *queryResolver) FindProductByID")
}
