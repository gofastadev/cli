package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCmd_HasSubcommands(t *testing.T) {
	cmds := Cmd.Commands()
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Name())
	}

	expectedCmds := []string{"scaffold", "model", "repository", "service", "controller",
		"dto", "migration", "route", "resolver", "provider", "email-template", "job", "task"}
	for _, expected := range expectedCmds {
		assert.Contains(t, names, expected, "Cmd should have subcommand: %s", expected)
	}
}

func TestCmd_HasAliases(t *testing.T) {
	assert.Contains(t, Cmd.Aliases, "g")
}

func TestWireCmd_Exists(t *testing.T) {
	assert.Equal(t, "wire", WireCmd.Use)
}

func TestModelSteps(t *testing.T) {
	steps := modelSteps()
	assert.Len(t, steps, 2)
	assert.Equal(t, "model", steps[0].Label)
	assert.Equal(t, "migration", steps[1].Label)
}

func TestDtoSteps(t *testing.T) {
	steps := dtoSteps()
	assert.Len(t, steps, 1)
	assert.Equal(t, "DTOs", steps[0].Label)
}

func TestMigrationSteps(t *testing.T) {
	steps := migrationSteps()
	assert.Len(t, steps, 1)
	assert.Equal(t, "migration", steps[0].Label)
}

func TestRepositorySteps(t *testing.T) {
	steps := repositorySteps()
	assert.Len(t, steps, 4)
	assert.Equal(t, "model", steps[0].Label)
	assert.Equal(t, "repository", steps[3].Label)
}

func TestRouteSteps(t *testing.T) {
	steps := routeSteps()
	assert.Len(t, steps, 1)
	assert.Equal(t, "routes", steps[0].Label)
}

func TestResolverSteps(t *testing.T) {
	steps := resolverSteps()
	assert.Len(t, steps, 1)
}

func TestProviderSteps(t *testing.T) {
	steps := providerSteps()
	assert.Len(t, steps, 3)
}

func TestServiceSteps_REST(t *testing.T) {
	d := ScaffoldData{IncludeGraphQL: false}
	steps := serviceSteps(d)
	for _, s := range steps {
		assert.NotEqual(t, "GraphQL schema", s.Label)
		assert.NotEqual(t, "auto-wire: resolver", s.Label)
		assert.NotEqual(t, "regenerate gqlgen", s.Label)
	}
}

func TestServiceSteps_GraphQL(t *testing.T) {
	d := ScaffoldData{IncludeGraphQL: true}
	steps := serviceSteps(d)
	labels := make([]string, 0, len(steps))
	for _, s := range steps {
		labels = append(labels, s.Label)
	}
	assert.Contains(t, labels, "GraphQL schema")
	assert.Contains(t, labels, "auto-wire: resolver")
	assert.Contains(t, labels, "regenerate gqlgen")
}

func TestScaffoldSteps_REST(t *testing.T) {
	d := ScaffoldData{IncludeGraphQL: false}
	steps := scaffoldSteps(d)
	labels := make([]string, 0, len(steps))
	for _, s := range steps {
		labels = append(labels, s.Label)
	}
	assert.Contains(t, labels, "model")
	assert.Contains(t, labels, "controller")
	assert.Contains(t, labels, "routes")
	assert.NotContains(t, labels, "GraphQL schema")
}

func TestScaffoldSteps_GraphQL(t *testing.T) {
	d := ScaffoldData{IncludeGraphQL: true}
	steps := scaffoldSteps(d)
	labels := make([]string, 0, len(steps))
	for _, s := range steps {
		labels = append(labels, s.Label)
	}
	assert.Contains(t, labels, "GraphQL schema")
	assert.Contains(t, labels, "regenerate gqlgen")
}

func TestControllerSteps_REST(t *testing.T) {
	d := ScaffoldData{IncludeGraphQL: false}
	steps := controllerSteps(d)
	labels := make([]string, 0, len(steps))
	for _, s := range steps {
		labels = append(labels, s.Label)
	}
	assert.Contains(t, labels, "controller")
	assert.Contains(t, labels, "routes")
	assert.NotContains(t, labels, "GraphQL schema")
}

func TestControllerSteps_GraphQL(t *testing.T) {
	d := ScaffoldData{IncludeGraphQL: true}
	steps := controllerSteps(d)
	labels := make([]string, 0, len(steps))
	for _, s := range steps {
		labels = append(labels, s.Label)
	}
	assert.Contains(t, labels, "GraphQL schema")
}

func TestHasGraphQLFlag_False(t *testing.T) {
	cmd := scaffoldCmd
	// Reset flags for test
	assert.False(t, hasGraphQLFlag(cmd))
}

func TestBuildFromArgs(t *testing.T) {
	setupTempProject(t)
	d := buildFromArgs([]string{"product", "name:string"})
	assert.Equal(t, "Product", d.Name)
	assert.Len(t, d.Fields, 1)
}
