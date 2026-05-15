package commands

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateCmd_HasUpDown(t *testing.T) {
	subCmds := migrateCmd.Commands()
	names := make([]string, 0, len(subCmds))
	for _, c := range subCmds {
		names = append(names, c.Name())
	}
	assert.Contains(t, names, "up")
	assert.Contains(t, names, "down")
}

func TestMigrateCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "migrate" {
			found = true
			break
		}
	}
	assert.True(t, found, "migrateCmd should be registered on rootCmd")
}

func TestRunMigration_NoConfig(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	// loadConfig returns a koanf instance even without config.yaml,
	// so BuildMigrationURL returns a postgres URL with defaults
	err := runMigration("up")
	// Should fail because migrate binary is not available
	assert.Error(t, err)
}

func TestRunMigration_EmptyURL(t *testing.T) {
	// runMigration checks for empty URL and returns an error
	// This is hard to trigger since loadConfig always returns a koanf instance
	// but we can test the direction parameter
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	err := runMigration("down")
	assert.Error(t, err)
}

// TestRunMigration_EmptyURLSeam — the buildMigrationURL seam returns
// "" so the defensive "failed to load config" branch fires.
func TestRunMigration_EmptyURLSeam(t *testing.T) {
	orig := buildMigrationURL
	buildMigrationURL = func() string { return "" }
	t.Cleanup(func() { buildMigrationURL = orig })
	err := runMigration("up")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// TestRunMigration_EmptyURLCoverage — no config.yaml and no env vars
// so configutil's defaults produce a non-empty URL; the empty-URL
// branch is defensive. This test exercises the code path without a
// seam override.
func TestRunMigration_EmptyURLCoverage(t *testing.T) {
	chdirTemp(t)
	withFakeExec(t, 0)
	_ = runMigration("up")
}

// TestRunMigration_DownPassesSingleStep — regression: the migrate
// down command's docs promise single-step rollback, but the old
// implementation passed bare "down" which means "rollback ALL with a
// y/N prompt". With stdin not wired (the previous bug), the prompt
// auto-aborted and the command always failed. Now we always append
// "1" for the down direction so neither the prompt nor the all-
// rollback semantics surprise a developer.
func TestRunMigration_DownPassesSingleStep(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	var captured []string
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExecCommand(0)(name, args...)
	}
	t.Cleanup(func() { execCommand = orig })

	require.NoError(t, runMigration("down"))
	require.NotEmpty(t, captured, "execCommand should have been invoked")
	assert.Equal(t, "1", captured[len(captured)-1],
		"`migrate down` must pass '1' as the step count to avoid the all-rollback prompt")
}

// TestRunMigration_UpDoesNotPassCount — symmetric assertion: `up` does
// not append a step count, since "apply all pending" is the right
// semantics for the up direction.
func TestRunMigration_UpDoesNotPassCount(t *testing.T) {
	chdirTemp(t)
	writeConfigYAML(t)
	var captured []string
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExecCommand(0)(name, args...)
	}
	t.Cleanup(func() { execCommand = orig })

	require.NoError(t, runMigration("up"))
	assert.Equal(t, "up", captured[len(captured)-1],
		"`migrate up` must end with 'up' (no step count) so all pending migrations apply")
}

// TestRunMigration_LoadsDotEnv — regression for the bug where
// `gofasta migrate up` produced `postgres://:@localhost:5432/?sslmode=disable`
// because it never read .env. The scaffold's config.yaml intentionally
// omits user/password/name; .env supplies them via the project-prefixed
// env vars (e.g. ACME_DATABASE_USER). This test pins that runMigration
// loads .env BEFORE building the URL so the credentials show up in the
// -database argument passed to `migrate`.
func TestRunMigration_LoadsDotEnv(t *testing.T) {
	chdirTemp(t)
	// Scaffold-style config.yaml: no user/pass/name, host/port from yaml.
	scaffoldConfig := `database:
  driver: postgres
  host: localhost
  port: "5432"
  sslmode: disable
`
	require.NoError(t, os.WriteFile("config.yaml", []byte(scaffoldConfig), 0o644))
	require.NoError(t, os.WriteFile("go.mod",
		[]byte("module github.com/acme/myapp\n\ngo 1.25.0\n"), 0o644))

	// .env that overrides everything the way the dev workflow expects:
	// host port (5433) maps to container 5432, plus the credentials.
	dotenv := `MYAPP_DATABASE_USER=myappuser
MYAPP_DATABASE_PASSWORD=myapppass
MYAPP_DATABASE_NAME=myapp_dev
MYAPP_DATABASE_HOST=localhost
MYAPP_DATABASE_PORT=5433
`
	require.NoError(t, os.WriteFile(".env", []byte(dotenv), 0o644))
	// Clean up the env vars that loadDotEnv will os.Setenv so this
	// test doesn't leak state into sibling tests.
	for _, k := range []string{
		"MYAPP_DATABASE_USER", "MYAPP_DATABASE_PASSWORD",
		"MYAPP_DATABASE_NAME", "MYAPP_DATABASE_HOST", "MYAPP_DATABASE_PORT",
	} {
		t.Cleanup(func() { _ = os.Unsetenv(k) })
	}

	// Capture the exact args passed to the migrate shell-out so we can
	// assert the URL contains the .env values.
	var captured []string
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		captured = append([]string{name}, args...)
		return fakeExecCommand(0)(name, args...)
	}
	t.Cleanup(func() { execCommand = orig })

	require.NoError(t, runMigration("up"))

	// Find the -database arg (it follows -database in the captured slice).
	var dbURL string
	for i, a := range captured {
		if a == "-database" && i+1 < len(captured) {
			dbURL = captured[i+1]
			break
		}
	}
	require.NotEmpty(t, dbURL, "migrate should be invoked with -database <url>")
	assert.Contains(t, dbURL, "myappuser:myapppass@", "URL must include .env credentials, not empty :@")
	assert.Contains(t, dbURL, "localhost:5433", "URL must use the .env host:port mapping")
	assert.Contains(t, dbURL, "/myapp_dev", "URL must include .env database name")
	assert.NotContains(t, dbURL, "://:@", "URL must not have empty user/password")
	assert.NotContains(t, dbURL, "/?sslmode", "URL must include database name before query")
}
