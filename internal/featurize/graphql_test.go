package featurize

import (
	"go/format"
	"sort"
	"strings"
	"testing"
	texttemplate "text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofastadev/cli/internal/generate/templates"
)

func orderResource() Resource {
	return Resource{Name: "Order", Snake: "order", Plural: "Orders"}
}

// resolverStructSrc mirrors the skeleton's resolver.go for a non-User
// resource — the DI'd Resolver struct referencing the service interface.
func resolverStructSrc() string {
	return `package resolvers

import (
	svcInterfaces "` + testMod + `/app/services/interfaces"
	"` + testMod + `/app/validators"
)

type Resolver struct {
	OrderService svcInterfaces.OrderServiceInterface
	Validator    *validators.AppValidator
}

func NewResolver(orderService svcInterfaces.OrderServiceInterface, validator *validators.AppValidator) *Resolver {
	return &Resolver{OrderService: orderService, Validator: validator}
}
`
}

// orderResolversSrc mirrors the skeleton's user.resolvers.go for a
// non-User resource: per-resource dtos, service sentinels, and one
// shared dtos alias (TPaginationObjectDto) that must keep its
// qualifier.
func orderResolversSrc() string {
	return `package resolvers

import (
	"context"
	"errors"

	"` + testMod + `/app/dtos"
	"` + testMod + `/app/services"
)

func (r *mutationResolver) CreateOrder(ctx context.Context, input dtos.TCreateOrderDto) (*dtos.Order, error) {
	order, err := r.OrderService.Create(ctx, input.ToCreateInput())
	switch {
	case errors.Is(err, services.ErrOrderVersionConflict):
		return nil, gqlError("CONFLICT", "record version mismatch")
	case errors.Is(err, services.ErrOrderNotFound):
		return nil, gqlError("NOT_FOUND", "order not found")
	case err != nil:
		return nil, internalGqlError("failed to create order", err)
	}
	return dtos.OrderFromModel(order), nil
}

func (r *queryResolver) FindOrdersWithFilters(ctx context.Context, filters dtos.TOrderFiltersQueryParamsDto) (*dtos.TOrdersResponseDto, error) {
	orders, total, err := r.OrderService.List(ctx, orderFiltersToFilter(filters))
	if err != nil {
		return nil, internalGqlError("failed to list orders", err)
	}
	totalRecords := int(total)
	return &dtos.TOrdersResponseDto{
		Data: dtos.OrdersFromModels(orders),
		Pagination: &dtos.TPaginationObjectDto{
			TotalRecords: &totalRecords,
		},
	}, nil
}
`
}

// gqlFiltersSrc mirrors gql_filters.go: a gqlgen-GENERATED dtos type
// (OrderFiltersDto — not a per-resource hand-written symbol) plus the
// domain filter from the service layer.
func gqlFiltersSrc() string {
	return `package resolvers

import (
	"` + testMod + `/app/dtos"
	"` + testMod + `/app/services"
)

func orderFiltersToFilter(in dtos.OrderFiltersDto) services.ListOrdersFilter {
	f := services.ListOrdersFilter{Page: 1}
	return f
}
`
}

func TestTransformGraphQL_ResolverStruct(t *testing.T) {
	out, err := TransformGraphQL([]byte(resolverStructSrc()), testMod, []Resource{orderResource()})
	require.NoError(t, err)
	s := string(out)

	assert.Contains(t, s, "orderpkg.OrderServiceInterface")
	assert.NotContains(t, s, "svcInterfaces.")
	assert.NotContains(t, s, `"`+testMod+`/app/services/interfaces"`)
	assert.Contains(t, s, `orderpkg "`+testMod+`/app/order"`)
	assert.Contains(t, s, `"`+testMod+`/app/validators"`)
	assert.Contains(t, s, "package resolvers")
}

