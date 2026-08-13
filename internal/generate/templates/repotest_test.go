package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRepoTest_FixturePopulatesAllFields(t *testing.T) {
	out := renderTemplate(t, "repotest", RepoTest, sampleData())

	for _, line := range []string{
		`Name: "sample-name",`,
		"Price: 1.5,",
		`Summary: "sample text",`,
		"OwnerID: uuid.New(),",
		"ReleasedAt: time.Now().UTC(),",
	} {
		assert.Contains(t, out, line)
	}
	assert.NotContains(t, out, "TODO")
	// The pagination loop reuses the fixture instead of bare Create.
	assert.Contains(t, out, "makeProduct(t, db)\n\t\ttime.Sleep")
}

func TestRepoTest_RendersValidWithZeroFields(t *testing.T) {
	data := sampleData()
	data.Fields = nil
	out := renderTemplate(t, "repotest", RepoTest, data)

	assert.Contains(t, out, "e := &models.Product{\n\t}")
}
