package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const dockerTotalSteps = 11

// DeployDocker deploys the application using Docker compose on the remote server.
//
//nolint:revive // name kept for public-API stability; rename is a breaking change.
func DeployDocker(cfg *DeployConfig) error {
	step := 0

	// Step 1: Pre-flight
	step++
	PrintStep(step, dockerTotalSteps, "Running pre-flight checks...")
	if err := PreflightChecks(cfg); err != nil {
		return err
	}

	// Step 2: Build Docker image
	step++
	PrintStep(step, dockerTotalSteps, "Building Docker image...")
	imageTag := cfg.AppName + ":" + cfg.ReleaseTag
	imageLatest := cfg.AppName + ":latest"
	if err := RunLocal(cfg, "docker", "build", "-t", imageTag, "-t", imageLatest, "."); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	// Step 3: Create release directory on remote
	step++
	PrintStep(step, dockerTotalSteps, "Creating release directory...")
	mkdirCmd := fmt.Sprintf("mkdir -p %s %s", cfg.ReleasePath(), cfg.SharedPath())
	if err := RunRemote(cfg, mkdirCmd); err != nil {
		return fmt.Errorf("failed to create remote directories: %w", err)
	}

	// Step 4: Transfer Docker image
	step++
	PrintStep(step, dockerTotalSteps, "Transferring Docker image to server...")
	pipeline := fmt.Sprintf("docker save %s | ssh -p %d %s 'docker load'",
		imageTag, cfg.Port, cfg.Host)
	if err := RunLocalPiped(cfg, pipeline); err != nil {
		return fmt.Errorf("docker image transfer failed: %w", err)
	}

	// Step 5: Copy shared config files
	step++
	PrintStep(step, dockerTotalSteps, "Uploading configuration files...")
	if err := copySharedFiles(cfg); err != nil {
		return err
	}

	// Step 6: Copy compose file
	step++
	PrintStep(step, dockerTotalSteps, "Uploading compose file...")
	composeFile := "deployments/docker/compose.production.yaml"
	if _, err := os.Stat(composeFile); err != nil {
		return fmt.Errorf("compose file not found at %s — are you in a gofasta project directory?", composeFile)
	}
	if err := CopyFile(cfg, composeFile, filepath.Join(cfg.ReleasePath(), "compose.yaml")); err != nil {
		return fmt.Errorf("failed to copy compose file: %w", err)
	}

	// Step 7: Stop old containers and start new ones
	step++
	PrintStep(step, dockerTotalSteps, "Starting containers...")
	composeUp := fmt.Sprintf("cd %s && docker compose -f compose.yaml up -d", cfg.ReleasePath())
	if err := RunRemote(cfg, composeUp); err != nil {
		return fmt.Errorf("docker compose up failed: %w", err)
	}

	// Step 8: Run migrations
	step++
	PrintStep(step, dockerTotalSteps, "Running database migrations...")
	containerName := cfg.AppName + "_app"
	// The app container has migrate CLI built in from the Dockerfile.
	migrateCmd := fmt.Sprintf("docker exec %s sh -c 'migrate -path /migrations -database \"$DATABASE_URL\" up 2>/dev/null' || echo '   Migrations: nothing to apply or skipped'",
		containerName)
	_ = RunRemote(cfg, migrateCmd)

	// Step 9: Health check
	step++
	PrintStep(step, dockerTotalSteps, "Checking application health...")
	if err := CheckHealth(cfg); err != nil {
		PrintError("Health check failed — not updating current release")
		PrintInfo("Previous release is still active. Check logs with: gofasta deploy logs")
		return err
	}
	PrintSuccess("Application is healthy")

	// Step 10: Update current symlink
	step++
	PrintStep(step, dockerTotalSteps, "Updating current release...")
	symlinkCmd := fmt.Sprintf("ln -sfn %s %s", cfg.ReleasePath(), cfg.CurrentPath())
	if err := RunRemote(cfg, symlinkCmd); err != nil {
		return fmt.Errorf("failed to update current symlink: %w", err)
	}

	// Step 11: Cleanup old releases
	step++
	PrintStep(step, dockerTotalSteps, "Cleaning up old releases...")
	if err := CleanupOldReleases(cfg); err != nil {
		PrintWarning(fmt.Sprintf("Cleanup warning: %v", err))
	}

	fmt.Println()
	PrintSuccess(fmt.Sprintf("Deployed %s to %s (docker)", cfg.ReleaseTag, cfg.Host))
	PrintInfo(fmt.Sprintf("Release: %s", cfg.ReleasePath()))
	PrintInfo(fmt.Sprintf("App:     http://%s:%s", cfg.Host, cfg.ServerPort))
	return nil
}

func copySharedFiles(cfg *DeployConfig) error {
	shared := cfg.SharedPath()

	if _, err := os.Stat(".env"); err == nil {
		if err := CopyFile(cfg, ".env", filepath.Join(shared, ".env")); err != nil {
			return fmt.Errorf("failed to copy .env: %w", err)
		}
	}
	if _, err := os.Stat("config.yaml"); err == nil {
		if err := CopyFile(cfg, "config.yaml", filepath.Join(shared, "config.yaml")); err != nil {
			return fmt.Errorf("failed to copy config.yaml: %w", err)
		}
	}
	return nil
}

// CleanupOldReleases removes releases beyond the keep_releases threshold.
func CleanupOldReleases(cfg *DeployConfig) error {
	releasesDir := filepath.Join(cfg.Path, "releases")
	keep := strconv.Itoa(cfg.KeepReleases)

	// List releases sorted newest first, skip the first N, remove the rest
	cleanupCmd := fmt.Sprintf(
		"cd %s && ls -1t | tail -n +%d | xargs -r rm -rf",
		releasesDir, cfg.KeepReleases+1,
	)

	if cfg.DryRun {
		fmt.Printf("   \033[90m[dry-run] keeping %s releases, removing older\033[0m\n", keep)
		return nil
	}

	return RunRemote(cfg, cleanupCmd)
}
