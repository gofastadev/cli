package deploy

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
)

const binaryTotalSteps = 11

// DeployBinary deploys the application as a compiled binary managed by systemd.
//
// Cutover model: restart-based with automatic rollback (same shape as the
// docker method). Migrations run BEFORE cutover using the release's own
// binary — a failed migration aborts the deploy without ever touching the
// running service. Cutover installs the binary, flips `current` (the
// systemd unit's WorkingDirectory), and restarts; a failed health gate
// reinstalls the previous release's binary and restores the pointer.
//
//nolint:gocyclo,revive // linear step pipeline; name kept for public-API stability.
func DeployBinary(cfg *DeployConfig) error {
	step := 0
	nextStep := func(msg string) {
		step++
		cliout.Step("[%d/%d] %s", step, binaryTotalSteps, msg)
	}

	// Step 1: Pre-flight
	nextStep("Running pre-flight checks...")
	if err := PreflightChecks(cfg); err != nil {
		return err
	}

	// Step 2: Cross-compile binary
	nextStep(fmt.Sprintf("Cross-compiling binary (linux/%s)...", cfg.Arch))
	if err := os.MkdirAll("tmp", 0o755); err != nil {
		return clierr.Wrap(clierr.CodeDeployConfig, err, "cannot create local tmp directory")
	}
	binaryPath := filepath.Join("tmp", cfg.AppName)

	// Set cross-compilation environment
	prevCGO := os.Getenv("CGO_ENABLED")
	prevGOOS := os.Getenv("GOOS")
	prevGOARCH := os.Getenv("GOARCH")
	_ = os.Setenv("CGO_ENABLED", "0")
	_ = os.Setenv("GOOS", "linux")
	_ = os.Setenv("GOARCH", cfg.Arch)
	defer func() {
		setOrUnset("CGO_ENABLED", prevCGO)
		setOrUnset("GOOS", prevGOOS)
		setOrUnset("GOARCH", prevGOARCH)
	}()

	if err := RunLocal(cfg, "go", "build", "-ldflags=-s -w", "-o", binaryPath, "./app/main"); err != nil {
		return clierr.Wrap(clierr.CodeDeployConfig, err, "cross-compile failed")
	}

	// Step 3: Create release directory on remote
	nextStep("Creating release directory...")
	releasePath := cfg.ReleasePath()
	mkdirCmd := fmt.Sprintf("mkdir -p %s/migrations %s/templates %s/configs %s",
		releasePath, releasePath, releasePath, cfg.SharedPath())
	if err := RunRemote(cfg, mkdirCmd); err != nil {
		return clierr.Wrap(clierr.CodeSSHFailed, err, "failed to create remote directories")
	}

	// Step 4: Transfer binary
	nextStep("Uploading binary...")
	if err := CopyFile(cfg, binaryPath, filepath.Join(releasePath, cfg.AppName)); err != nil {
		return clierr.Wrap(clierr.CodeSSHFailed, err, "failed to copy binary")
	}

	// Step 5: Transfer supporting files. Every copy is checked: a silently
	// missing migrations directory used to surface much later as a
	// mystifying migration failure.
	nextStep("Uploading migrations, templates, and configs...")
	for _, dir := range []struct{ local, remote string }{
		{"db/migrations", "migrations"},
		{"templates", "templates"},
		{"configs", "configs"},
	} {
		if _, err := os.Stat(dir.local); err != nil {
			continue // optional directory not present in this project
		}
		if err := CopyDir(cfg, dir.local+"/.", filepath.Join(releasePath, dir.remote)); err != nil {
			return clierr.Wrapf(clierr.CodeSSHFailed, err, "failed to copy %s", dir.local)
		}
	}

	// Step 6: Copy shared config files and link them into the release
	nextStep("Uploading configuration files...")
	if err := copySharedFiles(cfg); err != nil {
		return err
	}
	if err := linkSharedInto(cfg, releasePath); err != nil {
		return clierr.Wrap(clierr.CodeSSHFailed, err, "failed to link shared config into the release")
	}

	// Step 7: Install systemd unit (the binary itself is installed at
	// cutover). daemon-reload here so a changed unit is picked up once.
	nextStep("Installing systemd service...")
	serviceFile := "deployments/systemd/app.service"
	if _, err := os.Stat(serviceFile); err != nil {
		return clierr.Newf(clierr.CodeDeployConfig,
			"no systemd service file found at %s — are you in a gofasta project directory?", serviceFile)
	}
	remoteSvc := fmt.Sprintf("/tmp/%s.service", cfg.AppName)
	if err := CopyFile(cfg, serviceFile, remoteSvc); err != nil {
		return clierr.Wrap(clierr.CodeSSHFailed, err, "failed to copy service file")
	}
	installSvc := fmt.Sprintf(
		"sudo cp %s /etc/systemd/system/%s.service && sudo systemctl daemon-reload",
		remoteSvc, cfg.AppName,
	)
	if err := RunRemote(cfg, installSvc); err != nil {
		return clierr.Wrap(clierr.CodeSSHFailed, err, "failed to install systemd service")
	}

	// Step 8: Run migrations BEFORE cutover, with the release's own binary
	// from the release directory (so ./migrations resolves). The running
	// service is untouched — a failed migration aborts cleanly.
	// NOTE: already-applied migration steps are not reverted on abort
	// (golang-migrate has no automatic undo).
	nextStep("Running database migrations...")
	migrateCmd := fmt.Sprintf("cd %s && ./%s migrate up", releasePath, cfg.AppName)
	if err := RunRemote(cfg, migrateCmd); err != nil {
		cliout.Fail("Migrations failed — the running service was not touched")
		removeFailedRelease(cfg, releasePath)
		return clierr.Wrap(clierr.CodeMigrationFailed, err, "database migrations failed")
	}

	// Step 9: Record previous release, then cutover.
	nextStep("Cutting over (brief restart)...")
	prevRelease := currentBinaryRelease(cfg)
	if prevRelease != "" {
		cliout.Info("Previous release: %s", prevRelease)
	} else {
		cliout.Info("First deploy — no previous release to fall back to")
	}
	if err := RunRemote(cfg, binaryCutoverCommand(cfg, releasePath)); err != nil {
		return autoRollbackBinary(cfg, prevRelease,
			clierr.Wrap(clierr.CodeSSHFailed, err, "failed to install and restart the service"))
	}

	// Step 10: Health gate
	nextStep("Checking application health...")
	if err := CheckHealth(cfg); err != nil {
		return autoRollbackBinary(cfg, prevRelease, err)
	}
	cliout.Success("Application is healthy")

	// Step 11: Cleanup old releases
	nextStep("Cleaning up old releases...")
	if err := CleanupOldReleases(cfg); err != nil {
		cliout.Warn("Cleanup warning: %v", err)
	}

	// Cleanup local temp binary
	_ = os.Remove(binaryPath)

	cliout.Blank()
	cliout.Success("Deployed %s to %s (binary)", cfg.ReleaseTag, cfg.Host)
	cliout.Info("Release:  %s", releasePath)
	cliout.Info("Binary:   /usr/local/bin/%s", cfg.AppName)
	cliout.Info("Service:  %s.service", cfg.AppName)
	cliout.Info("App:      http://%s:%s", cfg.Host, cfg.ServerPort)
	return nil
}

