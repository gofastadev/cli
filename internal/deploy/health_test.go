package deploy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckHealth_DryRun(t *testing.T) {
	cfg := newTestCfg("docker")
	assert.NoError(t, CheckHealth(cfg))
}

func TestCheckHealth_Success(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	// fake exec success -> curl succeeds
	withFakeExec(t, 0)
	assert.NoError(t, CheckHealth(cfg))
}

func TestCheckHealth_Timeout(t *testing.T) {
	cfg := newTestCfg("docker")
	cfg.DryRun = false
	cfg.HealthTimeout = 0 // immediate timeout
	withFakeExec(t, 1)
	start := time.Now()
	err := CheckHealth(cfg)
	// should error and return quickly
	assert.Error(t, err)
	assert.Less(t, time.Since(start), 3*time.Second)
}

// TestCheckHealth_RetriesThenFails — health endpoint always returns
// non-2xx so CheckHealth retries then fails. Keep HealthTimeout small
// to avoid slowing the test.
func TestCheckHealth_RetriesThenFails(t *testing.T) {
	cfg := newTestCfg("binary")
	cfg.HealthTimeout = 1 // 1 second total budget
	cfg.DryRun = false
	// Provide an unreachable endpoint so the loop iterates then fails.
	cfg.Host = "127.0.0.1"
	cfg.ServerPort = "1" // definitely nothing listening here
	err := CheckHealth(cfg)
	require.Error(t, err)
}

func TestDryRun_CheckHealth(t *testing.T) {
	cfg := &DeployConfig{
		Host:          "user@server.com",
		Port:          22,
		ServerPort:    "8080",
		HealthPath:    "/health",
		HealthTimeout: 5,
		DryRun:        true,
	}
	err := CheckHealth(cfg)
	assert.NoError(t, err)
}
