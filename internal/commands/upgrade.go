package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gofastadev/cli/internal/termcolor"
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
	Short: "Self-update the CLI to the latest GitHub release (auto-detects install method)",
	Long: `Check GitHub for a newer release of the gofasta CLI and install it in
place. The upgrade method is auto-detected based on how the running
binary was originally installed:

  • Go install — re-runs ` + "`go install github.com/gofastadev/cli/cmd/gofasta@latest`" + `
                 if the running binary lives under $GOBIN, $GOPATH/bin,
                 or ~/go/bin
  • Pre-built  — downloads the platform-matched asset from the latest
                 GitHub release and atomically replaces the running
                 binary with ` + "`os.Rename`" + ` (falling back to a read/write
                 copy if rename crosses filesystems)

Version comparison strips any leading "v" from both the installed and
latest tags, so ` + "`v1.2.3`" + ` and ` + "`1.2.3`" + ` compare equal. Pseudo-versions like
` + "`v0.1.3-0.20260411-abcdef`" + ` (typical of ` + "`go install`" + ` from a branch) always
compare unequal and will trigger an upgrade.

After installation the new binary's version is read back by executing
` + "`<new-binary> --version`" + ` and compared against the expected release tag. A
mismatch is reported as an error along with a hint to check $GOBIN /
$GOPATH, because it almost always means ` + "`go install`" + ` wrote the binary to
a different directory than the one on $PATH.

If the upgrade succeeds but ` + "`gofasta --version`" + ` still reports the old
version in your current shell, run ` + "`hash -r`" + ` (bash / zsh) or open a new
terminal — your shell has cached the old executable's inode.`,
	RunE: func(_ *cobra.Command, _ []string) error {
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

// normalizeVersion returns a version string without a leading "v" so that
// runtime/debug-style "v0.1.2" and GitHub-tag-style "v0.1.2" both compare to
// "0.1.2". Pseudo-versions like "v0.1.3-0.20260411-abcdef" are returned with
// only the leading v stripped — they will compare unequal to release tags,
// which is exactly what we want (a dev build should always be "upgradeable").
func normalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}

func runUpgrade() error {
	current := normalizeVersion(rootCmd.Version)

	latest, err := fetchLatestVersion()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}
	latestClean := normalizeVersion(latest)

	if current == latestClean {
		termcolor.PrintSuccess("gofasta is already up to date (v%s)", current)
		return nil
	}

	termcolor.PrintHeader("Upgrading gofasta: v%s → v%s", current, latestClean)

	execPath, err := osExecutable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}

	if isGoInstall(execPath) {
		return upgradeViaGoInstall(latestClean)
	}
	return upgradeViaBinary(execPath, latest)
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

func isGoInstall(execPath string) bool {
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, _ := os.UserHomeDir()
		gopath = home + "/go"
	}
	return strings.HasPrefix(execPath, gopath)
}

// goInstallTargetPath returns the absolute path where `go install` will write
// the gofasta binary, honoring $GOBIN > $GOPATH/bin > $HOME/go/bin in order.
func goInstallTargetPath() (string, error) {
	if v := os.Getenv("GOBIN"); v != "" {
		return filepath.Join(v, "gofasta"), nil
	}
	if v := os.Getenv("GOPATH"); v != "" {
		return filepath.Join(v, "bin", "gofasta"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "go", "bin", "gofasta"), nil
}

// readBinaryVersion runs `<binPath> --version` and parses the version string
// out of the output. Returns the raw version (with leading v).
func readBinaryVersion(binPath string) (string, error) {
	cmd := execCommand(binPath, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	// Cobra's --version prints lines like:
	//   gofasta version v0.1.2
	// We want the last whitespace-separated token of the first line.
	first := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	fields := strings.Fields(first)
	if len(fields) == 0 {
		return "", fmt.Errorf("could not parse version from %q", first)
	}
	return fields[len(fields)-1], nil
}

func upgradeViaGoInstall(expectedVersion string) error {
	termcolor.PrintStep("Detected `go install`. Running: go install github.com/gofastadev/cli/cmd/gofasta@latest")
	cmd := execCommand("go", "install", "github.com/gofastadev/cli/cmd/gofasta@latest")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install failed: %w", err)
	}

	// Verify the install actually placed a new binary at the expected version.
	target, err := goInstallTargetPath()
	if err != nil {
		termcolor.PrintWarn("Upgrade complete (could not determine install path for verification).")
		printShellHashHint()
		return nil
	}

	installed, err := readBinaryVersion(target)
	if err != nil {
		termcolor.PrintSuccess("Upgrade complete. Installed to %s", target)
		termcolor.PrintWarn("Could not verify the version of the new binary — it may still be correct.")
		printShellHashHint()
		return nil
	}

	if normalizeVersion(installed) != expectedVersion {
		return fmt.Errorf(
			"go install reported success but %s reports version %s, expected v%s — "+
				"this usually means $GOBIN or $GOPATH is set differently than expected. "+
				"Try running `go install github.com/gofastadev/cli/cmd/gofasta@latest` manually "+
				"and check `which gofasta`",
			target, installed, expectedVersion,
		)
	}

	termcolor.PrintSuccess("Upgraded to %s at %s", installed, target)
	printShellHashHint()
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
	termcolor.PrintStep("Downloading %s...", url)

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

	termcolor.PrintSuccess("Installed %s to %s", version, execPath)
	printShellHashHint()
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

	termcolor.PrintSuccess("Installed to %s", dst)
	printShellHashHint()
	return nil
}

// printShellHashHint reminds the user that their current shell session may
// still resolve `gofasta` to the old binary's cached inode.
func printShellHashHint() {
	fmt.Println()
	fmt.Println("If `gofasta --version` still reports the old version in this shell,")
	fmt.Println("your shell has cached the old executable. Refresh it with:")
	fmt.Println()
	fmt.Println("    hash -r        # bash / zsh")
	fmt.Println("    rehash         # zsh (alternative)")
	fmt.Println()
	fmt.Println("…or just open a new terminal.")
}
