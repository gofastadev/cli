package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDbCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "db" {
			found = true
			break
		}
	}
	assert.True(t, found, "dbCmd should be registered on rootCmd")
}

func TestDbResetCmd_Registered(t *testing.T) {
	subCmds := dbCmd.Commands()
	names := make([]string, 0, len(subCmds))
	for _, c := range subCmds {
		names = append(names, c.Name())
	}
	assert.Contains(t, names, "reset")
}

func TestDbResetCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, dbResetCmd.Short)
	assert.NotEmpty(t, dbResetCmd.Long)
}

func TestDbResetCmd_HasSkipSeedFlag(t *testing.T) {
	flag := dbResetCmd.Flags().Lookup("skip-seed")
	assert.NotNil(t, flag)
	assert.Equal(t, "false", flag.DefValue)
}

func TestRunDBReset_NoConfig(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	err := runDBReset(false)
	// Should fail because migrate binary is not available or DB unreachable
	assert.Error(t, err)
}

// TestRunDBReset_EmptyURL — the buildMigrationURL seam returns "" so
// the defensive "failed to load config" branch fires.
func TestRunDBReset_EmptyURL(t *testing.T) {
	orig := buildMigrationURL
	buildMigrationURL = func() string { return "" }
	t.Cleanup(func() { buildMigrationURL = orig })
	err := runDBReset(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// TestRunDBReset_JSON_Success — JSON mode emits a dbResetResult
// document; runDBStep's JSON branch (buffered stdout/stderr) is
// exercised too.
func TestRunDBReset_JSON_Success(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	withFakeExec(t, 0)
	withJSONMode(t)

	out := captureStdout(t, func() {
		require.NoError(t, runDBReset(true))
	})

	var got dbResetResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got))
	assert.NotEmpty(t, got.Steps)
}

// TestRunDBStep_JSONMode — runDBStep specifically: in JSON mode no
// step banner is printed and stdout/stderr are bagged in a buffer.
func TestRunDBStep_JSONMode(t *testing.T) {
	withJSONMode(t)
	withFakeExec(t, 0)
	require.NoError(t, runDBStep("test", "true"))
}

// TestPrintDBResetSummary_AllStatuses — exercises ok/fail/skip switch
// branches plus the final tally line.
func TestPrintDBResetSummary_AllStatuses(t *testing.T) {
	r := dbResetResult{
		Steps: []dbResetStep{
			{Name: "drop", Status: "ok", DurationMs: 12},
			{Name: "seed", Status: "fail", Message: "boom"},
			{Name: "extra", Status: "skip"},
		},
		DurationMs: 50,
	}
	var buf bytes.Buffer
	printDBResetSummary(&buf, r)
	out := buf.String()
	assert.Contains(t, out, "drop complete")
	assert.Contains(t, out, "seed failed: boom")
	assert.Contains(t, out, "extra skipped")
	assert.Contains(t, out, "3 step(s)")
}
