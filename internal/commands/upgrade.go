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

	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

// upgradeResult is the JSON contract for `gofasta upgrade --json`. The
// shape distinguishes the two install methods (go-install vs binary)
// via Method and reports both versions so an agent can decide whether
// to follow up with hash -r / a shell restart.
type upgradeResult struct {
	Action     string `json:"action"`
	Method     string `json:"method"` // go-install | binary | none
	OldVersion string `json:"old_version"`
	NewVersion string `json:"new_version,omitempty"`
	Path       string `json:"path,omitempty"`
	Upgraded   bool   `json:"upgraded"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

// emitUpgradeResult is the JSON-mode emitter. Text mode keeps the
// existing termcolor printers so the human progress UX is unchanged.
func emitUpgradeResult(r upgradeResult) {
	if !cliout.JSON() {
		return
	}
	cliout.Print(r, func(_ io.Writer) {})
}

// Package-level seams so tests can redirect network + URLs without hitting GitHub.
var (
	httpGet              = http.Get
	osExecutable         = os.Executable
	osChmodFn            = os.Chmod
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
		emitUpgradeResult(upgradeResult{
			Action: "upgrade", Method: "none", OldVersion: current,
			Success: false, Error: err.Error(),
		})
		return fmt.Errorf("failed to check for updates: %w", err)
	}
	latestClean := normalizeVersion(latest)

	if current == latestClean {
		if !cliout.JSON() {
			termcolor.PrintSuccess("gofasta is already up to date (v%s)", current)
		}
		emitUpgradeResult(upgradeResult{
			Action: "upgrade", Method: "none", OldVersion: current,
			NewVersion: current, Upgraded: false, Success: true,
		})
		return nil
	}

	if !cliout.JSON() {
		termcolor.PrintHeader("Upgrading gofasta: v%s → v%s", current, latestClean)
	}

	execPath, err := osExecutable()
	if err != nil {
		emitUpgradeResult(upgradeResult{
			Action: "upgrade", Method: "none", OldVersion: current,
			Success: false, Error: err.Error(),
		})
		return fmt.Errorf("cannot determine executable path: %w", err)
	}

	if isGoInstall(execPath) {
		return upgradeViaGoInstall(latest, latestClean, current)
	}
	return upgradeViaBinary(execPath, latest, current)
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

// upgradeViaGoInstall runs `go install` pinned to a specific version instead
// of @latest. Pinning matters because fetchLatestVersion queries the GitHub
// Releases API directly, but `go install @latest` goes through the Go module
// proxy, which has its own indexing lag — when a fresh tag is pushed the
// proxy can continue returning the previous version as "latest" for minutes
// to hours. Pinning to `@<rawTag>` (e.g. `@v0.1.5`) tells Go to fetch that
// exact version on demand, sidestepping the race entirely.
//
// rawTag is the version string as it appears on the release (with the "v"
// prefix, e.g. "v0.1.5"). expectedVersion is the normalized form (no "v",
// e.g. "0.1.5") used for the post-install version-match assertion.
func upgradeViaGoInstall(rawTag, expectedVersion, oldVersion string) error {
	modulePath := "github.com/gofastadev/cli/cmd/gofasta@" + rawTag
	if !cliout.JSON() {
		termcolor.PrintStep("Detected `go install`. Running: go install %s", modulePath)
	}
	cmd := execCommand("go", "install", modulePath)
	if cliout.JSON() {
		// Route the child's stdout to stderr so the structured result we
		// emit at the end is the only thing on stdout.
		cmd.Stdout = os.Stderr
	} else {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		emitUpgradeResult(upgradeResult{
			Action: "upgrade", Method: "go-install", OldVersion: oldVersion,
			NewVersion: expectedVersion, Success: false,
			Error: fmt.Sprintf("go install failed: %v", err),
		})
		return fmt.Errorf("go install failed: %w", err)
	}

	// Verify the install actually placed a new binary at the expected version.
	target, err := goInstallTargetPath()
	if err != nil {
		if !cliout.JSON() {
			termcolor.PrintWarn("Upgrade complete (could not determine install path for verification).")
			printShellHashHint()
		}
		emitUpgradeResult(upgradeResult{
			Action: "upgrade", Method: "go-install", OldVersion: oldVersion,
			NewVersion: expectedVersion, Upgraded: true, Success: true,
		})
		return nil
	}

	installed, err := readBinaryVersion(target)
	if err != nil {
		if !cliout.JSON() {
			termcolor.PrintSuccess("Upgrade complete. Installed to %s", target)
			termcolor.PrintWarn("Could not verify the version of the new binary — it may still be correct.")
			printShellHashHint()
		}
		emitUpgradeResult(upgradeResult{
			Action: "upgrade", Method: "go-install", OldVersion: oldVersion,
			NewVersion: expectedVersion, Path: target, Upgraded: true, Success: true,
		})
		return nil
	}

	if normalizeVersion(installed) != expectedVersion {
		mismatchErr := fmt.Errorf(
			"go install reported success but %s reports version %s, expected v%s — "+
				"this usually means $GOBIN or $GOPATH is set differently than expected. "+
				"Try running `go install %s` manually and check `which gofasta`",
			target, installed, expectedVersion, modulePath,
		)
		emitUpgradeResult(upgradeResult{
			Action: "upgrade", Method: "go-install", OldVersion: oldVersion,
			NewVersion: installed, Path: target, Success: false,
			Error: mismatchErr.Error(),
		})
		return mismatchErr
	}

	if !cliout.JSON() {
		termcolor.PrintSuccess("Upgraded to %s at %s", installed, target)
		printShellHashHint()
	}
	emitUpgradeResult(upgradeResult{
		Action: "upgrade", Method: "go-install", OldVersion: oldVersion,
		NewVersion: installed, Path: target, Upgraded: true, Success: true,
	})
	return nil
}

// runtimeGOOS is a seam over runtime.GOOS so tests can exercise the
// windows-suffix branch on any host.
var runtimeGOOS = func() string { return runtime.GOOS }

func upgradeViaBinary(execPath, version, oldVersion string) error {
	goos := runtimeGOOS()
	goarch := runtime.GOARCH

	binary := fmt.Sprintf("gofasta-%s-%s", goos, goarch)
	if goos == "windows" {
		binary += ".exe"
	}

	url := fmt.Sprintf(githubDownloadURLFmt, version, binary)
	if !cliout.JSON() {
		termcolor.PrintStep("Downloading %s...", url)
	}

	emitFail := func(err error) error {
		emitUpgradeResult(upgradeResult{
			Action: "upgrade", Method: "binary", OldVersion: oldVersion,
			NewVersion: normalizeVersion(version), Path: execPath,
			Success: false, Error: err.Error(),
		})
		return err
	}

	resp, err := httpGet(url)
	if err != nil {
		return emitFail(fmt.Errorf("download failed: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return emitFail(fmt.Errorf("download failed: HTTP %d", resp.StatusCode))
	}

	// Write to a temp file first
	tmpFile, err := os.CreateTemp("", "gofasta-upgrade-*")
	if err != nil {
		return emitFail(fmt.Errorf("failed to create temp file: %w", err))
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		_ = tmpFile.Close()
		return emitFail(fmt.Errorf("download failed: %w", err))
	}
	_ = tmpFile.Close()

	if err := osChmodFn(tmpPath, 0o755); err != nil {
		return emitFail(fmt.Errorf("failed to set permissions: %w", err))
	}

	// Replace the current binary
	if err := os.Rename(tmpPath, execPath); err != nil {
		// Rename may fail across filesystems, fall back to copy
		return replaceViaCopy(tmpPath, execPath, oldVersion, version)
	}

	if !cliout.JSON() {
		termcolor.PrintSuccess("Installed %s to %s", version, execPath)
		printShellHashHint()
	}
	emitUpgradeResult(upgradeResult{
		Action: "upgrade", Method: "binary", OldVersion: oldVersion,
		NewVersion: normalizeVersion(version), Path: execPath,
		Upgraded: true, Success: true,
	})
	return nil
}

func replaceViaCopy(src, dst, oldVersion, version string) error {
	input, err := os.ReadFile(src)
	if err != nil {
		emitUpgradeResult(upgradeResult{
			Action: "upgrade", Method: "binary", OldVersion: oldVersion,
			NewVersion: normalizeVersion(version), Path: dst,
			Success: false, Error: err.Error(),
		})
		return fmt.Errorf("failed to read downloaded binary: %w", err)
	}

	if err := os.WriteFile(dst, input, 0o755); err != nil {
		emitUpgradeResult(upgradeResult{
			Action: "upgrade", Method: "binary", OldVersion: oldVersion,
			NewVersion: normalizeVersion(version), Path: dst,
			Success: false, Error: err.Error(),
		})
		return fmt.Errorf("failed to write binary (you may need sudo): %w", err)
	}

	if !cliout.JSON() {
		termcolor.PrintSuccess("Installed to %s", dst)
		printShellHashHint()
	}
	emitUpgradeResult(upgradeResult{
		Action: "upgrade", Method: "binary", OldVersion: oldVersion,
		NewVersion: normalizeVersion(version), Path: dst,
		Upgraded: true, Success: true,
	})
	return nil
}

// printShellHashHint reminds the user that their current shell session may
// still resolve `gofasta` to the old binary's cached inode. Text-mode only;
// the JSON consumer doesn't need shell-cache advice.
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
