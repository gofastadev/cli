package deploy

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
)

// Rollback reverts to the release immediately older than the one `current`
// points at.
//
// Selection is by NAME (release tags are UTC timestamps, so lexicographic
// order is chronological order) and strictly older than current — a leftover
// directory NEWER than current (e.g. from an interrupted deploy) must never
// be chosen. The previous implementation picked "newest directory that isn't
// current" by mtime, which after a failed deploy re-activated the exact
// release that had just failed.
//
// Order mirrors a deploy's cutover: activate the target, health-check it,
// and only then leave the `current` pointer on it; if the target turns out
// unhealthy the pre-rollback release is restored.
func Rollback(cfg *DeployConfig) error {
	cliout.Plain("Rolling back %s on %s...\n\n", cfg.AppName, cfg.Host)

	// Dry-run has no remote state to inspect (captures return empty), so
	// describe the plan instead of tripping the safety checks below.
	if cfg.DryRun {
		dryRunNote("would list %s, pick the newest release older than current, activate it, health-check, then move the pointer", cfg.ReleasesDir())
		return nil
	}

	// Step 1: Find the target release
	cliout.Step("[1/4] Finding previous release...")
	output, err := RunRemoteCapture(cfg, fmt.Sprintf("ls -1 %s", cfg.ReleasesDir()))
	if err != nil {
		return clierr.Wrap(clierr.CodeRollbackFailed, err, "failed to list releases")
	}
	releases := strings.Fields(strings.TrimSpace(output))
	sort.Sort(sort.Reverse(sort.StringSlice(releases)))
	if len(releases) < 2 {
		return clierr.Newf(clierr.CodeRollbackFailed,
			"no previous release to rollback to (only %d release found)", len(releases))
	}

	currentOutput, err := RunRemoteCapture(cfg, fmt.Sprintf("readlink %s", cfg.CurrentPath()))
	if err != nil || strings.TrimSpace(currentOutput) == "" {
		// Guessing here is how a rollback ends up "restoring" the release
		// that is already live (or worse). Without a trustworthy pointer,
		// stop.
		return clierr.Newf(clierr.CodeRollbackFailed,
			"cannot read the current release pointer at %s — refusing to guess a rollback target (%v)",
			cfg.CurrentPath(), err)
	}
	current := filepath.Base(strings.TrimSpace(currentOutput))
	currentPath := filepath.Join(cfg.ReleasesDir(), current)

	var previous string
	for _, r := range releases {
		if r < current {
			previous = r
			break
		}
	}
	if previous == "" {
		return clierr.Newf(clierr.CodeRollbackFailed,
			"no release older than the current one (%s) to rollback to", current)
	}

	previousPath := filepath.Join(cfg.ReleasesDir(), previous)
	cliout.Info("Current:  %s", current)
	cliout.Info("Rolling back to: %s", previous)
	cliout.Blank()

	// Step 2: Activate the target release
	cliout.Step("[2/4] Activating previous release...")
	if err := activateRelease(cfg, previousPath); err != nil {
		return err
	}

	// Step 3: Health-gate the target before committing the pointer to it
	cliout.Step("[3/4] Checking application health...")
	// CheckHealth (docker mode) probes via the `current` symlink, so the
	// pointer must move before the probe; on failure it is moved back.
	if err := RunRemote(cfg, symlinkCommand(cfg, previousPath)); err != nil {
		return clierr.Wrap(clierr.CodeRollbackFailed, err, "failed to update current symlink")
	}
	if err := CheckHealth(cfg); err != nil {
		cliout.Fail("Rollback target %s is unhealthy — restoring %s", previous, current)
		if restoreErr := activateRelease(cfg, currentPath); restoreErr != nil {
			return clierr.Wrapf(clierr.CodeRollbackFailed, err,
				"rollback target is unhealthy AND restoring %s failed (%v) — the app may be down", current, restoreErr)
		}
		if linkErr := RunRemote(cfg, symlinkCommand(cfg, currentPath)); linkErr != nil {
			cliout.Warn("Could not restore the current symlink: %v", linkErr)
		}
		return clierr.Wrapf(clierr.CodeRollbackFailed, err,
			"rollback target %s failed its health check — %s was restored", previous, current)
	}

	// Step 4: Done — pointer already on the healthy target
	cliout.Step("[4/4] Finalizing...")
	cliout.Blank()
	cliout.Success("Rolled back to release %s", previous)
	return nil
}

// activateRelease starts the given release with the method's cutover
// mechanics (fixed compose project + pinned image, or binary reinstall +
// systemd restart).
func activateRelease(cfg *DeployConfig, releasePath string) error {
	if cfg.Method == "docker" {
		imageTag, err := RunRemoteCapture(cfg,
			fmt.Sprintf("cat %s", filepath.Join(releasePath, releaseImageFile)))
		if err != nil || (imageTag == "" && !cfg.DryRun) {
			return clierr.Newf(clierr.CodeRollbackFailed,
				"release %s has no %s file (deployed by an older gofasta version?) — redeploy it instead: %v",
				filepath.Base(releasePath), releaseImageFile, err)
		}
		if err := RunRemote(cfg, composeUpCommand(cfg, releasePath, imageTag)); err != nil {
			return clierr.Wrap(clierr.CodeRollbackFailed, err, "failed to start release containers")
		}
		return nil
	}
	if err := RunRemote(cfg, binaryCutoverCommand(cfg, releasePath)); err != nil {
		return clierr.Wrap(clierr.CodeRollbackFailed, err, "failed to reinstall release binary")
	}
	return nil
}
