package commands

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSwaggerCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "swagger" {
			found = true
			break
		}
	}
	assert.True(t, found, "swaggerCmd should be registered on rootCmd")
}

func TestSwaggerCmd_HasDescription(t *testing.T) {
	assert.NotEmpty(t, swaggerCmd.Short)
}

func TestSwaggerCmd_RunE_Fails(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	err := swaggerCmd.RunE(swaggerCmd, nil)
	assert.Error(t, err)
}
