package deploy

import (
	"fmt"
	"os"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/cliout"
)

const setupTotalSteps = 5

// migrateCLIVersion pins the golang-migrate CLI installed for binary
// deploys. Keep in sync with the scaffold's Dockerfile.tmpl, which bakes
// the same version into the docker-method image.
const migrateCLIVersion = "v4.18.1"

// SetupServer prepares a fresh VPS for deployment.
func SetupServer(cfg *DeployConfig) error {
	step := 0
	nextStep := func(msg string) {
		step++
		cliout.Step("[%d/%d] %s", step, setupTotalSteps, msg)
	}

	cliout.Plain("Setting up %s for %s deployment...\n\n", cfg.Host, cfg.Method)

	// Step 1: Test connectivity
	nextStep("Testing SSH connectivity...")
	if _, err := RunRemoteCapture(cfg, "echo ok"); err != nil {
		return clierr.Wrapf(clierr.CodeSSHFailed, err, "cannot connect to %s", cfg.Host)
	}
	cliout.Success("Connected to %s", cfg.Host)

	// Step 2: Update packages and install essentials
	nextStep("Installing system packages...")
	if err := RunRemote(cfg, "sudo apt-get update -qq && sudo apt-get install -y -qq curl nginx > /dev/null"); err != nil {
		return clierr.Wrap(clierr.CodeSSHFailed, err, "failed to install packages")
	}

	// Step 3: Method-specific provisioning
	if cfg.Method == "docker" {
		nextStep("Installing Docker...")
		// Check if Docker is already installed
		if _, err := RunRemoteCapture(cfg, "docker --version"); err != nil {
			installDocker := "curl -fsSL https://get.docker.com | sudo sh && sudo usermod -aG docker $(whoami)"
			if err := RunRemote(cfg, installDocker); err != nil {
				return clierr.Wrap(clierr.CodeDockerFailed, err, "failed to install Docker")
			}
			cliout.Success("Docker installed")
			cliout.Warn("You may need to log out and back in for Docker group membership to take effect")
		} else {
			cliout.Success("Docker already installed")
		}
	} else {
		nextStep("Creating service user...")
		// -U creates a matching group — the systemd unit declares both
		// User= and Group=<app>. POSIX redirection (not bash's &>): the
		// remote login shell may be dash.
		createUser := fmt.Sprintf(
			"id -u %s >/dev/null 2>&1 || sudo useradd -r -U -s /usr/sbin/nologin %s",
			cfg.AppName, cfg.AppName,
		)
		if err := RunRemote(cfg, createUser); err != nil {
			return clierr.Wrap(clierr.CodeSSHFailed, err, "failed to create service user")
		}
		cliout.Success("Service user ready")

		// Binary deploys shell out to the golang-migrate CLI on the server
		// (the docker image bakes its own copy). Install it if missing.
		installMigrate := fmt.Sprintf(
			"command -v migrate >/dev/null 2>&1 || "+
				"(curl -fsSL https://github.com/golang-migrate/migrate/releases/download/%s/migrate.linux-%s.tar.gz "+
				"| sudo tar xz -C /usr/local/bin migrate)",
			migrateCLIVersion, cfg.Arch,
		)
		if err := RunRemote(cfg, installMigrate); err != nil {
			cliout.Warn("Failed to install migrate CLI — install it manually before running migrations: %v", err)
		} else {
			cliout.Success("migrate CLI ready")
		}
	}

	// Step 4: Create directory structure
	nextStep("Creating directory structure...")
	mkdirCmd := fmt.Sprintf(
		"sudo mkdir -p %s/releases %s/shared && sudo chown -R $(whoami) %s",
		cfg.Path, cfg.Path, cfg.Path,
	)
	if err := RunRemote(cfg, mkdirCmd); err != nil {
		return clierr.Wrap(clierr.CodeSSHFailed, err, "failed to create directories")
	}
	cliout.Success("Created %s", cfg.Path)

	// Step 5: nginx vhost — only when a real domain is configured.
	// Installing the template's placeholder server_name produced a vhost
	// that matched no request; without a domain, print instructions instead.
	nextStep("Configuring nginx...")
	if cfg.Domain == "" {
		cliout.Info("deploy.domain is not set — skipping nginx vhost install.")
		cliout.Info("To serve behind nginx: set deploy.domain in config.yaml and re-run")
		cliout.Info("`gofasta deploy setup`, or install deployments/nginx/app.conf manually.")
	} else {
		if err := installNginxVhost(cfg); err != nil {
			return err
		}
	}

	cliout.Blank()
	cliout.Success("Server %s is ready for deployment!", cfg.Host)
	cliout.Blank()
	cliout.Info("Next steps:")
	cliout.Info("  gofasta deploy                  # Deploy your application")
	cliout.Info("  gofasta deploy status            # Check service status")
	cliout.Blank()

	// Suggest HTTPS setup
	domain := cfg.Domain
	if domain == "" {
		domain = "your-domain.com"
	}
	cliout.Info("For HTTPS (recommended):")
	cliout.Info("  sudo apt-get install -y certbot python3-certbot-nginx")
	cliout.Info("  sudo certbot --nginx -d %s", domain)

	return nil
}

// installNginxVhost uploads deployments/nginx/app.conf with the configured
// domain and app port substituted in, then enables and reloads nginx.
// cfg.Domain and cfg.ServerPort are validated at config load (hostname /
// numeric), so the sed patterns cannot smuggle shell or sed syntax.
func installNginxVhost(cfg *DeployConfig) error {
	nginxConf := "deployments/nginx/app.conf"
	if _, err := os.Stat(nginxConf); err != nil {
		return clierr.Newf(clierr.CodeDeployConfig,
			"deploy.domain is set but %s was not found — are you in a gofasta project directory?", nginxConf)
	}
	remoteTmp := fmt.Sprintf("/tmp/%s.nginx.conf", cfg.AppName)
	if err := CopyFile(cfg, nginxConf, remoteTmp); err != nil {
		return clierr.Wrap(clierr.CodeSSHFailed, err, "failed to copy nginx config")
	}
	install := fmt.Sprintf(
		"sed -i 's/server_name .*;/server_name %s;/; s/server 127\\.0\\.0\\.1:[0-9]*/server 127.0.0.1:%s/' %s && "+
			"sudo cp %s /etc/nginx/sites-available/%s.conf && "+
			"sudo ln -sf /etc/nginx/sites-available/%s.conf /etc/nginx/sites-enabled/ && "+
			"sudo nginx -t && sudo systemctl reload nginx",
		cfg.Domain, cfg.ServerPort, remoteTmp,
		remoteTmp, cfg.AppName, cfg.AppName,
	)
	if err := RunRemote(cfg, install); err != nil {
		return clierr.Wrap(clierr.CodeSSHFailed, err,
			"nginx configuration failed — check `sudo nginx -t` on the server")
	}
	cliout.Success("Nginx configured for %s", cfg.Domain)
	return nil
}
