package deploy

import (
	"fmt"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
)

// healthProbeCommand builds the remote command that answers "is the app
// serving?" for the active method.
//
// Docker: probe from INSIDE the app container via `compose exec`. The
// published host port can be remapped by a server-side ${PORT} in .env that
// the local config never sees — the container-internal port (config.yaml's
// server.port, baked into the image) is the one address that is always
// right. Runs against the compose file in the CURRENT release so deploys,
// auto-rollbacks, and explicit rollbacks all probe whichever release is
// live.
//
// Binary: curl on the server, sourcing shared/.env first so a server-side
// <PREFIX>_SERVER_PORT override is honored, falling back to the local
// config's server.port.
func healthProbeCommand(cfg *DeployConfig) string {
	if cfg.Method == "docker" {
		return fmt.Sprintf(
			"cd %s && docker compose -p %s -f compose.yaml exec -T app wget -qO /dev/null http://127.0.0.1:%s%s",
			cfg.CurrentPath(), cfg.ComposeProject(), cfg.ServerPort, cfg.HealthPath,
		)
	}
	return fmt.Sprintf(
		"set -a; [ -f %s/.env ] && . %s/.env; curl -sf http://localhost:\"${%s_SERVER_PORT:-%s}\"%s",
		cfg.SharedPath(), cfg.SharedPath(), cfg.EnvPrefix(), cfg.ServerPort, cfg.HealthPath,
	)
}

// CheckHealth polls the health endpoint on the remote server until it responds or times out.
func CheckHealth(cfg *DeployConfig) error {
	checkCmd := healthProbeCommand(cfg)

	if cfg.DryRun {
		dryRunNote("%s (retrying for %ds)", checkCmd, cfg.HealthTimeout)
		return nil
	}

	deadline := time.Now().Add(time.Duration(cfg.HealthTimeout) * time.Second)
	for time.Now().Before(deadline) {
		if _, err := RunRemoteCapture(cfg, checkCmd); err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}

	return clierr.Newf(clierr.CodeHealthCheckFailed,
		"health check failed after %ds — app did not respond at %s", cfg.HealthTimeout, cfg.HealthPath)
}
