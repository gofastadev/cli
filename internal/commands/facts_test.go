package commands

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildFacts_Deterministic(t *testing.T) {
	first, err := buildFacts()
	require.NoError(t, err)
	second, err := buildFacts()
	require.NoError(t, err)

	a, err := json.Marshal(first)
	require.NoError(t, err)
	b, err := json.Marshal(second)
	require.NoError(t, err)
	assert.Equal(t, string(a), string(b))
}

func TestBuildFacts_EveryTopLevelCommandGrouped(t *testing.T) {
	facts, err := buildFacts()
	require.NoError(t, err)
	for _, c := range facts.Commands {
		assert.NotEmpty(t, c.Group, "top-level command %q has no help group — add it to commandGroupAssignments (or set GroupID) so help, README, and facts agree", c.Name)
	}
}

func TestBuildFacts_HiddenCommandsExcluded(t *testing.T) {
	facts, err := buildFacts()
	require.NoError(t, err)
	for _, c := range facts.Commands {
		assert.NotEqual(t, "__complete", c.Name)
		assert.NotEqual(t, "help", c.Name)
	}
}

func TestBuildFacts_CoreShape(t *testing.T) {
	facts, err := buildFacts()
	require.NoError(t, err)

	assert.Equal(t, 1, facts.SchemaVersion)
	assert.Equal(t, 18, facts.Scaffold.CreatedCount)
	assert.Equal(t, 4, facts.Scaffold.PatchedCount)
	assert.Len(t, facts.Scaffold.GraphQLExtra.Created, 2)
	assert.Len(t, facts.Scaffold.GraphQLExtra.Patched, 2)
	assert.Equal(t, 78, facts.Skeleton.FileCount)
	assert.Equal(t, []string{"postgres", "mysql", "sqlite", "sqlserver", "clickhouse"}, facts.Drivers)
	assert.Equal(t, []string{"layered", "feature"}, facts.Layouts)
	assert.Len(t, facts.FieldTypes, 7)
	assert.Len(t, facts.Workflows, 5)
	assert.Len(t, facts.AIAgents, 5)
	assert.Equal(t, []string{"linux", "darwin", "windows"}, facts.ReleasePlatforms.Goos)

	// Every driver ships at least one foundational migration set.
	require.Len(t, facts.Skeleton.Migrations, len(facts.Drivers))
	for _, m := range facts.Skeleton.Migrations {
		assert.Positive(t, m.Count, "driver %s has no foundational migrations", m.Driver)
	}

	// The `g` alias must be published — docs use it everywhere.
	var generateCmd *string
	for _, c := range facts.Commands {
		if c.Name == "generate" {
			generateCmd = &c.Aliases[0]
		}
	}
	require.NotNil(t, generateCmd, "generate command missing from facts")
	assert.Equal(t, "g", *generateCmd)
}

func TestWorkflowFacts_RendersSteps(t *testing.T) {
	wfs := workflowFacts()
	require.Len(t, wfs, 5)
	byKey := map[string][]string{}
	for _, wf := range wfs {
		byKey[wf.Key] = wf.Steps
	}
	assert.Equal(t,
		[]string{"gofasta g scaffold <ResourceName>", "gofasta migrate up", "gofasta swagger"},
		byKey["new-rest-endpoint"])
	assert.Equal(t, []string{"gofasta wire", "gofasta swagger"}, byKey["rebuild"])
}
