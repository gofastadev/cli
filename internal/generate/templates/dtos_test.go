package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDTOs_GraphQLUpdateInput(t *testing.T) {
	out := renderTemplate(t, "dtos", DTOs, sampleData())

	assert.Contains(t, out, "type TUpdateProductGraphQLInput struct {")
	assert.Contains(t, out, `ID            uuid.UUID `+"`"+`json:"id" validate:"required,uuid4_valid,does_record_exist_by_id_for_verification=products"`+"`")
	assert.Contains(t, out, `RecordVersion int       `+"`"+`json:"recordVersion" validate:"required,min=1"`+"`")
	// Resource fields are optional pointers.
	assert.Contains(t, out, "Name *string")
	assert.Contains(t, out, "ReleasedAt *time.Time")
	// ToPatch converges on the same domain patch as the REST DTO.
	assert.Contains(t, out, "func (d TUpdateProductGraphQLInput) ToPatch() services.UpdateProductPatch {")
}

func TestDTOs_GraphQLUpdateInputZeroFields(t *testing.T) {
	data := sampleData()
	data.Fields = nil
	out := renderTemplate(t, "dtos", DTOs, data)

	assert.Contains(t, out, "type TUpdateProductGraphQLInput struct {")
	assert.Contains(t, out, "IsActive    *bool")
}
