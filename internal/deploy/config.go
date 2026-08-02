package deploy

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/cobra"
)

// DeployConfig holds all deployment configuration.
//
//nolint:revive // name kept for public-API stability; rename is a breaking change.
type DeployConfig struct {
	Host          string
	Method        string
	Port          int
	Path          string
	Arch          string
	Domain        string // public domain for the nginx vhost (empty = skip nginx in setup)
	HealthPath    string
	HealthTimeout int
	KeepReleases  int
	DryRun        bool
	AppName       string
	ServerPort    string
	ReleaseTag    string
	StrictHostKey string // ssh StrictHostKeyChecking value (default "accept-new")
}

// Every one of these values ends up interpolated into a remote shell command
// or a local argv. Host and AppName were validated first (CVE-2017-1000117
// class); the rest of the patterns close the same hole for the remaining
// config-sourced strings — a deploy.path of "/opt/x; rm -rf /" must be
// rejected at load time, not executed on the server.
var (
	// deployHostPattern accepts an optional user@ prefix followed by a hostname or
	// IP. It deliberately forbids a leading "-" and shell metacharacters so a Host
	// read from a (possibly untrusted, cloned) config.yaml cannot be parsed by ssh
	// as an option or smuggle shell syntax.
	deployHostPattern = regexp.MustCompile(`^([A-Za-z0-9._-]+@)?[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)

	// deployAppNamePattern constrains the go.mod-derived app name (which is
	// interpolated into image tags and remote commands) to safe characters.
	deployAppNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

	// deployPathPattern requires an absolute path with no shell metacharacters.
	deployPathPattern = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)

	// deployHealthPathPattern requires a rooted URL path with safe characters.
	deployHealthPathPattern = regexp.MustCompile(`^/[A-Za-z0-9._/-]*$`)

	// deployDomainPattern is a hostname (no scheme, no port, no metacharacters).
	deployDomainPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)

	// deployServerPortPattern — the app port is spliced into probe URLs.
	deployServerPortPattern = regexp.MustCompile(`^\d{1,5}$`)
)

// LoadDeployConfig reads deploy config from config.yaml, overlays env vars, then CLI flags.
//
//nolint:gocyclo // flat sequence of flag-or-default checks; refactoring adds indirection.
func LoadDeployConfig(cmd *cobra.Command) (*DeployConfig, error) {
	k := loadKoanf()

	cfg := &DeployConfig{
		Host:          k.String("deploy.host"),
		Method:        k.String("deploy.method"),
		Port:          k.Int("deploy.port"),
		Path:          k.String("deploy.path"),
		Arch:          k.String("deploy.arch"),
		Domain:        k.String("deploy.domain"),
		HealthPath:    k.String("deploy.health_path"),
		HealthTimeout: k.Int("deploy.health_timeout"),
		KeepReleases:  k.Int("deploy.keep_releases"),
		ServerPort:    k.String("server.port"),
		StrictHostKey: k.String("deploy.strict_host_key"),
	}

	// Apply CLI flag overrides (only if explicitly set)
	if cmd != nil {
		if v, _ := cmd.Flags().GetString("host"); v != "" {
			cfg.Host = v
		}
		if v, _ := cmd.Flags().GetString("method"); v != "" {
			cfg.Method = v
		}
		if v, _ := cmd.Flags().GetInt("port"); v != 0 {
			cfg.Port = v
		}
		if v, _ := cmd.Flags().GetString("path"); v != "" {
			cfg.Path = v
		}
		if v, _ := cmd.Flags().GetString("arch"); v != "" {
			cfg.Arch = v
		}
		if v, _ := cmd.Flags().GetBool("dry-run"); v {
			cfg.DryRun = true
		}
	}

	// Apply defaults
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
		// /health/ready (not /health): the deploy gate must check real
		// readiness — DB and cache connectivity — not just process liveness.
		// Matches the scaffold's config.yaml default.
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

	// Derive app name from go.mod
	appName, err := readAppName()
	if err != nil {
		return nil, clierr.Wrap(clierr.CodeDeployConfig, err, "could not determine app name")
	}
	cfg.AppName = appName

	if cfg.Path == "" {
		cfg.Path = "/opt/" + cfg.AppName
	}

	// Generate release tag
	cfg.ReleaseTag = time.Now().UTC().Format("20060102-150405")

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate rejects any value that could smuggle shell syntax into a remote
// command, plus plain misconfiguration. Called from LoadDeployConfig; split
// out so the checks read as one block.
//
//nolint:gocyclo // deliberately a flat list of independent field checks.
func (c *DeployConfig) validate() error {
	if c.Host == "" {
		return clierr.New(clierr.CodeDeployHostRequired,
			"deploy host is required — set deploy.host in config.yaml or use --host flag")
	}
	if !deployHostPattern.MatchString(c.Host) {
		return clierr.Newf(clierr.CodeDeployConfig,
			"invalid deploy.host %q — must be a hostname, IP, or user@host (no leading '-' or shell metacharacters)", c.Host)
	}
	if !deployAppNamePattern.MatchString(c.AppName) {
		return clierr.Newf(clierr.CodeDeployConfig,
			"invalid app name %q derived from go.mod — must match [A-Za-z0-9._-]", c.AppName)
	}
	if c.Method != "docker" && c.Method != "binary" {
		return clierr.Newf(clierr.CodeDeployConfig,
			"deploy method must be 'docker' or 'binary', got %q", c.Method)
	}
	if c.Port < 1 || c.Port > 65535 {
		return clierr.Newf(clierr.CodeDeployConfig,
			"invalid deploy.port %d — must be 1-65535", c.Port)
	}
	if !deployPathPattern.MatchString(c.Path) {
		return clierr.Newf(clierr.CodeDeployConfig,
			"invalid deploy.path %q — must be an absolute path using [A-Za-z0-9._/-]", c.Path)
	}
	if c.Arch != "amd64" && c.Arch != "arm64" {
		return clierr.Newf(clierr.CodeDeployConfig,
			"invalid deploy.arch %q — must be amd64 or arm64", c.Arch)
	}
	if c.Domain != "" && !deployDomainPattern.MatchString(c.Domain) {
		return clierr.Newf(clierr.CodeDeployConfig,
			"invalid deploy.domain %q — must be a bare hostname (no scheme, port, or path)", c.Domain)
	}
	if !deployHealthPathPattern.MatchString(c.HealthPath) {
		return clierr.Newf(clierr.CodeDeployConfig,
			"invalid deploy.health_path %q — must be a rooted path using [A-Za-z0-9._/-]", c.HealthPath)
	}
	if !deployServerPortPattern.MatchString(c.ServerPort) {
		return clierr.Newf(clierr.CodeDeployConfig,
			"invalid server.port %q — must be numeric", c.ServerPort)
	}
	switch c.StrictHostKey {
	case "", "yes", "no", "accept-new":
	default:
		return clierr.Newf(clierr.CodeDeployConfig,
			"invalid deploy.strict_host_key %q — must be yes, no, or accept-new", c.StrictHostKey)
	}
	return nil
}

// ReleasePath returns the full path for the current release on the remote server.
func (c *DeployConfig) ReleasePath() string {
	return filepath.Join(c.Path, "releases", c.ReleaseTag)
}

// SharedPath returns the shared directory path on the remote server.
func (c *DeployConfig) SharedPath() string {
	return filepath.Join(c.Path, "shared")
}

// CurrentPath returns the current symlink path on the remote server.
func (c *DeployConfig) CurrentPath() string {
	return filepath.Join(c.Path, "current")
}

// ReleasesDir returns the directory that holds all releases on the remote server.
func (c *DeployConfig) ReleasesDir() string {
	return filepath.Join(c.Path, "releases")
}

// ComposeProject returns the fixed Docker Compose project name for this app.
// A stable -p value is what keeps container names, networks, and the data
// volume identical across releases. Compose requires lowercase
// [a-z0-9][a-z0-9_-]*, so the go.mod-derived AppName is normalized.
func (c *DeployConfig) ComposeProject() string {
	name := strings.ToLower(c.AppName)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.TrimLeft(b.String(), "_-")
	if out == "" {
		out = "app"
	}
	return out
}

// EnvPrefix returns the project's environment-variable prefix (the scaffold's
// {{.ProjectNameUpper}}): uppercased AppName with non-alphanumerics mapped to
// "_". Used by the binary-method health probe to honor a server-side
// <PREFIX>_SERVER_PORT override from shared/.env.
func (c *DeployConfig) EnvPrefix() string {
	var b strings.Builder
	for _, r := range strings.ToUpper(c.AppName) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func readAppName() (string, error) {
	f, err := os.Open("go.mod")
	if err != nil {
		return "", fmt.Errorf("cannot read go.mod: %w (are you in a gofasta project directory?)", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if mod, ok := strings.CutPrefix(line, "module "); ok {
			// Extract the last segment of the module path
			parts := strings.Split(mod, "/")
			return parts[len(parts)-1], nil
		}
	}
	return "", fmt.Errorf("no module directive found in go.mod")
}

func loadKoanf() *koanf.Koanf {
	k := koanf.New(".")
	if _, err := os.Stat("config.yaml"); err == nil {
		_ = k.Load(file.Provider("config.yaml"), yaml.Parser())
	}
	// Only the FIRST underscore separates section from key: the deploy keys
	// are single-section ("deploy.health_path"), so GOFASTA_DEPLOY_HEALTH_PATH
	// must map to deploy.health_path — the previous every-underscore transform
	// produced deploy.health.path, which nothing reads, making every
	// multi-word key silently unreachable via env.
	_ = k.Load(env.Provider("GOFASTA_", ".", func(s string) string {
		key := strings.ToLower(strings.TrimPrefix(s, "GOFASTA_"))
		parts := strings.SplitN(key, "_", 2)
		return strings.Join(parts, ".")
	}), nil)
	return k
}
