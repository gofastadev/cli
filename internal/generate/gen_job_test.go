package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenJob_CreatesFile(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	err := GenJob(d)
	require.NoError(t, err)

	content := readTestFile(t, "app/jobs/product.go")
	assert.Contains(t, content, "ProductJob")
	assert.Contains(t, content, "product")
	assert.Contains(t, content, "package jobs")
}

func TestGenJob_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	writeTestFile(t, "app/jobs/product.go", "original")

	err := GenJob(d)
	require.NoError(t, err)
	assert.Equal(t, "original", readTestFile(t, "app/jobs/product.go"))
}

func TestPatchJobRegistry_AddsEntry(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	serveContent := `package cmd

	registry := map[string]scheduler.Job{
		// "cleanup-tokens": jobs.NewCleanupTokensJob(container.DB, logger),
	}

	for _, jobCfg := range configs {
`
	writeTestFile(t, "cmd/serve.go", serveContent)

	err := PatchJobRegistry(d)
	require.NoError(t, err)

	content := readTestFile(t, "cmd/serve.go")
	assert.Contains(t, content, `"product": jobs.NewProductJob(container.DB, logger)`)
}

func TestPatchJobRegistry_SkipsExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	serveContent := `package cmd
		"product": jobs.NewProductJob(container.DB, logger),
`
	writeTestFile(t, "cmd/serve.go", serveContent)

	err := PatchJobRegistry(d)
	require.NoError(t, err)
}

func TestPatchJobConfig_AddsToExisting(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	d.Schedule = "0 30 * * * *"

	writeTestFile(t, "config.yaml", "jobs:\n  - name: existing\n    schedule: \"0 0 * * * *\"\n")

	err := PatchJobConfig(d)
	require.NoError(t, err)

	content := readTestFile(t, "config.yaml")
	assert.Contains(t, content, "name: product")
	assert.Contains(t, content, `schedule: "0 30 * * * *"`)
}

func TestPatchJobConfig_CreatesSection(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	writeTestFile(t, "config.yaml", "server:\n  port: 8080\n")

	err := PatchJobConfig(d)
	require.NoError(t, err)

	content := readTestFile(t, "config.yaml")
	assert.Contains(t, content, "jobs:")
	assert.Contains(t, content, "name: product")
}

func TestPatchJobRegistry_FallbackInsertion(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()

	// serve.go without the marker comment but with the closing brace pattern
	serveContent := `package cmd

	registry := map[string]scheduler.Job{
	}

	for _, jobCfg := range configs {
`
	writeTestFile(t, "cmd/serve.go", serveContent)

	err := PatchJobRegistry(d)
	require.NoError(t, err)

	content := readTestFile(t, "cmd/serve.go")
	assert.Contains(t, content, "NewProductJob")
}

func TestPatchJobConfig_DefaultSchedule(t *testing.T) {
	setupTempProject(t)
	d := sampleScaffoldData()
	d.Schedule = "" // empty should default

	writeTestFile(t, "config.yaml", "jobs:\n")

	err := PatchJobConfig(d)
	require.NoError(t, err)

	content := readTestFile(t, "config.yaml")
	assert.Contains(t, content, `schedule: "0 0 * * * *"`)
}
