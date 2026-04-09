package deploy

import (
	"fmt"
	"os/exec"
)

// PreflightChecks verifies that all prerequisites are met before deploying.
func PreflightChecks(cfg *DeployConfig) error {
	fmt.Println("Running pre-flight checks...")
	allPassed := true

	// Local tool checks
	if _, err := exec.LookPath("ssh"); err != nil {
		printCheck("ssh", "not found — install OpenSSH", false)
		allPassed = false
	} else {
		printCheck("ssh", "available", true)
	}

	if _, err := exec.LookPath("scp"); err != nil {
		printCheck("scp", "not found — install OpenSSH", false)
		allPassed = false
	} else {
		printCheck("scp", "available", true)
	}

	if cfg.Method == "docker" {
		if _, err := exec.LookPath("docker"); err != nil {
			printCheck("docker (local)", "not found — required for docker deploy method", false)
			allPassed = false
		} else {
			printCheck("docker (local)", "available", true)
		}
	}

	// SSH connectivity
	if cfg.DryRun {
		printCheck("ssh connectivity", "[dry-run] skipped", true)
	} else {
		if _, err := RunRemoteCapture(cfg, "echo ok"); err != nil {
			printCheck("ssh connectivity", fmt.Sprintf("failed to connect to %s: %v", cfg.Host, err), false)
			allPassed = false
		} else {
			printCheck("ssh connectivity", cfg.Host, true)
		}
	}

	// Remote tool checks
	if !allPassed {
		return fmt.Errorf("pre-flight checks failed")
	}

	if cfg.DryRun {
		printCheck("remote tools", "[dry-run] skipped", true)
		fmt.Println()
		return nil
	}

	if cfg.Method == "docker" {
		if _, err := RunRemoteCapture(cfg, "docker --version"); err != nil {
			printCheck("docker (remote)", "not found — run 'gofasta deploy setup' first", false)
			allPassed = false
		} else {
			printCheck("docker (remote)", "available", true)
		}
		if _, err := RunRemoteCapture(cfg, "docker compose version"); err != nil {
			printCheck("docker compose (remote)", "not found — run 'gofasta deploy setup' first", false)
			allPassed = false
		} else {
			printCheck("docker compose (remote)", "available", true)
		}
	} else {
		if _, err := RunRemoteCapture(cfg, "systemctl --version"); err != nil {
			printCheck("systemctl (remote)", "not found — systemd is required for binary deploys", false)
			allPassed = false
		} else {
			printCheck("systemctl (remote)", "available", true)
		}
	}

	fmt.Println()
	if !allPassed {
		return fmt.Errorf("pre-flight checks failed")
	}
	return nil
}

func printCheck(name, info string, ok bool) {
	mark := "\033[32m✓\033[0m"
	if !ok {
		mark = "\033[31m✗\033[0m"
	}
	fmt.Printf("  %s %-25s %s\n", mark, name, info)
}
