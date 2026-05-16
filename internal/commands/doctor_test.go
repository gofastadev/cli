package commands

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDoctorCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "doctor" {
			found = true
			break
		}
	}
	assert.True(t, found, "doctorCmd should be registered on rootCmd")
}

func TestDoctorCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, doctorCmd.Short)
	assert.NotEmpty(t, doctorCmd.Long)
}

func TestRunDoctor_ExecutesWithoutPanic(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	// Should not panic regardless of which tools are installed
	assert.NotPanics(t, func() {
		_ = runDoctor()
	})
}

func TestGoToolInstallHint(t *testing.T) {
	assert.Contains(t, goToolInstallHint("air"), "air-verse/air")
	assert.Contains(t, goToolInstallHint("wire"), "google/wire")
	assert.Contains(t, goToolInstallHint("unknown"), "unknown@latest")
}

// TestRunDoctor_LoadsDotEnvBeforeDBCheck — regression for the bug
// where `gofasta doctor` always reported "database not reachable"
// because it built the migration URL from config.yaml only (host-port
// defaults: 5432) without overlaying the project's `.env` (which
// remaps to the docker-mapped host port, e.g. 5433). This test
// confirms doctor's .env-loading step actually mutates the process
// env BEFORE the migration URL gets built.
func TestRunDoctor_LoadsDotEnvBeforeDBCheck(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	_ = os.Chdir(dir)

	// Minimal scaffold-shaped layout: config.yaml present, .env present
	// with a value that doctor must read into the process env.
	require := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	require(os.WriteFile("config.yaml", []byte("database:\n  driver: postgres\n  host: localhost\n  port: \"5432\"\n"), 0o644))
	require(os.WriteFile(".env", []byte("GOFASTA_DATABASE_PORT=5433\n"), 0o644))

	t.Cleanup(func() { _ = os.Unsetenv("GOFASTA_DATABASE_PORT") })
	_ = os.Unsetenv("GOFASTA_DATABASE_PORT")

	assert.NotPanics(t, func() { _ = runDoctor() })

	// The bug was that doctor never loaded .env at all; this assertion
	// proves the side-effect (the env var IS now set in the process)
	// regardless of whether the migrate child process succeeded.
	assert.Equal(t, "5433", os.Getenv("GOFASTA_DATABASE_PORT"),
		"doctor must load .env so the DB-reachability probe uses the host-mapped port, not config.yaml's in-container default")
}

// TestPrintDoctorSection_DefaultStatus — entries with a status that
// isn't "ok" or "fail" (e.g. "skip", "warn", unknown) fall through to
// the default switch arm. Covered nowhere else because all live
// doctorEntry producers emit only "ok"/"fail".
func TestPrintDoctorSection_DefaultStatus(t *testing.T) {
	var buf bytes.Buffer
	printDoctorSection(&buf, "Skipped:", []doctorEntry{
		{Status: "skip", Name: "thing", Message: "n/a"},
	})
	out := buf.String()
	assert.Contains(t, out, "Skipped:")
	assert.Contains(t, out, "thing")
	assert.Contains(t, out, "n/a")
}

// TestPrintDoctorSection_AllStatuses — exercises ok / fail / default
// in a single call so the three switch arms are hit in one place
// independent of which check produced what.
func TestPrintDoctorSection_AllStatuses(t *testing.T) {
	var buf bytes.Buffer
	printDoctorSection(&buf, "Mixed:", []doctorEntry{
		{Status: "ok", Name: "good", Message: "running"},
		{Status: "fail", Name: "broken", Message: "missing"},
		{Status: "skip", Name: "unknown", Message: "tbd"},
	})
	out := buf.String()
	assert.Contains(t, out, "good")
	assert.Contains(t, out, "broken")
	assert.Contains(t, out, "unknown")
}
