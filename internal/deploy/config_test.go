package deploy

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gofastadev/cli/internal/clierr"
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
	assert.Equal(t, "/health/ready", cfg.HealthPath)
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

// TestLoadDeployConfig_RejectsUnsafeHost covers the deployHostPattern branch.
// A host is interpolated into an ssh argv, so a value starting with "-" would
// be read by ssh as an option — "-oProxyCommand=..." executes a local command
// (CVE-2017-1000117 class). config.yaml can come from a cloned repo, so this
// rejection is a security boundary, not input tidiness.
func TestLoadDeployConfig_RejectsUnsafeHost(t *testing.T) {
	unsafe := map[string]string{
		"leading dash option":  "-oProxyCommand=touch /tmp/pwned",
		"shell metacharacter":  "server.com; rm -rf /",
		"command substitution": "$(whoami)@server.com",
		"space separated":      "server.com extra",
		"backtick":             "`id`",
	}

	for name, host := range unsafe {
		t.Run(name, func(t *testing.T) {
			chdirProject(t, "deploy:\n  host: \""+host+"\"\n  method: docker\n")
			_, err := LoadDeployConfig(nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid deploy.host")
		})
	}
}

// TestLoadDeployConfig_AcceptsValidHosts is the counterpart, so the pattern
// cannot be tightened into rejecting ordinary hosts without a test failing.
func TestLoadDeployConfig_AcceptsValidHosts(t *testing.T) {
	for _, host := range []string{"server.com", "user@server.com", "10.0.0.1", "deploy-1.eu-west.example.com"} {
		t.Run(host, func(t *testing.T) {
			chdirProject(t, "deploy:\n  host: \""+host+"\"\n  method: docker\n")
			cfg, err := LoadDeployConfig(nil)
			require.NoError(t, err)
			assert.Equal(t, host, cfg.Host)
		})
	}
}

// TestLoadDeployConfig_RejectsUnsafeAppName covers the deployAppNamePattern
// branch. The app name is derived from go.mod's module path and lands in image
// tags and remote command strings, so the same argument-injection reasoning
// applies to a hostile module line.
func TestLoadDeployConfig_RejectsUnsafeAppName(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(dir))

	// The last path segment becomes the app name.
	require.NoError(t, os.WriteFile("go.mod", []byte("module github.com/test/app;rm -rf /\n\ngo 1.21\n"), 0o644))
	require.NoError(t, os.WriteFile("config.yaml", []byte("deploy:\n  host: server.com\n  method: docker\n"), 0o644))

	_, err = LoadDeployConfig(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid app name")
}

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
		cfg.HealthPath = "/health/ready"
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
	assert.Equal(t, "/health/ready", cfg.HealthPath)
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

func TestDeployHelperProcess(t *testing.T) {
	if os.Getenv("GOFASTA_WANT_DEPLOY_HELPER") != "1" {
		return
	}
	if out := os.Getenv(fakeEnvStdout); out != "" {
		os.Stdout.WriteString(out)
	}
	code, _ := strconv.Atoi(os.Getenv(fakeEnvExitCode))
	os.Exit(code)
}

// ── Validation: every config value interpolated into a remote shell must
// reject metacharacters at load time, with the right clierr code. ──

func TestLoadDeployConfig_RejectsHostileValues(t *testing.T) {
	cases := []struct {
		name   string
		config string
	}{
		{"path with semicolon", "deploy:\n  host: host\n  path: \"/opt/x; rm -rf /\"\n"},
		{"path with spaces", "deploy:\n  host: host\n  path: \"/opt/x $(whoami)\"\n"},
		{"relative path", "deploy:\n  host: host\n  path: \"opt/x\"\n"},
		{"arch injection", "deploy:\n  host: host\n  arch: \"amd64; rm -rf /\"\n"},
		{"unknown arch", "deploy:\n  host: host\n  arch: riscv\n"},
		{"health path injection", "deploy:\n  host: host\n  health_path: \"/health; reboot\"\n"},
		{"domain injection", "deploy:\n  host: host\n  domain: \"example.com; reboot\"\n"},
		{"domain with scheme", "deploy:\n  host: host\n  domain: \"https://example.com\"\n"},
		{"strict host key injection", "deploy:\n  host: host\n  strict_host_key: \"no -oProxyCommand=payload\"\n"},
		{"server port injection", "deploy:\n  host: host\nserver:\n  port: \"8080; reboot\"\n"},
		{"ssh port out of range", "deploy:\n  host: host\n  port: 70000\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chdirProject(t, tc.config)
			_, err := LoadDeployConfig(nil)
			require.Error(t, err, "hostile value must be rejected at load time")
			ce, ok := clierr.As(err)
			require.True(t, ok, "validation errors must carry a clierr code")
			assert.Equal(t, string(clierr.CodeDeployConfig), ce.Code)
		})
	}
}

func TestLoadDeployConfig_MissingHostCode(t *testing.T) {
	chdirProject(t, "")
	_, err := LoadDeployConfig(nil)
	require.Error(t, err)
	ce, ok := clierr.As(err)
	require.True(t, ok)
	assert.Equal(t, string(clierr.CodeDeployHostRequired), ce.Code)
}

func TestLoadDeployConfig_AcceptsDomainAndStrictHostKey(t *testing.T) {
	chdirProject(t, "deploy:\n  host: host\n  domain: api.example.com\n  strict_host_key: \"yes\"\n")
	cfg, err := LoadDeployConfig(nil)
	require.NoError(t, err)
	assert.Equal(t, "api.example.com", cfg.Domain)
	assert.Equal(t, "yes", cfg.StrictHostKey)
}

// ── Env overlay: multi-word keys were unreachable when every underscore
// became a dot (GOFASTA_DEPLOY_HEALTH_PATH → deploy.health.path). Only the
// FIRST underscore separates section from key. ──

func TestLoadDeployConfig_EnvOverlay_MultiWordKeys(t *testing.T) {
	chdirProject(t, "deploy:\n  host: host\n")
	t.Setenv("GOFASTA_DEPLOY_HEALTH_PATH", "/env/health")
	t.Setenv("GOFASTA_DEPLOY_HEALTH_TIMEOUT", "77")
	t.Setenv("GOFASTA_DEPLOY_KEEP_RELEASES", "9")
	t.Setenv("GOFASTA_DEPLOY_STRICT_HOST_KEY", "no")

	cfg, err := LoadDeployConfig(nil)
	require.NoError(t, err)
	assert.Equal(t, "/env/health", cfg.HealthPath)
	assert.Equal(t, 77, cfg.HealthTimeout)
	assert.Equal(t, 9, cfg.KeepReleases)
	assert.Equal(t, "no", cfg.StrictHostKey)
}

// ── Derived helpers ──

func TestComposeProject_NormalizesAppName(t *testing.T) {
	for in, want := range map[string]string{
		"myapp":    "myapp",
		"My.App":   "my-app",
		"_leading": "leading",
		"API_v2":   "api_v2",
		"...":      "app",
	} {
		cfg := &DeployConfig{AppName: in}
		assert.Equal(t, want, cfg.ComposeProject(), "AppName %q", in)
	}
}

func TestEnvPrefix_MirrorsScaffoldUpper(t *testing.T) {
	for in, want := range map[string]string{
		"myapp":  "MYAPP",
		"my-app": "MY_APP",
		"api.v2": "API_V2",
	} {
		cfg := &DeployConfig{AppName: in}
		assert.Equal(t, want, cfg.EnvPrefix(), "AppName %q", in)
	}
}
