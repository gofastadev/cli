package commands

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServeCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "serve" {
			found = true
			break
		}
	}
	assert.True(t, found, "serveCmd should be registered on rootCmd")
}

func TestServeCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, serveCmd.Short)
}

func TestServeCmd_RunE_Fails(t *testing.T) {
	// serveCmd delegates to `go run ./app/main serve` which won't exist
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	err := serveCmd.RunE(serveCmd, nil)
	assert.Error(t, err)
}
