package deploy

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
)

const dockerTotalSteps = 11

// releaseImageFile is written into every docker release directory and holds
// the exact image tag that release runs. Auto-rollback, `deploy rollback`,
// and release cleanup all read it — it is the link between a release
// directory and the image `docker save | ssh docker load` shipped for it.
const releaseImageFile = "RELEASE_IMAGE"

// DeployDocker deploys the application using Docker compose on the remote server.
//
// Cutover model: restart-based with automatic rollback. One fixed compose
// project (stable containers, network, and data volume across releases);
// the deploy pins the transferred image via APP_IMAGE, restarts the app
// service onto it (a brief blip, not zero-downtime), gates on the health
// endpoint, and on failure restarts the previous release's image. The
// `current` symlink is the bookkeeping/rollback pointer.
//
//nolint:revive // name kept for public-API stability; rename is a breaking change.
func DeployDocker(cfg *DeployConfig) error {
	step := 0
	nextStep := func(msg string) {
		step++
		cliout.Step("[%d/%d] %s", step, dockerTotalSteps, msg)
	}

	// Step 1: Pre-flight
	nextStep("Running pre-flight checks...")
	if err := PreflightChecks(cfg); err != nil {
		return err
	}

	// Step 2: Build Docker image
	nextStep("Building Docker image...")
	imageTag := cfg.AppName + ":" + cfg.ReleaseTag
	if err := RunLocal(cfg, "docker", "build", "-t", imageTag, "."); err != nil {
		return clierr.Wrap(clierr.CodeDockerFailed, err, "docker build failed")
	}

	// Steps 3-7: stage the release on the server (directories, image
	// transfer, shared config, compose file, RELEASE_IMAGE, previous-release
	// bookkeeping) — everything up to but not including the cutover.
	prevRelease, prevImage, err := stageDockerRelease(cfg, imageTag, nextStep)
	if err != nil {
		return err
	}

	// Step 8: Cutover — flip the pointer and restart the fixed compose
	// project onto the pinned image. Brief restart blip by design.
	nextStep("Cutting over (brief restart)...")
	if err := RunRemote(cfg, composeUpCommand(cfg, cfg.ReleasePath(), imageTag)); err != nil {
		return autoRollbackDocker(cfg, prevRelease, prevImage,
			clierr.Wrap(clierr.CodeDockerFailed, err, "docker compose up failed"))
	}
	if err := RunRemote(cfg, symlinkCommand(cfg, cfg.ReleasePath())); err != nil {
		return autoRollbackDocker(cfg, prevRelease, prevImage,
			clierr.Wrap(clierr.CodeSSHFailed, err, "failed to update current symlink"))
	}

	// Step 9: Run migrations via the app's own `migrate` subcommand, which
	// loads DB credentials from config.yaml + the {{PREFIX}}_DATABASE_* env
	// (the container image bakes in the migrate CLI). A failed migration
	// aborts the deploy and rolls back: new code running against a schema it
	// didn't get is exactly the state the health gate exists to prevent.
	// NOTE: already-applied migration steps are NOT reverted (golang-migrate
	// has no automatic undo) — the rollback restores the previous CODE.
	nextStep("Running database migrations...")
	migrateCmd := fmt.Sprintf("cd %s && docker compose -p %s -f compose.yaml exec -T app /app migrate up",
		cfg.ReleasePath(), cfg.ComposeProject())
	if err := RunRemote(cfg, migrateCmd); err != nil {
		return autoRollbackDocker(cfg, prevRelease, prevImage,
			clierr.Wrap(clierr.CodeMigrationFailed, err, "database migrations failed"))
	}

	// Step 10: Health gate
	nextStep("Checking application health...")
	if err := CheckHealth(cfg); err != nil {
		return autoRollbackDocker(cfg, prevRelease, prevImage, err)
	}
	cliout.Success("Application is healthy")

	// Step 11: Cleanup old releases
	nextStep("Cleaning up old releases...")
	if err := CleanupOldReleases(cfg); err != nil {
		cliout.Warn("Cleanup warning: %v", err)
	}

	cliout.Blank()
	cliout.Success("Deployed %s to %s (docker)", cfg.ReleaseTag, cfg.Host)
	cliout.Info("Release: %s", cfg.ReleasePath())
	cliout.Info("App:     http://%s:%s", cfg.Host, cfg.ServerPort)
	return nil
}

