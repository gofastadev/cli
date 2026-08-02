package commands

import (
	"fmt"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
	"github.com/gofastadev/cli/internal/deploy"
	"github.com/spf13/cobra"
)

// deployResult is the JSON contract for `gofasta deploy --json`. The
// shape covers both the top-level deploy and its read-only siblings
// (status, setup, rollback) — discriminated by Action so a single
// parser handles all of them. Logs is a streaming command and refuses
// in JSON mode.
type deployResult struct {
	Action  string `json:"action"`
	Method  string `json:"method,omitempty"`
	Host    string `json:"host,omitempty"`
	App     string `json:"app,omitempty"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy the application to a remote server via SSH",
	Long: `Deploy a gofasta project to a VPS using SSH.

Two deployment methods are supported:
  docker  — Build a Docker image, transfer it to the server, run with Docker Compose (default)
  binary  — Cross-compile a Go binary, transfer it via SCP, manage with systemd

Each deploy creates a release directory under <path>/releases/<timestamp>
and restarts the app onto it (a brief restart, not zero-downtime). The
deploy then polls the health endpoint; if the new release does not become
healthy, gofasta automatically rolls back to the previous release. The
` + "`current`" + ` symlink always points at the live release.

Configuration is read from the deploy: section in config.yaml. Flags override config values.

Prerequisites:
  - SSH key-based access to the target server
  - For docker method: Docker installed locally and on the server
  - For binary method: systemd on the server

First-time setup:
  gofasta deploy setup              # Prepare the server (install Docker/nginx, create dirs)

Examples:
  gofasta deploy                    # Deploy using config.yaml settings
  gofasta deploy --host user@server # Override host
  gofasta deploy --method binary    # Deploy as compiled binary
  gofasta deploy --dry-run          # Preview without executing`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDeploy(cmd)
	},
}

var deploySetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Prepare a remote server for deployment",
	Long: `Install prerequisites on a fresh VPS:
  - System packages (curl, nginx)
  - Docker (for docker deploy method)
  - Service user + migrate CLI (for binary deploy method)
  - Directory structure (<path>/releases, <path>/shared)
  - Nginx reverse proxy vhost — only when deploy.domain is set in config.yaml

Run this once before your first deployment. Put your production .env on
the server at <path>/shared/.env afterwards (deploys reuse it).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDeploySetup(cmd)
	},
}

var deployStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current release and service status on the remote server",
	Long: `Resolve the current release symlink on the remote host and show service
status for whichever deploy method is configured:

  docker   — ` + "`docker compose ps`" + ` against the deployed compose file
  binary   — ` + "`systemctl status <appname>`" + `

Read-only — makes no changes on the remote host. Config is loaded from
config.yaml and can be overridden with the same flags as ` + "`gofasta deploy`" + `.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDeployStatus(cmd)
	},
}

var deployLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Tail the application log stream from the remote server",
	Long: `Stream the last 100 lines of application logs and follow new output
until interrupted. Source depends on deploy method:

  docker   — ` + "`docker compose logs -f`" + ` for the current release
  binary   — ` + "`journalctl -u <appname> -f`" + `

Read-only — makes no changes. Press Ctrl+C to stop tailing.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDeployLogs(cmd)
	},
}

var deployRollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Restart the service on the previous release",
	Long: `Roll the remote service back to the newest release older than the one
` + "`current`" + ` points at: restart onto it (docker: the release's pinned image;
binary: the release's binary), verify its health endpoint, and only then
leave the current pointer on it. If the rollback target turns out
unhealthy, the release that was live before is restored.

Use this when a deploy has caused a regression and you need to revert
without re-running the full deploy pipeline. It does not touch any local
state or revert git.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDeployRollback(cmd)
	},
}

func init() {
	// Persistent: one flag set shared by deploy and every subcommand, so
	// --dry-run/--host/--port/... work uniformly (the previous per-command
	// copies silently omitted --dry-run from every subcommand).
	deployCmd.PersistentFlags().String("host", "", "Deploy target (user@server)")
	deployCmd.PersistentFlags().String("method", "", "Deploy method: docker or binary (default: docker)")
	deployCmd.PersistentFlags().Int("port", 0, "SSH port (default: 22)")
	deployCmd.PersistentFlags().String("path", "", "Remote deploy directory (default: /opt/<appname>)")
	deployCmd.PersistentFlags().String("arch", "", "Target architecture: amd64 or arm64 (default: amd64)")
	deployCmd.PersistentFlags().Bool("dry-run", false, "Show commands without executing")

	deployCmd.AddCommand(deploySetupCmd)
	deployCmd.AddCommand(deployStatusCmd)
	deployCmd.AddCommand(deployLogsCmd)
	deployCmd.AddCommand(deployRollbackCmd)
	rootCmd.AddCommand(deployCmd)
}

func runDeploy(cmd *cobra.Command) error {
	cfg, err := deploy.LoadDeployConfig(cmd)
	if err != nil {
		return err
	}

	if !cliout.JSON() {
		cliout.Plain("Deploying %s to %s (%s method)...\n\n", cfg.AppName, cfg.Host, cfg.Method)
	}

	deployErr := runDeployMethod(cfg)
	emitDeployResult("deploy", cfg, deployErr)
	return deployErr
}

