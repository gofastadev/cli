package deploy

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Rollback reverts to the previous release.
func Rollback(cfg *DeployConfig) error {
	fmt.Printf("Rolling back %s on %s...\n\n", cfg.AppName, cfg.Host)

	// Step 1: List releases
	PrintStep(1, 4, "Finding previous release...")
	releasesDir := filepath.Join(cfg.Path, "releases")
	output, err := RunRemoteCapture(cfg, fmt.Sprintf("ls -1t %s", releasesDir))
	if err != nil {
		return fmt.Errorf("failed to list releases: %w", err)
	}

	releases := strings.Split(strings.TrimSpace(output), "\n")
	if len(releases) < 2 {
		return fmt.Errorf("no previous release to rollback to (only %d release found)", len(releases))
	}

	// Find current release
	currentOutput, err := RunRemoteCapture(cfg, fmt.Sprintf("readlink %s", cfg.CurrentPath()))
	if err != nil {
		PrintWarning("Could not read current symlink — using newest release as current")
	}

	current := filepath.Base(strings.TrimSpace(currentOutput))
	var previous string
	for _, r := range releases {
		if r != current && r != "" {
			previous = r
			break
		}
	}
	if previous == "" {
		return fmt.Errorf("no previous release found to rollback to")
	}

	previousPath := filepath.Join(releasesDir, previous)
	PrintInfo(fmt.Sprintf("Current:  %s", current))
	PrintInfo(fmt.Sprintf("Rolling back to: %s", previous))
	fmt.Println()

	// Step 2: Activate previous release
	PrintStep(2, 4, "Activating previous release...")
	if cfg.Method == "docker" {
		composeUp := fmt.Sprintf("cd %s && docker compose -f compose.yaml up -d", previousPath)
		if err := RunRemote(cfg, composeUp); err != nil {
			return fmt.Errorf("failed to start previous release containers: %w", err)
		}
	} else {
		installCmd := fmt.Sprintf(
			"sudo cp %s/%s /usr/local/bin/%s && sudo chmod +x /usr/local/bin/%s && "+
				"sudo systemctl daemon-reload && sudo systemctl restart %s",
			previousPath, cfg.AppName, cfg.AppName, cfg.AppName, cfg.AppName,
		)
		if err := RunRemote(cfg, installCmd); err != nil {
			return fmt.Errorf("failed to rollback binary: %w", err)
		}
	}

	// Step 3: Update symlink
	PrintStep(3, 4, "Updating current release pointer...")
	symlinkCmd := fmt.Sprintf("ln -sfn %s %s", previousPath, cfg.CurrentPath())
	if err := RunRemote(cfg, symlinkCmd); err != nil {
		return fmt.Errorf("failed to update symlink: %w", err)
	}

	// Step 4: Health check
	PrintStep(4, 4, "Checking application health...")
	if err := CheckHealth(cfg); err != nil {
		PrintError("Health check failed after rollback")
		return err
	}

	fmt.Println()
	PrintSuccess(fmt.Sprintf("Rolled back to release %s", previous))
	return nil
}