// stageDockerRelease performs the pre-cutover remote staging: release +
// shared directories, image transfer, shared-config linking, compose file,
// the RELEASE_IMAGE record, and the previous-release lookup that automatic
// and manual rollback depend on.
func stageDockerRelease(cfg *DeployConfig, imageTag string, nextStep func(string)) (prevRelease, prevImage string, err error) {
	// Create release directory on remote
	nextStep("Creating release directory...")
	mkdirCmd := fmt.Sprintf("mkdir -p %s %s", cfg.ReleasePath(), cfg.SharedPath())
	if err := RunRemote(cfg, mkdirCmd); err != nil {
		return "", "", clierr.Wrap(clierr.CodeSSHFailed, err, "failed to create remote directories")
	}

	// Transfer Docker image. Built as two argv commands piped in-process
	// (docker save | ssh ... docker load) rather than a `sh -c "<string>"` so
	// that cfg.Host / imageTag (which embed values from the project's config.yaml
	// and go.mod) can never inject a local shell command.
	nextStep("Transferring Docker image to server...")
	saveArgs := []string{"docker", "save", imageTag}
	loadArgs := append([]string{"ssh"}, sshBaseArgs(cfg)...)
	loadArgs = append(loadArgs, "--", cfg.Host, "docker load")
	if err := RunLocalPiped(cfg, saveArgs, loadArgs); err != nil {
		return "", "", clierr.Wrap(clierr.CodeDockerFailed, err, "docker image transfer failed")
	}

	// Copy shared config files, then link them into the release dir so
	// `docker compose` (which reads .env only from its working directory)
	// picks up the real credentials. Without this link, compose interpolates
	// empty DB credentials and the database container never becomes healthy.
	nextStep("Uploading configuration files...")
	if err := copySharedFiles(cfg); err != nil {
		return "", "", err
	}
	if err := linkSharedInto(cfg, cfg.ReleasePath()); err != nil {
		return "", "", clierr.Wrap(clierr.CodeSSHFailed, err, "failed to link shared config into the release")
	}

	// Copy compose file and record the release's image tag
	nextStep("Uploading compose file...")
	composeFile := "deployments/docker/compose.production.yaml"
	if _, err := os.Stat(composeFile); err != nil {
		return "", "", clierr.Newf(clierr.CodeDeployConfig,
			"compose file not found at %s — are you in a gofasta project directory?", composeFile)
	}
	if err := CopyFile(cfg, composeFile, filepath.Join(cfg.ReleasePath(), "compose.yaml")); err != nil {
		return "", "", clierr.Wrap(clierr.CodeSSHFailed, err, "failed to copy compose file")
	}
	recordImage := fmt.Sprintf("printf '%%s' %s > %s",
		imageTag, filepath.Join(cfg.ReleasePath(), releaseImageFile))
	if err := RunRemote(cfg, recordImage); err != nil {
		return "", "", clierr.Wrap(clierr.CodeSSHFailed, err, "failed to record the release image tag")
	}

	// Record the previous release BEFORE cutover so both automatic and
	// manual rollback know exactly where to go back to.
	nextStep("Recording previous release...")
	prevRelease, prevImage = currentDockerRelease(cfg)
	if prevRelease != "" {
		cliout.Info("Previous release: %s", prevRelease)
	} else {
		cliout.Info("First deploy — no previous release to fall back to")
	}
	return prevRelease, prevImage, nil
}

// composeUpCommand starts (or restarts) the fixed compose project from the
// given release directory with APP_IMAGE pinned. The stable -p project name
// is what makes containers, the network, and the db volume survive releases
// instead of colliding with the previous release's compose project.
func composeUpCommand(cfg *DeployConfig, releasePath, imageTag string) string {
	return fmt.Sprintf("cd %s && APP_IMAGE=%s docker compose -p %s -f compose.yaml up -d --remove-orphans",
		releasePath, imageTag, cfg.ComposeProject())
}

// symlinkCommand atomically points `current` at the given release.
func symlinkCommand(cfg *DeployConfig, releasePath string) string {
	return fmt.Sprintf("ln -sfn %s %s", releasePath, cfg.CurrentPath())
}

// linkSharedInto links shared/.env and shared/config.yaml into a release
// directory — only the files that actually exist, and failing loudly on a
// real link error instead of masking it with 2>/dev/null.
func linkSharedInto(cfg *DeployConfig, releasePath string) error {
	cmd := fmt.Sprintf(
		"for f in .env config.yaml; do if [ -f %s/$f ]; then ln -sf %s/$f %s/$f || exit 1; fi; done",
		cfg.SharedPath(), cfg.SharedPath(), releasePath,
	)
	return RunRemote(cfg, cmd)
}

// currentDockerRelease returns the path and pinned image of the release
// `current` points at, or empty strings on a first deploy.
func currentDockerRelease(cfg *DeployConfig) (releasePath, imageTag string) {
	out, err := RunRemoteCapture(cfg, fmt.Sprintf("readlink %s", cfg.CurrentPath()))
	if err != nil || out == "" {
		return "", ""
	}
	img, err := RunRemoteCapture(cfg, fmt.Sprintf("cat %s 2>/dev/null", filepath.Join(out, releaseImageFile)))
	if err != nil {
		return out, ""
	}
	return out, img
}

