package deploy

import (
	"fmt"
	"os"
	"path/filepath"
)

const setupTotalSteps = 6

// SetupServer prepares a fresh VPS for deployment.
func SetupServer(cfg *DeployConfig) error {
	step := 0

	fmt.Printf("Setting up %s for %s deployment...\n\n", cfg.Host, cfg.Method)

	// Step 1: Test connectivity
	step++
	PrintStep(step, setupTotalSteps, "Testing SSH connectivity...")
	if _, err := RunRemoteCapture(cfg, "echo ok"); err != nil {
		return fmt.Errorf("cannot connect to %s: %w", cfg.Host, err)
	}
	PrintSuccess("Connected to " + cfg.Host)

	// Step 2: Update packages and install essentials
	step++
	PrintStep(step, setupTotalSteps, "Installing system packages...")
	if err := RunRemote(cfg, "sudo apt-get update -qq && sudo apt-get install -y -qq curl nginx > /dev/null"); err != nil {
		return fmt.Errorf("failed to install packages: %w", err)
	}

	// Step 3: Install Docker (if docker method)
	step++
	if cfg.Method == "docker" {
		PrintStep(step, setupTotalSteps, "Installing Docker...")
		// Check if Docker is already installed
		if _, err := RunRemoteCapture(cfg, "docker --version"); err != nil {
			installDocker := "curl -fsSL https://get.docker.com | sudo sh && sudo usermod -aG docker $(whoami)"
			if err := RunRemote(cfg, installDocker); err != nil {
				return fmt.Errorf("failed to install Docker: %w", err)
			}
			PrintSuccess("Docker installed")
			PrintWarning("You may need to log out and back in for Docker group membership to take effect")
		} else {
			PrintSuccess("Docker already installed")
		}
	} else {
		PrintStep(step, setupTotalSteps, "Creating service user...")
		// Create service user for binary mode
		createUser := fmt.Sprintf(
			"id -u %s &>/dev/null || sudo useradd -r -s /bin/false %s",
			cfg.AppName, cfg.AppName,
		)
		RunRemote(cfg, createUser)
		PrintSuccess("Service user ready")
	}

	// Step 4: Create directory structure
	step++
	PrintStep(step, setupTotalSteps, "Creating directory structure...")
	mkdirCmd := fmt.Sprintf(
		"sudo mkdir -p %s/releases %s/shared && sudo chown -R $(whoami) %s",
		cfg.Path, cfg.Path, cfg.Path,
	)
	if err := RunRemote(cfg, mkdirCmd); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}
	PrintSuccess("Created " + cfg.Path)

	// Step 5: Install nginx config
	step++
	PrintStep(step, setupTotalSteps, "Configuring nginx...")
	nginxConf := "deployments/nginx/app.conf"
	if _, err := os.Stat(nginxConf); err == nil {
		remoteTmp := fmt.Sprintf("/tmp/%s.nginx.conf", cfg.AppName)
		if err := CopyFile(cfg, nginxConf, remoteTmp); err != nil {
			PrintWarning("Failed to copy nginx config: " + err.Error())
		} else {
			installNginx := fmt.Sprintf(
				"sudo cp %s /etc/nginx/sites-available/%s.conf && "+
					"sudo ln -sf /etc/nginx/sites-available/%s.conf /etc/nginx/sites-enabled/ && "+
					"sudo nginx -t && sudo systemctl reload nginx",
				remoteTmp, cfg.AppName, cfg.AppName,
			)
			if err := RunRemote(cfg, installNginx); err != nil {
				PrintWarning("Nginx config installed but reload failed — check config manually")
			} else {
				PrintSuccess("Nginx configured")
			}
		}
	} else {
		// Try the .tmpl version
		nginxTmpl := "deployments/nginx/app.conf.tmpl"
		if _, err := os.Stat(nginxTmpl); err == nil {
			PrintWarning(fmt.Sprintf("Found %s — render it first, then re-run setup", nginxTmpl))
		} else {
			PrintWarning("No nginx config found at " + nginxConf)
		}
	}

	// Step 6: For binary mode, create config directory
	step++
	if cfg.Method == "binary" {
		PrintStep(step, setupTotalSteps, "Creating service directories...")
		svcDir := fmt.Sprintf("/etc/%s", cfg.AppName)
		mkSvcDir := fmt.Sprintf(
			"sudo mkdir -p %s && sudo chown %s:%s %s",
			svcDir, cfg.AppName, cfg.AppName, svcDir,
		)
		RunRemote(cfg, mkSvcDir)
		PrintSuccess("Service directory created at " + svcDir)
	} else {
		PrintStep(step, setupTotalSteps, "Finalizing setup...")
	}

	fmt.Println()
	PrintSuccess(fmt.Sprintf("Server %s is ready for deployment!", cfg.Host))
	fmt.Println()
	PrintInfo("Next steps:")
	PrintInfo(fmt.Sprintf("  gofasta deploy                  # Deploy your application"))
	PrintInfo(fmt.Sprintf("  gofasta deploy status            # Check service status"))
	fmt.Println()

	// Suggest HTTPS setup
	siteConf := filepath.Join("/etc/nginx/sites-available", cfg.AppName+".conf")
	PrintInfo("For HTTPS (recommended):")
	PrintInfo("  sudo apt-get install -y certbot python3-certbot-nginx")
	PrintInfo(fmt.Sprintf("  sudo certbot --nginx -d your-domain.com"))
	_ = siteConf

	return nil
}
