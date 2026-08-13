package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSvcTest_GeneratesCreateTest(t *testing.T) {
	out := renderTemplate(t, "svctest", SvcTest, sampleData())

	assert.Contains(t, out, "func TestProductService_Create(t *testing.T)")
	// The happy path builds an input from sample literals…
	assert.Contains(t, out, `Name: "sample-name",`)
	assert.Contains(t, out, `Summary: "sample text",`)
	assert.Contains(t, out, "OwnerID: uuid.New(),")
	assert.Contains(t, out, "ReleasedAt: time.Now().UTC(),")
	// …and asserts every field survived the input→model mapping.
	assert.Contains(t, out, "assert.Equal(t, in.Name, got.Name)")
	assert.Contains(t, out, "assert.Equal(t, in.ReleasedAt, got.ReleasedAt)")
	// The infra branch pins the wrap prefix.
	assert.Contains(t, out, `assert.Contains(t, err.Error(), "ProductService.Create")`)
}

func TestSvcTest_RendersValidWithZeroFields(t *testing.T) {
	data := sampleData()
	data.Fields = nil
	out := renderTemplate(t, "svctest", SvcTest, data)

	assert.Contains(t, out, "func TestProductService_Create(t *testing.T)")
	assert.Contains(t, out, "in := services.CreateProductInput{\n\t\t}")
}