// binaryCutoverCommand installs the release's binary, flips `current` (the
// unit's WorkingDirectory), enables the unit, and restarts onto the new
// release — the restart-based cutover in one remote command.
//
// The install is stage-then-rename: `cp` directly over a RUNNING executable
// fails with ETXTBSY ("Text file busy"), which made every deploy after the
// first one impossible; rename(2) replaces the path atomically regardless.
func binaryCutoverCommand(cfg *DeployConfig, releasePath string) string {
	return fmt.Sprintf(
		"sudo cp %s/%s /usr/local/bin/%s.new && sudo chmod +x /usr/local/bin/%s.new && "+
			"sudo mv -f /usr/local/bin/%s.new /usr/local/bin/%s && "+
			"ln -sfn %s %s && sudo systemctl enable %s && sudo systemctl restart %s",
		releasePath, cfg.AppName, cfg.AppName, cfg.AppName,
		cfg.AppName, cfg.AppName,
		releasePath, cfg.CurrentPath(), cfg.AppName, cfg.AppName,
	)
}

// currentBinaryRelease returns the release path `current` points at, or ""
// on a first deploy.
func currentBinaryRelease(cfg *DeployConfig) string {
	out, err := RunRemoteCapture(cfg, fmt.Sprintf("readlink %s", cfg.CurrentPath()))
	if err != nil {
		return ""
	}
	return out
}

// autoRollbackBinary reinstalls the previous release's binary after a failed
// cutover or health check and removes the failed release directory. On a
// first deploy the broken service is left in place for inspection.
func autoRollbackBinary(cfg *DeployConfig, prevRelease string, cause error) error {
	cliout.Fail("Deploy failed: %v", cause)

	if prevRelease == "" {
		cliout.Warn("First deploy — no previous release to roll back to")
		cliout.Info("The failed service is left in place for inspection:")
		cliout.Info("  gofasta deploy logs      # journalctl output")
		cliout.Info("  gofasta deploy status    # service state")
		return cause
	}

	cliout.Warn("Rolling back to previous release %s...", filepath.Base(prevRelease))
	if err := RunRemote(cfg, binaryCutoverCommand(cfg, prevRelease)); err != nil {
		cliout.Fail("Automatic rollback failed: %v", err)
		return clierr.Wrapf(clierr.CodeRollbackFailed, cause,
			"deploy failed AND automatic rollback failed (%v) — the app may be down, inspect with `gofasta deploy logs`", err)
	}
	if err := CheckHealth(cfg); err != nil {
		cliout.Fail("Previous release did not become healthy after rollback: %v", err)
		return clierr.Wrap(clierr.CodeRollbackFailed, cause,
			"deploy failed and the restored release is unhealthy — inspect with `gofasta deploy logs`")
	}

	removeFailedRelease(cfg, cfg.ReleasePath())
	cliout.Success("Rolled back — previous release %s is live again", filepath.Base(prevRelease))
	return cause
}

// removeFailedRelease deletes a release directory that never became current,
// so `deploy rollback`'s newest-older-than-current selection can never land
// on a known-bad release.
func removeFailedRelease(cfg *DeployConfig, releasePath string) {
	if err := RunRemote(cfg, fmt.Sprintf("rm -rf %s", releasePath)); err != nil {
		cliout.Warn("Could not remove the failed release %s: %v", releasePath, err)
	}
}

func setOrUnset(key, value string) {
	if value == "" {
		_ = os.Unsetenv(key)
	} else {
		_ = os.Setenv(key, value)
	}
}
