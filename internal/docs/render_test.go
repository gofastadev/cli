package docs

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func renderFixture() Facts {
	return Facts{
		Groups: []Group{
			{ID: "lifecycle", Title: "Project lifecycle"},
			{ID: "workflow", Title: "Development workflow"},
		},
		Commands: []Command{
			{Name: "new", Group: "lifecycle", Short: "Scaffold"},
			{Name: "config", Group: "workflow", Children: []Command{{Name: "schema"}}},
			{Name: "generate", Group: "workflow", Aliases: []string{"g"},
				Children: []Command{{Name: "model"}, {Name: "scaffold"}}},
		},
		Scaffold: Scaffold{
			Layered: ScaffoldFiles{
				Created: []string{"app/models/{resource}.model.go"},
				Patched: []string{"app/di/container.go"},
			},
			CreatedCount: 18,
			PatchedCount: 4,
		},
		FieldTypes: []FieldType{
			{Key: "string", GoType: "string", GQLType: "String", SQL: SQLTypes{Postgres: "VARCHAR(255) NOT NULL"}},
			{Key: "time", Aliases: []string{"datetime"}, GoType: "time.Time", GQLType: "DateTime", SQL: SQLTypes{Postgres: "TIMESTAMP"}},
		},
		Workflows: []Workflow{
			{Key: "rebuild", Description: "Regenerate artifacts"},
			{Key: "new-rest-endpoint", Description: "Scaffold + migrate", Args: "<ResourceName> [field:type ...]"},
		},
		AIAgents: []AIAgent{{Key: "claude", Name: "Claude Code", Description: "Anthropic's agent"}},
	}
}

func TestRenderCommandIndex(t *testing.T) {
	out := renderCommandIndex(renderFixture())
	assert.Contains(t, out, "| Project lifecycle | `new` |")
	assert.Contains(t, out, "`config` (1 subcommand: `schema`)")
	assert.Contains(t, out, "`generate` (alias `g`) (2 subcommands: `model`, `scaffold`)")
}

func TestRenderFieldTypes(t *testing.T) {
	out := renderFieldTypes(renderFixture())
	assert.Contains(t, out, "| `string` | `string` | `VARCHAR(255) NOT NULL` | `String` |")
	assert.Contains(t, out, "| `time` (alias `datetime`) |")
}

func TestRenderScaffoldFiles(t *testing.T) {
	out, err := renderScaffoldFiles(renderFixture())
	require.NoError(t, err)
	assert.Contains(t, out, "creates **18 files**")
	assert.Contains(t, out, "| `app/models/product.model.go` |")
	assert.Contains(t, out, "- `app/di/container.go` —")
}

func TestRenderScaffoldFiles_UnknownPathFails(t *testing.T) {
	f := renderFixture()
	f.Scaffold.Layered.Created = append(f.Scaffold.Layered.Created, "app/new/{resource}.thing.go")
	_, err := renderScaffoldFiles(f)
	assert.ErrorContains(t, err, "has no description")

	f = renderFixture()
	f.Scaffold.Layered.Patched = append(f.Scaffold.Layered.Patched, "app/new/target.go")
	_, err = renderScaffoldFiles(f)
	assert.ErrorContains(t, err, "has no description")
}

func TestRenderWorkflowsAndAgents(t *testing.T) {
	wf := renderWorkflows(renderFixture())
	assert.True(t, strings.HasPrefix(wf, "```bash\n"))
	assert.Contains(t, wf, "gofasta do rebuild")
	assert.Contains(t, wf, "gofasta do new-rest-endpoint <ResourceName> [field:type ...]")
	assert.Contains(t, wf, "gofasta do list")

	ag := renderAIAgents(renderFixture())
	assert.Contains(t, ag, "gofasta ai claude")
	assert.Contains(t, ag, "gofasta ai status")
}

func TestRenderBlocks_AllIDsPresent(t *testing.T) {
	blocks, err := RenderBlocks(renderFixture())
	require.NoError(t, err)
	for _, id := range []string{"command-index", "field-types", "scaffold-files", "do-workflows", "ai-agents"} {
		assert.Contains(t, blocks, id)
	}
}

func TestRenderBlocks_ScaffoldErrorPropagates(t *testing.T) {
	f := renderFixture()
	f.Scaffold.Layered.Created = append(f.Scaffold.Layered.Created, "app/unknown/{resource}.go")
	_, err := RenderBlocks(f)
	assert.ErrorContains(t, err, "has no description")
}

func TestRenderCommandIndex_EmptyGroupOmitted(t *testing.T) {
	f := renderFixture()
	f.Groups = append(f.Groups, Group{ID: "deploy", Title: "Deployment"})
	out := renderCommandIndex(f)
	assert.NotContains(t, out, "Deployment", "groups with no commands must not render a row")
}
