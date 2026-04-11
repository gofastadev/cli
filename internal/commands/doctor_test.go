package commands

import (
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