// autoRollbackDocker restores the previous release after a failed cutover,
// migration, or health check. The failed release directory and its image are
// removed on success so a later `deploy rollback` can never land on them.
// On a first deploy (nothing to restore) the broken release is left running
// for inspection.
func autoRollbackDocker(cfg *DeployConfig, prevRelease, prevImage string, cause error) error {
	cliout.Fail("Deploy failed: %v", cause)

	if prevRelease == "" {
		cliout.Warn("First deploy — no previous release to roll back to")
		cliout.Info("The failed release is left running for inspection:")
		cliout.Info("  gofasta deploy logs      # container logs")
		cliout.Info("  gofasta deploy status    # container state")
		return cause
	}
	if prevImage == "" {
		cliout.Warn("Previous release has no %s file — cannot auto-rollback; run `gofasta deploy rollback` manually", releaseImageFile)
		return cause
	}

	cliout.Warn("Rolling back to previous release %s...", filepath.Base(prevRelease))
	if err := RunRemote(cfg, composeUpCommand(cfg, prevRelease, prevImage)); err != nil {
		cliout.Fail("Automatic rollback failed: %v", err)
		return clierr.Wrapf(clierr.CodeRollbackFailed, cause,
			"deploy failed AND automatic rollback failed (%v) — the app may be down, inspect with `gofasta deploy logs`", err)
	}
	if err := RunRemote(cfg, symlinkCommand(cfg, prevRelease)); err != nil {
		cliout.Warn("Could not restore the current symlink: %v", err)
	}
	if err := CheckHealth(cfg); err != nil {
		cliout.Fail("Previous release did not become healthy after rollback: %v", err)
		return clierr.Wrap(clierr.CodeRollbackFailed, cause,
			"deploy failed and the restored release is unhealthy — inspect with `gofasta deploy logs`")
	}

	// The broken release must not linger: `deploy rollback` targets the
	// newest release older than current, and a failed directory newer than
	// current would shadow the real rollback target.
	cleanupFailed := fmt.Sprintf("docker rmi %s:%s >/dev/null 2>&1; rm -rf %s",
		cfg.AppName, cfg.ReleaseTag, cfg.ReleasePath())
	if err := RunRemote(cfg, cleanupFailed); err != nil {
		cliout.Warn("Could not remove the failed release %s: %v", cfg.ReleaseTag, err)
	}

	cliout.Success("Rolled back — previous release %s is live again", filepath.Base(prevRelease))
	return cause
}

func copySharedFiles(cfg *DeployConfig) error {
	shared := cfg.SharedPath()

	if _, err := os.Stat(".env"); err == nil {
		if err := CopyFile(cfg, ".env", filepath.Join(shared, ".env")); err != nil {
			return clierr.Wrap(clierr.CodeSSHFailed, err, "failed to copy .env")
		}
	}
	if _, err := os.Stat("config.yaml"); err == nil {
		if err := CopyFile(cfg, "config.yaml", filepath.Join(shared, "config.yaml")); err != nil {
			return clierr.Wrap(clierr.CodeSSHFailed, err, "failed to copy config.yaml")
		}
	}
	return nil
}

// CleanupOldReleases removes releases beyond the keep_releases threshold.
// Releases are ordered by NAME (the timestamp tags sort lexicographically),
// not mtime — a rollback must never change which releases get pruned. The
// release `current` points at is always kept, and each removed docker
// release's pinned image is deleted from the server so /var/lib/docker
// doesn't grow one orphaned image per deploy forever.
func CleanupOldReleases(cfg *DeployConfig) error {
	if cfg.DryRun {
		dryRunNote("keeping %d releases (always including current), removing older + their images", cfg.KeepReleases)
		return nil
	}

	script := fmt.Sprintf(`cd %s 2>/dev/null || exit 0
current=$(basename "$(readlink %s 2>/dev/null)" 2>/dev/null)
kept=0
for r in $(ls -1 | sort -r); do
  if [ "$r" = "$current" ] || [ "$kept" -lt %d ]; then
    kept=$((kept+1))
    continue
  fi
  if [ -f "$r/%s" ]; then docker rmi "$(cat "$r/%s")" >/dev/null 2>&1 || true; fi
  rm -rf -- "$r"
done`,
		cfg.ReleasesDir(), cfg.CurrentPath(), cfg.KeepReleases, releaseImageFile, releaseImageFile)

	return RunRemote(cfg, script)
}
