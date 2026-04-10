package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// Package-level seams so tests can redirect network + URLs without hitting GitHub.
var (
	httpGet              = http.Get
	osExecutable         = os.Executable
	githubAPIURL         = "https://api.github.com/repos/gofastadev/cli/releases/latest"
	githubDownloadURLFmt = "https://github.com/gofastadev/cli/releases/download/%s/%s"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade gofasta to the latest version",
	Long: `Check for a newer version of the gofasta CLI and install it.

The upgrade method depends on how gofasta was originally installed:
  - Homebrew: runs "brew upgrade gofasta"
  - Go install: runs "go install github.com/gofastadev/cli/cmd/gofasta@latest"
  - Binary: downloads the latest release from GitHub and replaces the current binary`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUpgrade()
	},
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}

// githubRelease represents the relevant fields from the GitHub releases API.
type githubRelease struct {
	TagName string `json:"tag_name"`
}

func runUpgrade() error {
	currentVersion := rootCmd.Version

	// Fetch the latest version from GitHub
	latest, err := fetchLatestVersion()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	latestClean := strings.TrimPrefix(latest, "v")
	if currentVersion == latestClean {
		fmt.Printf("gofasta is already up to date (v%s)\n", currentVersion)
		return nil
	}

	fmt.Printf("Upgrading gofasta: v%s → v%s\n", currentVersion, latestClean)

	// Detect installation method and upgrade accordingly
	execPath, err := osExecutable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}

	switch {
	case isHomebrew(execPath):
		return upgradeViaHomebrew()
	case isGoInstall(execPath):
		return upgradeViaGoInstall()
	default:
		return upgradeViaBinary(execPath, latest)
	}
}

func fetchLatestVersion() (string, error) {
	resp, err := httpGet(githubAPIURL)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	if release.TagName == "" {
		return "", fmt.Errorf("no releases found")
	}

	return release.TagName, nil
}

func isHomebrew(execPath string) bool {
	return strings.Contains(execPath, "Cellar") || strings.Contains(execPath, "homebrew") || strings.Contains(execPath, "linuxbrew")
}

func isGoInstall(execPath string) bool {
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, _ := os.UserHomeDir()
		gopath = home + "/go"
	}
	return strings.HasPrefix(execPath, gopath)
}

func upgradeViaHomebrew() error {
	fmt.Println("Detected Homebrew installation, running: brew upgrade gofasta")
	cmd := execCommand("brew", "upgrade", "gofasta")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("brew upgrade failed: %w", err)
	}
	fmt.Println("Upgrade complete.")
	return nil
}

func upgradeViaGoInstall() error {
	fmt.Println("Detected go install, running: go install github.com/gofastadev/cli/cmd/gofasta@latest")
	cmd := execCommand("go", "install", "github.com/gofastadev/cli/cmd/gofasta@latest")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install failed: %w", err)
	}
	fmt.Println("Upgrade complete.")
	return nil
}

func upgradeViaBinary(execPath, version string) error {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	binary := fmt.Sprintf("gofasta-%s-%s", goos, goarch)
	if goos == "windows" {
		binary += ".exe"
	}

	url := fmt.Sprintf(githubDownloadURLFmt, version, binary)
	fmt.Printf("Downloading %s...\n", url)

	resp, err := httpGet(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Write to a temp file first
	tmpFile, err := os.CreateTemp("", "gofasta-upgrade-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("download failed: %w", err)
	}
	_ = tmpFile.Close()

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	// Replace the current binary
	if err := os.Rename(tmpPath, execPath); err != nil {
		// Rename may fail across filesystems, fall back to copy
		return replaceViaCopy(tmpPath, execPath)
	}

	fmt.Printf("Upgrade complete. Installed to %s\n", execPath)
	return nil
}

func replaceViaCopy(src, dst string) error {
	input, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read downloaded binary: %w", err)
	}

	if err := os.WriteFile(dst, input, 0o755); err != nil {
		return fmt.Errorf("failed to write binary (you may need sudo): %w", err)
	}

	fmt.Printf("Upgrade complete. Installed to %s\n", dst)
	return nil
}