// runDeployMethod dispatches on the validated method. Split from runDeploy
// so the impossible-method branch is unit-testable with a hand-built config
// (LoadDeployConfig never returns other values).
func runDeployMethod(cfg *deploy.DeployConfig) error {
	switch cfg.Method {
	case "docker":
		return deploy.DeployDocker(cfg)
	case "binary":
		return deploy.DeployBinary(cfg)
	default:
		return clierr.Newf(clierr.CodeDeployConfig, "unknown deploy method: %s", cfg.Method)
	}
}

func runDeploySetup(cmd *cobra.Command) error {
	cfg, err := deploy.LoadDeployConfig(cmd)
	if err != nil {
		return err
	}
	setupErr := deploy.SetupServer(cfg)
	emitDeployResult("deploy.setup", cfg, setupErr)
	return setupErr
}

func runDeployStatus(cmd *cobra.Command) error {
	cfg, err := deploy.LoadDeployConfig(cmd)
	if err != nil {
		return err
	}

	if !cliout.JSON() {
		cliout.Plain("Checking status of %s on %s...\n\n", cfg.AppName, cfg.Host)
	}

	// Show current release
	current, err := deploy.RunRemoteCapture(cfg, fmt.Sprintf("readlink %s 2>/dev/null || echo 'no current release'", cfg.CurrentPath()))
	if err == nil && !cliout.JSON() {
		cliout.Info("Current release: %s", current)
	}

	if !cliout.JSON() {
		cliout.Blank()
	}

	// Show service status — service stdout (docker compose ps / systemctl
	// status) is captured via RunRemoteCapture in JSON mode so it lands
	// in the structured result rather than mixing with the JSON document.
	var serviceStatus string
	var statusErr error
	switch cfg.Method {
	case "docker":
		// -p keeps the query on the same fixed compose project the deploy
		// uses; without it, compose derives a project name from the release
		// directory and reports the wrong (empty) project.
		query := fmt.Sprintf("cd %s && docker compose -p %s -f compose.yaml ps 2>/dev/null || echo 'No containers running'",
			cfg.CurrentPath(), cfg.ComposeProject())
		if cliout.JSON() {
			serviceStatus, statusErr = deploy.RunRemoteCapture(cfg, query)
		} else {
			statusErr = deploy.RunRemote(cfg, query)
		}
	default:
		query := fmt.Sprintf("sudo systemctl status %s --no-pager 2>/dev/null || echo 'Service not found'", cfg.AppName)
		if cliout.JSON() {
			serviceStatus, statusErr = deploy.RunRemoteCapture(cfg, query)
		} else {
			statusErr = deploy.RunRemote(cfg, query)
		}
	}

	if cliout.JSON() {
		cliout.Print(struct {
			deployResult
			CurrentRelease string `json:"current_release,omitempty"`
			ServiceStatus  string `json:"service_status,omitempty"`
		}{
			deployResult: deployResult{
				Action:  "deploy.status",
				Method:  cfg.Method,
				Host:    cfg.Host,
				App:     cfg.AppName,
				Success: statusErr == nil,
				Error:   errString(statusErr),
			},
			CurrentRelease: current,
			ServiceStatus:  serviceStatus,
		}, nil)
	}
	return statusErr
}

func runDeployLogs(cmd *cobra.Command) error {
	// Tailing remote logs is an interactive stream — Ctrl+C to stop —
	// so refuse in JSON mode rather than dumping unstructured text into
	// what the agent expects to be a JSON document. For programmatic
	// log access, agents should ssh directly with `journalctl --output=json`.
	if cliout.JSON() {
		return clierr.Newf(clierr.CodeInteractiveOnly,
			"`deploy logs` is an interactive log tail and cannot run in --json mode")
	}
	cfg, err := deploy.LoadDeployConfig(cmd)
	if err != nil {
		return err
	}

	cliout.Plain("Tailing logs for %s on %s (Ctrl+C to stop)...\n\n", cfg.AppName, cfg.Host)

	if cfg.Method == "docker" {
		return deploy.RunRemoteInteractive(cfg, fmt.Sprintf("cd %s && docker compose -p %s -f compose.yaml logs -f --tail 100",
			cfg.CurrentPath(), cfg.ComposeProject()))
	}
	return deploy.RunRemoteInteractive(cfg, fmt.Sprintf("sudo journalctl -u %s -f -n 100", cfg.AppName))
}

func runDeployRollback(cmd *cobra.Command) error {
	cfg, err := deploy.LoadDeployConfig(cmd)
	if err != nil {
		return err
	}
	rbErr := deploy.Rollback(cfg)
	emitDeployResult("deploy.rollback", cfg, rbErr)
	return rbErr
}

// emitDeployResult prints a structured result in JSON mode. Text mode
// is silent here because the deploy package is already streaming progress
// messages through cliout; the operation's outcome is conveyed by the
// exit code (returned err).
func emitDeployResult(action string, cfg *deploy.DeployConfig, err error) {
	if !cliout.JSON() {
		return
	}
	cliout.Print(deployResult{
		Action:  action,
		Method:  cfg.Method,
		Host:    cfg.Host,
		App:     cfg.AppName,
		Success: err == nil,
		Error:   errString(err),
	}, nil)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
