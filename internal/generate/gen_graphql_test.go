package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenGraphQL_CreatesFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := GenGraphQL(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/graphql/schema/product.gql")
	assert.Contains(t, content, "Product")
}

func TestGenGraphQL_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	writeTestFile(t, "app/graphql/schema/product.gql", "original")

	err := GenGraphQL(d)
	require.NoError(t, err)
	assert.Equal(t, "original", readTestFile(t, "app/graphql/schema/product.gql"))
}
