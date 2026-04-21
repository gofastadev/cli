package deploy

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	HealthPath    string
	HealthTimeout int
	KeepReleases  int
	DryRun        bool
	AppName       string
	ServerPort    string
	ReleaseTag    string
}

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
		HealthPath:    k.String("deploy.health_path"),
		HealthTimeout: k.Int("deploy.health_timeout"),
		KeepReleases:  k.Int("deploy.keep_releases"),
		ServerPort:    k.String("server.port"),
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

	// Derive app name from go.mod
	appName, err := readAppName()
	if err != nil {
		return nil, fmt.Errorf("could not determine app name: %w", err)
	}
	cfg.AppName = appName

	if cfg.Path == "" {
		cfg.Path = "/opt/" + cfg.AppName
	}

	// Generate release tag
	cfg.ReleaseTag = time.Now().UTC().Format("20060102-150405")

	// Validate
	if cfg.Host == "" {
		return nil, fmt.Errorf("deploy host is required — set deploy.host in config.yaml or use --host flag")
	}
	if cfg.Method != "docker" && cfg.Method != "binary" {
		return nil, fmt.Errorf("deploy method must be 'docker' or 'binary', got %q", cfg.Method)
	}

	return cfg, nil
}

// loadDeployConfigForLax is a seam over LoadDeployConfig so tests can
// exercise the "host-required swallow" branch in LoadDeployConfigLax
// — the current LoadDeployConfig never returns a non-nil cfg alongside
// that error, so the branch is otherwise defensive.
var loadDeployConfigForLax = LoadDeployConfig

// LoadDeployConfigLax loads config without requiring Host (for setup/status commands that get host from flag).
func LoadDeployConfigLax(cmd *cobra.Command) (*DeployConfig, error) {
	cfg, err := loadDeployConfigForLax(cmd)
	if err != nil && cfg == nil {
		return nil, err
	}
	// If the only error was missing host, return the config anyway
	if err != nil && strings.Contains(err.Error(), "deploy host is required") {
		return cfg, nil
	}
	return cfg, err
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

func readAppName() (string, error) {
	f, err := os.Open("go.mod")
	if err != nil {
		return "", fmt.Errorf("cannot read go.mod: %w (are you in a gofasta project directory?)", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			mod := strings.TrimPrefix(line, "module ")
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
	_ = k.Load(env.Provider("GOFASTA_", ".", func(s string) string {
		return strings.ReplaceAll(
			strings.ToLower(strings.TrimPrefix(s, "GOFASTA_")),
			"_", ".",
		)
	}), nil)
	return k
}
