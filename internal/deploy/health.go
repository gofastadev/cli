package deploy

import (
	"fmt"
	"time"
)

// CheckHealth polls the health endpoint on the remote server until it responds or times out.
func CheckHealth(cfg *DeployConfig) error {
	if cfg.DryRun {
		fmt.Printf("   \033[90m[dry-run] curl -sf http://localhost:%s%s (retrying for %ds)\033[0m\n",
			cfg.ServerPort, cfg.HealthPath, cfg.HealthTimeout)
		return nil
	}

	checkCmd := fmt.Sprintf("curl -sf http://localhost:%s%s", cfg.ServerPort, cfg.HealthPath)
	deadline := time.Now().Add(time.Duration(cfg.HealthTimeout) * time.Second)

	for time.Now().Before(deadline) {
		if _, err := RunRemoteCapture(cfg, checkCmd); err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("health check failed after %ds — app did not respond at %s%s",
		cfg.HealthTimeout, "localhost:"+cfg.ServerPort, cfg.HealthPath)
}