func TestTransformGraphQL_PerResourceResolversFile(t *testing.T) {
	out, err := TransformGraphQL([]byte(orderResolversSrc()), testMod, []Resource{orderResource()})
	require.NoError(t, err)
	s := string(out)

	// Per-resource dtos re-qualify to the feature package.
	assert.Contains(t, s, "orderpkg.TCreateOrderDto")
	assert.Contains(t, s, "*orderpkg.Order")
	assert.Contains(t, s, "orderpkg.OrderFromModel")
	assert.Contains(t, s, "orderpkg.TOrdersResponseDto")
	assert.Contains(t, s, "orderpkg.OrdersFromModels")
	assert.Contains(t, s, "orderpkg.TOrderFiltersQueryParamsDto")
	// Service sentinels re-qualify too.
	assert.Contains(t, s, "orderpkg.ErrOrderVersionConflict")
	assert.Contains(t, s, "orderpkg.ErrOrderNotFound")
	assert.NotContains(t, s, "services.")
	assert.NotContains(t, s, `"`+testMod+`/app/services"`)
	// The shared alias keeps its qualifier; the import path flips.
	assert.Contains(t, s, "dtos.TPaginationObjectDto")
	assert.Contains(t, s, `"`+testMod+`/app/shared/dtos"`)
	assert.NotContains(t, s, `"`+testMod+`/app/dtos"`)
	assert.Contains(t, s, `orderpkg "`+testMod+`/app/order"`)
}

func TestTransformGraphQL_DropsDtosImportWhenFullyRequalified(t *testing.T) {
	src := `package resolvers

import (
	"context"

	"` + testMod + `/app/dtos"
)

func (r *queryResolver) FindOrderByID(ctx context.Context, filters dtos.TFindOrderByIDDto) (*dtos.Order, error) {
	return nil, nil
}
`
	out, err := TransformGraphQL([]byte(src), testMod, []Resource{orderResource()})
	require.NoError(t, err)
	s := string(out)

	assert.Contains(t, s, "orderpkg.TFindOrderByIDDto")
	assert.NotContains(t, s, "/app/dtos")
	assert.NotContains(t, s, "/app/shared/dtos")
}

func TestTransformGraphQL_GqlFiltersKeepsGeneratedTypeQualifier(t *testing.T) {
	out, err := TransformGraphQL([]byte(gqlFiltersSrc()), testMod, []Resource{orderResource()})
	require.NoError(t, err)
	s := string(out)

	// OrderFiltersDto is gqlgen-generated — not on the per-resource
	// list — so it keeps the dtos qualifier with the flipped path.
	assert.Contains(t, s, "dtos.OrderFiltersDto")
	assert.Contains(t, s, `"`+testMod+`/app/shared/dtos"`)
	// The domain filter follows the service symbols into the feature.
	assert.Contains(t, s, "orderpkg.ListOrdersFilter")
	assert.NotContains(t, s, "services.ListOrdersFilter")
	assert.NotContains(t, s, `"`+testMod+`/app/services"`)
}

func TestTransformGraphQL_MultiResource(t *testing.T) {
	src := `package resolvers

import (
	"` + testMod + `/app/dtos"
	"` + testMod + `/app/services"
)

func lookupBoth() {
	_ = dtos.TCreateUserDto{}
	_ = dtos.TCreateOrderDto{}
	_ = services.ErrUserNotFound
	_ = services.ErrOrderNotFound
}
`
	out, err := TransformGraphQL([]byte(src), testMod, []Resource{userResource(), orderResource()})
	require.NoError(t, err)
	s := string(out)

	assert.Contains(t, s, "userpkg.TCreateUserDto")
	assert.Contains(t, s, "orderpkg.TCreateOrderDto")
	assert.Contains(t, s, "userpkg.ErrUserNotFound")
	assert.Contains(t, s, "orderpkg.ErrOrderNotFound")
	assert.Contains(t, s, `userpkg "`+testMod+`/app/user"`)
	assert.Contains(t, s, `orderpkg "`+testMod+`/app/order"`)
}

func TestTransformGraphQL_OutOfScopeResourceUntouched(t *testing.T) {
	// Only Order is in scope; the User references must stay layered.
	out, err := TransformGraphQL([]byte(orderResolversSrc()), testMod, []Resource{orderResource()})
	require.NoError(t, err)
	require.NotContains(t, string(out), "userpkg.")

	src := `package resolvers

import (
	"` + testMod + `/app/services"
)

func check() error { return services.ErrUserNotFound }
`
	out, err = TransformGraphQL([]byte(src), testMod, []Resource{orderResource()})
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, "services.ErrUserNotFound")
	assert.Contains(t, s, `"`+testMod+`/app/services"`)
}

