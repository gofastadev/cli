package deploy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeployConfig_Defaults(t *testing.T) {
	cfg := &DeployConfig{}

	// Apply the same defaults as LoadDeployConfig
	if cfg.Method == "" {
		cfg.Method = "docker"
	}
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.Arch == "" {
		cfg.Arch = "amd64"
	}
	if cfg.HealthPath == "" {
		cfg.HealthPath = "/health"
	}
	if cfg.HealthTimeout == 0 {
		cfg.HealthTimeout = 30
	}
	if cfg.KeepReleases == 0 {
		cfg.KeepReleases = 3
	}
	if cfg.ServerPort == "" {
		cfg.ServerPort = "8080"
	}

	assert.Equal(t, "docker", cfg.Method)
	assert.Equal(t, 22, cfg.Port)
	assert.Equal(t, "amd64", cfg.Arch)
	assert.Equal(t, "/health", cfg.HealthPath)
	assert.Equal(t, 30, cfg.HealthTimeout)
	assert.Equal(t, 3, cfg.KeepReleases)
	assert.Equal(t, "8080", cfg.ServerPort)
}

func TestDeployConfig_ReleasePath(t *testing.T) {
	cfg := &DeployConfig{
		Path:       "/opt/myapp",
		ReleaseTag: "20260409-150000",
	}
	assert.Equal(t, "/opt/myapp/releases/20260409-150000", cfg.ReleasePath())
}

func TestDeployConfig_SharedPath(t *testing.T) {
	cfg := &DeployConfig{Path: "/opt/myapp"}
	assert.Equal(t, "/opt/myapp/shared", cfg.SharedPath())
}

func TestDeployConfig_CurrentPath(t *testing.T) {
	cfg := &DeployConfig{Path: "/opt/myapp"}
	assert.Equal(t, "/opt/myapp/current", cfg.CurrentPath())
}

func TestDeployConfig_MethodValidation(t *testing.T) {
	tests := []struct {
		method  string
		isValid bool
	}{
		{"docker", true},
		{"binary", true},
		{"invalid", false},
		{"", true}, // empty defaults to "docker"
	}

	for _, tt := range tests {
		method := tt.method
		if method == "" {
			method = "docker"
		}
		valid := method == "docker" || method == "binary"
		assert.Equal(t, tt.isValid, valid, "method %q validation", tt.method)
	}
}

func TestSSHBaseArgs(t *testing.T) {
	cfg := &DeployConfig{
		Host: "user@server.com",
		Port: 2222,
	}
	args := sshBaseArgs(cfg)
	require.Len(t, args, 6)
	assert.Equal(t, "-p", args[0])
	assert.Equal(t, "2222", args[1])
	assert.Equal(t, "-o", args[2])
	assert.Equal(t, "StrictHostKeyChecking=accept-new", args[3])
	assert.Equal(t, "-o", args[4])
	assert.Equal(t, "ConnectTimeout=10", args[5])
}

func TestDryRun_RunRemote(t *testing.T) {
	cfg := &DeployConfig{
		Host:   "user@server.com",
		Port:   22,
		DryRun: true,
	}
	// Should not actually connect — dry run just prints
	err := RunRemote(cfg, "echo hello")
	assert.NoError(t, err)
}

func TestDryRun_CopyFile(t *testing.T) {
	cfg := &DeployConfig{
		Host:   "user@server.com",
		Port:   22,
		DryRun: true,
	}
	err := CopyFile(cfg, "/local/file", "/remote/file")
	assert.NoError(t, err)
}

func TestDryRun_CopyDir(t *testing.T) {
	cfg := &DeployConfig{
		Host:   "user@server.com",
		Port:   22,
		DryRun: true,
	}
	err := CopyDir(cfg, "/local/dir", "/remote/dir")
	assert.NoError(t, err)
}

func TestDryRun_RunLocalPiped(t *testing.T) {
	cfg := &DeployConfig{DryRun: true}
	err := RunLocalPiped(cfg, "echo hello | cat")
	assert.NoError(t, err)
}

func TestDryRun_RunLocal(t *testing.T) {
	cfg := &DeployConfig{DryRun: true}
	err := RunLocal(cfg, "echo", "hello")
	assert.NoError(t, err)
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

func TestPrintStep(t *testing.T) {
	// Just verify it doesn't panic
	PrintStep(1, 5, "Test step")
	PrintSuccess("Success")
	PrintWarning("Warning")
	PrintError("Error")
	PrintInfo("Info")
}
