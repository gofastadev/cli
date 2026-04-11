package deploy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chdirProject(t *testing.T, configYAML string) {
	t.Helper()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	require.NoError(t, os.WriteFile("go.mod", []byte("module github.com/test/myapp\n\ngo 1.21\n"), 0o644))
	if configYAML != "" {
		require.NoError(t, os.WriteFile("config.yaml", []byte(configYAML), 0o644))
	}
}

func makeFlagCmd(flags map[string]string) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().String("host", "", "")
	c.Flags().String("method", "", "")
	c.Flags().Int("port", 0, "")
	c.Flags().String("path", "", "")
	c.Flags().String("arch", "", "")
	c.Flags().Bool("dry-run", false, "")
	for k, v := range flags {
		c.Flags().Set(k, v)
	}
	return c
}

func TestLoadDeployConfig_FromYAML(t *testing.T) {
	chdirProject(t, `deploy:
  host: deploy@server.com
  method: docker
  port: 2222
  path: /srv/app
  arch: arm64
  health_path: /healthz
  health_timeout: 60
  keep_releases: 5
server:
  port: "9090"
`)
	cfg, err := LoadDeployConfig(nil)
	require.NoError(t, err)
	assert.Equal(t, "deploy@server.com", cfg.Host)
	assert.Equal(t, "docker", cfg.Method)
	assert.Equal(t, 2222, cfg.Port)
	assert.Equal(t, "/srv/app", cfg.Path)
	assert.Equal(t, "arm64", cfg.Arch)
	assert.Equal(t, "/healthz", cfg.HealthPath)
	assert.Equal(t, 60, cfg.HealthTimeout)
	assert.Equal(t, 5, cfg.KeepReleases)
	assert.Equal(t, "9090", cfg.ServerPort)
	assert.Equal(t, "myapp", cfg.AppName)
	assert.NotEmpty(t, cfg.ReleaseTag)
}

func TestLoadDeployConfig_Defaults(t *testing.T) {
	chdirProject(t, `deploy:
  host: host
`)
	cfg, err := LoadDeployConfig(nil)
	require.NoError(t, err)
	assert.Equal(t, "docker", cfg.Method)
	assert.Equal(t, 22, cfg.Port)
	assert.Equal(t, "amd64", cfg.Arch)
	assert.Equal(t, "/health", cfg.HealthPath)
	assert.Equal(t, 30, cfg.HealthTimeout)
	assert.Equal(t, 3, cfg.KeepReleases)
	assert.Equal(t, "8080", cfg.ServerPort)
	assert.Equal(t, "/opt/myapp", cfg.Path)
}

func TestLoadDeployConfig_FlagOverrides(t *testing.T) {
	chdirProject(t, `deploy:
  host: original.com
`)
	cmd := makeFlagCmd(map[string]string{
		"host":    "flag-host.com",
		"method":  "binary",
		"port":    "2200",
		"path":    "/flag/path",
		"arch":    "arm64",
		"dry-run": "true",
	})
	cfg, err := LoadDeployConfig(cmd)
	require.NoError(t, err)
	assert.Equal(t, "flag-host.com", cfg.Host)
	assert.Equal(t, "binary", cfg.Method)
	assert.Equal(t, 2200, cfg.Port)
	assert.Equal(t, "/flag/path", cfg.Path)
	assert.Equal(t, "arm64", cfg.Arch)
	assert.True(t, cfg.DryRun)
}

func TestLoadDeployConfig_MissingHost(t *testing.T) {
	chdirProject(t, `deploy:
  method: docker
`)
	_, err := LoadDeployConfig(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "host")
}

func TestLoadDeployConfig_InvalidMethod(t *testing.T) {
	chdirProject(t, `deploy:
  host: h
  method: weird
`)
	_, err := LoadDeployConfig(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "method")
}

func TestLoadDeployConfig_NoGoMod(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	os.WriteFile("config.yaml", []byte("deploy:\n  host: h\n"), 0644)
	_, err := LoadDeployConfig(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "app name")
}

func TestLoadDeployConfig_EnvOverride(t *testing.T) {
	chdirProject(t, `deploy:
  host: yaml-host
`)
	t.Setenv("GOFASTA_DEPLOY_HOST", "env-host")
	cfg, err := LoadDeployConfig(nil)
	require.NoError(t, err)
	assert.Equal(t, "env-host", cfg.Host)
}

func TestLoadDeployConfigLax_MissingHost(t *testing.T) {
	chdirProject(t, `deploy:
  method: docker
`)
	// LoadDeployConfigLax returns the same error because LoadDeployConfig
	// returns a nil cfg when host is missing. Test that lax passes through.
	_, err := LoadDeployConfigLax(nil)
	assert.Error(t, err)
}

func TestLoadDeployConfigLax_Success(t *testing.T) {
	chdirProject(t, `deploy:
  host: h
  method: docker
`)
	cfg, err := LoadDeployConfigLax(nil)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
}

func TestLoadDeployConfigLax_InvalidMethod(t *testing.T) {
	chdirProject(t, `deploy:
  host: h
  method: weird
`)
	_, err := LoadDeployConfigLax(nil)
	// Lax mode still rejects invalid method
	assert.Error(t, err)
}

func TestReadAppName_NoGoMod(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	_, err := readAppName()
	assert.Error(t, err)
}

func TestReadAppName_NoModuleDirective(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	require.NoError(t, os.WriteFile("go.mod", []byte("go 1.21\n"), 0644))
	_, err := readAppName()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "module directive")
}

func TestReadAppName_SimpleModule(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	require.NoError(t, os.WriteFile("go.mod", []byte("module plainapp\n"), 0644))
	name, err := readAppName()
	require.NoError(t, err)
	assert.Equal(t, "plainapp", name)
}

func TestLoadKoanf_NoConfigFile(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))
	k := loadKoanf()
	assert.NotNil(t, k)
}

// Ensure ReleasePath, SharedPath, CurrentPath filepath join works.
func TestDeployConfig_PathsFilepath(t *testing.T) {
	cfg := &DeployConfig{Path: filepath.Join("opt", "app"), ReleaseTag: "tag1"}
	assert.Contains(t, cfg.ReleasePath(), "tag1")
	assert.Contains(t, cfg.SharedPath(), "shared")
	assert.Contains(t, cfg.CurrentPath(), "current")
}