func TestTransformGraphQL_NonIdentSelectorsSafe(t *testing.T) {
	src := `package resolvers

func chained() {` + nonIdentSelectors + `}
`
	out, err := TransformGraphQL([]byte(src), testMod, []Resource{orderResource()})
	require.NoError(t, err)
	assert.Contains(t, string(out), "outer.inner.Field")
}

func TestTransformGraphQLReverse_ResolverStruct(t *testing.T) {
	forward, err := TransformGraphQL([]byte(resolverStructSrc()), testMod, []Resource{orderResource()})
	require.NoError(t, err)

	back, err := TransformGraphQLReverse(forward, testMod, []Resource{orderResource()})
	require.NoError(t, err)
	s := string(back)

	assert.Contains(t, s, "svcInterfaces.OrderServiceInterface")
	assert.Contains(t, s, `svcInterfaces "`+testMod+`/app/services/interfaces"`)
	assert.NotContains(t, s, "orderpkg")
}

func TestTransformGraphQLReverse_RestoresDtosAndServices(t *testing.T) {
	forward, err := TransformGraphQL([]byte(orderResolversSrc()), testMod, []Resource{orderResource()})
	require.NoError(t, err)

	back, err := TransformGraphQLReverse(forward, testMod, []Resource{orderResource()})
	require.NoError(t, err)
	s := string(back)

	assert.Contains(t, s, "dtos.TCreateOrderDto")
	assert.Contains(t, s, "services.ErrOrderNotFound")
	assert.Contains(t, s, `"`+testMod+`/app/dtos"`)
	assert.Contains(t, s, `"`+testMod+`/app/services"`)
	assert.NotContains(t, s, "orderpkg")
	assert.NotContains(t, s, "/app/shared/dtos")
}

// stripImportBlock removes the parenthesized import block so two
// sources can be compared modulo import ORDER — ensureImport appends
// at the end of the block, so a round trip can legally reorder imports
// while everything else must match byte-for-byte.
func stripImportBlock(t *testing.T, src string) (body string, imports []string) {
	t.Helper()
	inBlock := false
	var kept []string
	for line := range strings.SplitSeq(src, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "import (":
			inBlock = true
		case inBlock && trimmed == ")":
			inBlock = false
		case inBlock:
			if trimmed != "" {
				imports = append(imports, trimmed)
			}
		default:
			kept = append(kept, line)
		}
	}
	sort.Strings(imports)
	return strings.Join(kept, "\n"), imports
}

// TestTransformGraphQL_RoundTrip proves forward∘reverse is the
// identity (modulo gofmt and import order) for every skeleton-shaped
// fixture.
func TestTransformGraphQL_RoundTrip(t *testing.T) {
	fixtures := map[string]string{
		"resolver_struct": resolverStructSrc(),
		"order_resolvers": orderResolversSrc(),
		"gql_filters":     gqlFiltersSrc(),
	}
	for name, src := range fixtures {
		t.Run(name, func(t *testing.T) {
			formatted, err := format.Source([]byte(src))
			require.NoError(t, err)

			forward, err := TransformGraphQL([]byte(src), testMod, []Resource{orderResource()})
			require.NoError(t, err)
			back, err := TransformGraphQLReverse(forward, testMod, []Resource{orderResource()})
			require.NoError(t, err)

			wantBody, wantImports := stripImportBlock(t, string(formatted))
			gotBody, gotImports := stripImportBlock(t, string(back))
			assert.Equal(t, wantBody, gotBody)
			assert.Equal(t, wantImports, gotImports)
		})
	}
}

