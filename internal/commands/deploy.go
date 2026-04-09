package commands

import (
	"fmt"

	"github.com/gofastadev/cli/internal/deploy"
	"github.com/spf13/cobra"
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy the application to a remote server via SSH",
	Long: `Deploy a gofasta project to a VPS using SSH.

Two deployment methods are supported:
  docker  — Build a Docker image, transfer it to the server, run with Docker Compose (default)
  binary  — Cross-compile a Go binary, transfer it via SCP, manage with systemd

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
  - Service user (for binary deploy method)
  - Directory structure
  - Nginx reverse proxy configuration

Run this once before your first deployment.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDeploySetup(cmd)
	},
}

var deployStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of the deployed application",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDeployStatus(cmd)
	},
}

var deployLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Tail application logs from the remote server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDeployLogs(cmd)
	},
}

var deployRollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Rollback to the previous release",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDeployRollback(cmd)
	},
}

func init() {
	deployCmd.Flags().String("host", "", "Deploy target (user@server)")
	deployCmd.Flags().String("method", "", "Deploy method: docker or binary (default: docker)")
	deployCmd.Flags().Int("port", 0, "SSH port (default: 22)")
	deployCmd.Flags().String("path", "", "Remote deploy directory (default: /opt/<appname>)")
	deployCmd.Flags().String("arch", "", "Target architecture: amd64 or arm64 (default: amd64)")
	deployCmd.Flags().Bool("dry-run", false, "Show commands without executing")

	// Inherit flags for subcommands that need them
	deploySetupCmd.Flags().String("host", "", "Deploy target (user@server)")
	deploySetupCmd.Flags().String("method", "", "Deploy method: docker or binary (default: docker)")
	deploySetupCmd.Flags().Int("port", 0, "SSH port (default: 22)")
	deploySetupCmd.Flags().String("path", "", "Remote deploy directory")
	deploySetupCmd.Flags().String("arch", "", "Target architecture")

	deployStatusCmd.Flags().String("host", "", "Deploy target (user@server)")
	deployStatusCmd.Flags().String("method", "", "Deploy method: docker or binary")
	deployStatusCmd.Flags().Int("port", 0, "SSH port")
	deployStatusCmd.Flags().String("path", "", "Remote deploy directory")
	deployStatusCmd.Flags().String("arch", "", "Target architecture")

	deployLogsCmd.Flags().String("host", "", "Deploy target (user@server)")
	deployLogsCmd.Flags().String("method", "", "Deploy method: docker or binary")
	deployLogsCmd.Flags().Int("port", 0, "SSH port")
	deployLogsCmd.Flags().String("path", "", "Remote deploy directory")
	deployLogsCmd.Flags().String("arch", "", "Target architecture")

	deployRollbackCmd.Flags().String("host", "", "Deploy target (user@server)")
	deployRollbackCmd.Flags().String("method", "", "Deploy method: docker or binary")
	deployRollbackCmd.Flags().Int("port", 0, "SSH port")
	deployRollbackCmd.Flags().String("path", "", "Remote deploy directory")
	deployRollbackCmd.Flags().String("arch", "", "Target architecture")

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

	fmt.Printf("Deploying %s to %s (%s method)...\n\n", cfg.AppName, cfg.Host, cfg.Method)

	switch cfg.Method {
	case "docker":
		return deploy.DeployDocker(cfg)
	case "binary":
		return deploy.DeployBinary(cfg)
	default:
		return fmt.Errorf("unknown deploy method: %s", cfg.Method)
	}
}

func runDeploySetup(cmd *cobra.Command) error {
	cfg, err := deploy.LoadDeployConfig(cmd)
	if err != nil {
		return err
	}
	return deploy.SetupServer(cfg)
}

func runDeployStatus(cmd *cobra.Command) error {
	cfg, err := deploy.LoadDeployConfig(cmd)
	if err != nil {
		return err
	}

	fmt.Printf("Checking status of %s on %s...\n\n", cfg.AppName, cfg.Host)

	// Show current release
	current, err := deploy.RunRemoteCapture(cfg, fmt.Sprintf("readlink %s 2>/dev/null || echo 'no current release'", cfg.CurrentPath()))
	if err == nil {
		deploy.PrintInfo("Current release: " + current)
	}

	fmt.Println()

	// Show service status
	if cfg.Method == "docker" {
		composePath := fmt.Sprintf("%s/compose.yaml", cfg.CurrentPath())
		return deploy.RunRemote(cfg, fmt.Sprintf("cd %s && docker compose -f %s ps 2>/dev/null || echo 'No containers running'", cfg.CurrentPath(), composePath))
	}
	return deploy.RunRemote(cfg, fmt.Sprintf("sudo systemctl status %s --no-pager 2>/dev/null || echo 'Service not found'", cfg.AppName))
}

func runDeployLogs(cmd *cobra.Command) error {
	cfg, err := deploy.LoadDeployConfig(cmd)
	if err != nil {
		return err
	}

	fmt.Printf("Tailing logs for %s on %s (Ctrl+C to stop)...\n\n", cfg.AppName, cfg.Host)

	if cfg.Method == "docker" {
		composePath := fmt.Sprintf("%s/compose.yaml", cfg.CurrentPath())
		return deploy.RunRemoteInteractive(cfg, fmt.Sprintf("cd %s && docker compose -f %s logs -f --tail 100", cfg.CurrentPath(), composePath))
	}
	return deploy.RunRemoteInteractive(cfg, fmt.Sprintf("sudo journalctl -u %s -f -n 100", cfg.AppName))
}

func runDeployRollback(cmd *cobra.Command) error {
	cfg, err := deploy.LoadDeployConfig(cmd)
	if err != nil {
		return err
	}
	return deploy.Rollback(cfg)
}
