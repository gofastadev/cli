package generate

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunSteps_AllSucceed(t *testing.T) {
	called := make([]string, 0)
	steps := []Step{
		{"step1", func(d ScaffoldData) error { called = append(called, "step1"); return nil }},
		{"step2", func(d ScaffoldData) error { called = append(called, "step2"); return nil }},
		{"step3", func(d ScaffoldData) error { called = append(called, "step3"); return nil }},
	}
	d := ScaffoldData{}

	err := RunSteps(d, steps)
	require.NoError(t, err)
	assert.Equal(t, []string{"step1", "step2", "step3"}, called)
}

func TestRunSteps_StopsOnError(t *testing.T) {
	called := make([]string, 0)
	steps := []Step{
		{"step1", func(d ScaffoldData) error { called = append(called, "step1"); return nil }},
		{"step2", func(d ScaffoldData) error { return errors.New("boom") }},
		{"step3", func(d ScaffoldData) error { called = append(called, "step3"); return nil }},
	}

	err := RunSteps(ScaffoldData{}, steps)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "step2")
	assert.Contains(t, err.Error(), "boom")
	assert.Equal(t, []string{"step1"}, called)
}

func TestRunSteps_EmptySteps(t *testing.T) {
	err := RunSteps(ScaffoldData{}, nil)
	assert.NoError(t, err)
}

func TestRunSteps_ErrorMessage(t *testing.T) {
	steps := []Step{
		{"generate model", func(d ScaffoldData) error { return errors.New("template error") }},
	}

	err := RunSteps(ScaffoldData{}, steps)
	assert.EqualError(t, err, "failed at generate model: template error")
}

func TestRunSteps_PassesDataToSteps(t *testing.T) {
	var receivedName string
	steps := []Step{
		{"check", func(d ScaffoldData) error { receivedName = d.Name; return nil }},
	}

	RunSteps(ScaffoldData{Name: "Product"}, steps)
	assert.Equal(t, "Product", receivedName)
}

func TestRunWire_FailsWithoutGoTool(t *testing.T) {
	setupTempProject(t)
	// RunWire calls exec.Command("go", "tool", "wire", "./app/di/")
	// Without wire installed as a tool, it should fail
	err := RunWire(ScaffoldData{})
	assert.Error(t, err)
}

func TestRunGqlgen_FailsWithoutGoTool(t *testing.T) {
	setupTempProject(t)
	// RunGqlgen calls exec.Command("go", "tool", "gqlgen", "generate")
	// Without gqlgen installed as a tool, it should fail
	err := RunGqlgen(ScaffoldData{})
	assert.Error(t, err)
}