func TestPerResourceDtoSymbolNames_CoversSkeletonDtoSurface(t *testing.T) {
	names := perResourceDtoSymbolNames(userResource())
	require.Len(t, names, 11)
	for _, expected := range []string{
		"User", "TUserResponseDto", "TUsersResponseDto", "TCreateUserDto",
		"TUpdateUserDto", "TArchiveUserDto", "TFindUserByIDDto",
		"TUserFiltersQueryParamsDto", "TUpdateUserGraphQLInput",
		"UserFromModel", "UsersFromModels",
	} {
		assert.Contains(t, names, expected)
	}
	// None of the per-resource names may collide with the shared
	// aliases — a collision would re-qualify a shared symbol.
	for _, n := range names {
		assert.False(t, sharedDtoSymbols[n], "per-resource symbol %q collides with sharedDtoSymbols", n)
	}
}

func TestTransformGraphQL_IsIdempotent(t *testing.T) {
	once, err := TransformGraphQL([]byte(orderResolversSrc()), testMod, []Resource{orderResource()})
	require.NoError(t, err)
	twice, err := TransformGraphQL(once, testMod, []Resource{orderResource()})
	require.NoError(t, err)
	assert.Equal(t, string(once), string(twice))
}

func TestTransformGraphQL_ParseError(t *testing.T) {
	broken := []byte("package resolvers\n\nfunc Broken( {\n")
	_, err := TransformGraphQL(broken, testMod, []Resource{orderResource()})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "parse"))

	_, err = TransformGraphQLReverse(broken, testMod, []Resource{orderResource()})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "parse"))
}

// resolverTmplData is the minimal mirror of generate.ScaffoldData the
// Resolvers template consults. Defined locally because featurize must
// not import internal/generate (generate imports featurize).
type resolverTmplData struct {
	Name        string
	LowerName   string
	SnakeName   string
	PluralName  string
	PluralLower string
	ModulePath  string
	Feature     bool
}

type resolverTmplLayout struct{ feature bool }

func (l resolverTmplLayout) IsFeature() bool { return l.feature }

func (d resolverTmplData) L() resolverTmplLayout { return resolverTmplLayout{d.Feature} }

func renderResolversTemplate(t *testing.T, feature bool) []byte {
	t.Helper()
	parsed, err := texttemplate.New("resolvers").Parse(templates.Resolvers)
	require.NoError(t, err)
	var buf strings.Builder
	require.NoError(t, parsed.Execute(&buf, resolverTmplData{
		Name:        "Order",
		LowerName:   "order",
		SnakeName:   "order",
		PluralName:  "Orders",
		PluralLower: "orders",
		ModulePath:  testMod,
		Feature:     feature,
	}))
	return []byte(buf.String())
}

// TestTransformGraphQL_GeneratedResolversTemplateFixpoint pins the
// contract between the resolver generator template and this package's
// transforms: the template's layered render must forward-transform into
// exactly its feature render (and back), i.e. every symbol the template
// emits stays inside the forward/reverse swap tables. If a template
// edit introduces a symbol outside perResourceDtoSymbolNames /
// perResourceServiceSymbolNames / sharedDtoSymbols, this test fails.
func TestTransformGraphQL_GeneratedResolversTemplateFixpoint(t *testing.T) {
	layered := renderResolversTemplate(t, false)
	feature := renderResolversTemplate(t, true)
	res := []Resource{orderResource()}

	layeredFmt, err := format.Source(layered)
	require.NoError(t, err, "layered render must be gofmt-clean:\n%s", layered)
	featureFmt, err := format.Source(feature)
	require.NoError(t, err, "feature render must be gofmt-clean:\n%s", feature)

	t.Run("forward: layered render becomes the feature render", func(t *testing.T) {
		forward, err := TransformGraphQL(layeredFmt, testMod, res)
		require.NoError(t, err)
		wantBody, wantImports := stripImportBlock(t, string(featureFmt))
		gotBody, gotImports := stripImportBlock(t, string(forward))
		assert.Equal(t, wantBody, gotBody)
		assert.Equal(t, wantImports, gotImports)
	})

	t.Run("reverse: feature render becomes the layered render", func(t *testing.T) {
		back, err := TransformGraphQLReverse(featureFmt, testMod, res)
		require.NoError(t, err)
		wantBody, wantImports := stripImportBlock(t, string(layeredFmt))
		gotBody, gotImports := stripImportBlock(t, string(back))
		assert.Equal(t, wantBody, gotBody)
		assert.Equal(t, wantImports, gotImports)
	})
}
