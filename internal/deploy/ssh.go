package deploy

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// sshBaseArgs returns the common SSH arguments for connections.
func sshBaseArgs(cfg *DeployConfig) []string {
	return []string{
		"-p", strconv.Itoa(cfg.Port),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
	}
}

// RunRemote executes a command on the remote server, streaming stdout/stderr.
func RunRemote(cfg *DeployConfig, command string) error {
	args := append(sshBaseArgs(cfg), cfg.Host, command)

	if cfg.DryRun {
		fmt.Printf("   \033[90m[dry-run] ssh %s %q\033[0m\n", cfg.Host, command)
		return nil
	}

	cmd := exec.Command("ssh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunRemoteInteractive executes a command on the remote server with full I/O passthrough.
func RunRemoteInteractive(cfg *DeployConfig, command string) error {
	args := append(sshBaseArgs(cfg), cfg.Host, command)

	if cfg.DryRun {
		fmt.Printf("   \033[90m[dry-run] ssh %s %q\033[0m\n", cfg.Host, command)
		return nil
	}

	cmd := exec.Command("ssh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// RunRemoteCapture executes a command on the remote server and captures stdout.
func RunRemoteCapture(cfg *DeployConfig, command string) (string, error) {
	args := append(sshBaseArgs(cfg), cfg.Host, command)

	if cfg.DryRun {
		fmt.Printf("   \033[90m[dry-run] ssh %s %q\033[0m\n", cfg.Host, command)
		return "", nil
	}

	cmd := exec.Command("ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// CopyFile transfers a local file to a remote path via scp.
func CopyFile(cfg *DeployConfig, localPath, remotePath string) error {
	dest := cfg.Host + ":" + remotePath
	args := []string{
		"-P", strconv.Itoa(cfg.Port),
		"-o", "StrictHostKeyChecking=accept-new",
		localPath, dest,
	}

	if cfg.DryRun {
		fmt.Printf("   \033[90m[dry-run] scp %s %s\033[0m\n", localPath, dest)
		return nil
	}

	cmd := exec.Command("scp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CopyDir transfers a local directory recursively to a remote path via scp.
func CopyDir(cfg *DeployConfig, localDir, remoteDir string) error {
	dest := cfg.Host + ":" + remoteDir
	args := []string{
		"-r",
		"-P", strconv.Itoa(cfg.Port),
		"-o", "StrictHostKeyChecking=accept-new",
		localDir, dest,
	}

	if cfg.DryRun {
		fmt.Printf("   \033[90m[dry-run] scp -r %s %s\033[0m\n", localDir, dest)
		return nil
	}

	cmd := exec.Command("scp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunLocalPiped runs a piped shell command locally (e.g., docker save | ssh load).
func RunLocalPiped(cfg *DeployConfig, shellCmd string) error {
	if cfg.DryRun {
		fmt.Printf("   \033[90m[dry-run] sh -c %q\033[0m\n", shellCmd)
		return nil
	}

	cmd := exec.Command("sh", "-c", shellCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunLocal runs a local command with stdout/stderr passthrough.
func RunLocal(cfg *DeployConfig, name string, args ...string) error {
	if cfg.DryRun {
		fmt.Printf("   \033[90m[dry-run] %s %s\033[0m\n", name, strings.Join(args, " "))
		return nil
	}

	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
