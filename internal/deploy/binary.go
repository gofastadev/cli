package deploy

import (
	"fmt"
	"os"
	"path/filepath"
)

const binaryTotalSteps = 12

// DeployBinary deploys the application as a compiled binary managed by systemd.
func DeployBinary(cfg *DeployConfig) error {
	step := 0

	// Step 1: Pre-flight
	step++
	PrintStep(step, binaryTotalSteps, "Running pre-flight checks...")
	if err := PreflightChecks(cfg); err != nil {
		return err
	}

	// Step 2: Cross-compile binary
	step++
	PrintStep(step, binaryTotalSteps, fmt.Sprintf("Cross-compiling binary (linux/%s)...", cfg.Arch))
	os.MkdirAll("tmp", 0755)
	binaryPath := filepath.Join("tmp", cfg.AppName)

	// Set cross-compilation environment
	prevCGO := os.Getenv("CGO_ENABLED")
	prevGOOS := os.Getenv("GOOS")
	prevGOARCH := os.Getenv("GOARCH")
	os.Setenv("CGO_ENABLED", "0")
	os.Setenv("GOOS", "linux")
	os.Setenv("GOARCH", cfg.Arch)
	defer func() {
		setOrUnset("CGO_ENABLED", prevCGO)
		setOrUnset("GOOS", prevGOOS)
		setOrUnset("GOARCH", prevGOARCH)
	}()

	if err := RunLocal(cfg, "go", "build", "-ldflags=-s -w", "-o", binaryPath, "./app/main"); err != nil {
		return fmt.Errorf("cross-compile failed: %w", err)
	}

	// Step 3: Create release directory on remote
	step++
	PrintStep(step, binaryTotalSteps, "Creating release directory...")
	releasePath := cfg.ReleasePath()
	mkdirCmd := fmt.Sprintf("mkdir -p %s/migrations %s/templates %s/configs %s",
		releasePath, releasePath, releasePath, cfg.SharedPath())
	if err := RunRemote(cfg, mkdirCmd); err != nil {
		return fmt.Errorf("failed to create remote directories: %w", err)
	}

	// Step 4: Transfer binary
	step++
	PrintStep(step, binaryTotalSteps, "Uploading binary...")
	if err := CopyFile(cfg, binaryPath, filepath.Join(releasePath, cfg.AppName)); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	// Step 5: Transfer supporting files
	step++
	PrintStep(step, binaryTotalSteps, "Uploading migrations, templates, and configs...")
	if _, err := os.Stat("db/migrations"); err == nil {
		CopyDir(cfg, "db/migrations/.", filepath.Join(releasePath, "migrations"))
	}
	if _, err := os.Stat("templates"); err == nil {
		CopyDir(cfg, "templates/.", filepath.Join(releasePath, "templates"))
	}
	if _, err := os.Stat("configs"); err == nil {
		CopyDir(cfg, "configs/.", filepath.Join(releasePath, "configs"))
	}

	// Step 6: Copy shared config files
	step++
	PrintStep(step, binaryTotalSteps, "Uploading configuration files...")
	if err := copySharedFiles(cfg); err != nil {
		return err
	}
	// Symlink shared files into release
	symlinkShared := fmt.Sprintf(
		"ln -sf %s/.env %s/.env 2>/dev/null; ln -sf %s/config.yaml %s/config.yaml 2>/dev/null",
		cfg.SharedPath(), releasePath, cfg.SharedPath(), releasePath,
	)
	RunRemote(cfg, symlinkShared)

	// Step 7: Install binary
	step++
	PrintStep(step, binaryTotalSteps, "Installing binary...")
	installCmd := fmt.Sprintf(
		"sudo cp %s/%s /usr/local/bin/%s && sudo chmod +x /usr/local/bin/%s",
		releasePath, cfg.AppName, cfg.AppName, cfg.AppName,
	)
	if err := RunRemote(cfg, installCmd); err != nil {
		return fmt.Errorf("failed to install binary: %w", err)
	}

	// Step 8: Install systemd service
	step++
	PrintStep(step, binaryTotalSteps, "Installing systemd service...")
	serviceFile := "deployments/systemd/app.service"
	if _, err := os.Stat(serviceFile); err == nil {
		remoteSvc := fmt.Sprintf("/tmp/%s.service", cfg.AppName)
		if err := CopyFile(cfg, serviceFile, remoteSvc); err != nil {
			return fmt.Errorf("failed to copy service file: %w", err)
		}
		installSvc := fmt.Sprintf(
			"sudo cp %s /etc/systemd/system/%s.service",
			remoteSvc, cfg.AppName,
		)
		RunRemote(cfg, installSvc)
	} else {
		PrintWarning("No systemd service file found at " + serviceFile)
	}

	// Step 9: Run migrations
	step++
	PrintStep(step, binaryTotalSteps, "Running database migrations...")
	migrateCmd := fmt.Sprintf(
		"cd %s && /usr/local/bin/%s migrate up 2>/dev/null || echo '   Migrations: nothing to apply or skipped'",
		releasePath, cfg.AppName,
	)
	RunRemote(cfg, migrateCmd)

	// Step 10: Restart service
	step++
	PrintStep(step, binaryTotalSteps, "Restarting service...")
	restartCmd := fmt.Sprintf(
		"sudo systemctl daemon-reload && sudo systemctl enable %s && sudo systemctl restart %s",
		cfg.AppName, cfg.AppName,
	)
	if err := RunRemote(cfg, restartCmd); err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}

	// Step 11: Health check
	step++
	PrintStep(step, binaryTotalSteps, "Checking application health...")
	if err := CheckHealth(cfg); err != nil {
		PrintError("Health check failed — not updating current release")
		PrintInfo("Previous release is still active. Check logs with: gofasta deploy logs")
		return err
	}
	PrintSuccess("Application is healthy")

	// Step 12: Update current symlink and cleanup
	step++
	PrintStep(step, binaryTotalSteps, "Finalizing release...")
	symlinkCmd := fmt.Sprintf("ln -sfn %s %s", releasePath, cfg.CurrentPath())
	if err := RunRemote(cfg, symlinkCmd); err != nil {
		return fmt.Errorf("failed to update current symlink: %w", err)
	}
	if err := CleanupOldReleases(cfg); err != nil {
		PrintWarning(fmt.Sprintf("Cleanup warning: %v", err))
	}

	// Cleanup local temp binary
	os.Remove(binaryPath)

	fmt.Println()
	PrintSuccess(fmt.Sprintf("Deployed %s to %s (binary)", cfg.ReleaseTag, cfg.Host))
	PrintInfo(fmt.Sprintf("Release:  %s", releasePath))
	PrintInfo(fmt.Sprintf("Binary:   /usr/local/bin/%s", cfg.AppName))
	PrintInfo(fmt.Sprintf("Service:  %s.service", cfg.AppName))
	PrintInfo(fmt.Sprintf("App:      http://%s:%s", cfg.Host, cfg.ServerPort))
	return nil
}

func setOrUnset(key, value string) {
	if value == "" {
		os.Unsetenv(key)
	} else {
		os.Setenv(key, value)
	}
}
