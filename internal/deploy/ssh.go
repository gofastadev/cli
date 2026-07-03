package deploy

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// execCommand is a package-level seam for tests.
var execCommand = exec.Command

// strictHostKeyPolicy returns the ssh StrictHostKeyChecking value for this
// deploy. It defaults to "accept-new" (trust-on-first-use, then pin) so an
// automated first deploy isn't blocked, but security-conscious operators can
// set deploy.strict_host_key: "yes" in config.yaml to require the host key to
// already be in known_hosts.
func strictHostKeyPolicy(cfg *DeployConfig) string {
	if cfg.StrictHostKey != "" {
		return cfg.StrictHostKey
	}
	return "accept-new"
}

// sshBaseArgs returns the common SSH arguments for connections.
func sshBaseArgs(cfg *DeployConfig) []string {
	return []string{
		"-p", strconv.Itoa(cfg.Port),
		"-o", "StrictHostKeyChecking=" + strictHostKeyPolicy(cfg),
		"-o", "ConnectTimeout=10",
	}
}

// RunRemote executes a command on the remote server, streaming stdout/stderr.
// In --json mode the remote stdout is redirected to stderr so the caller's
// final JSON document on stdout stays uncorrupted.
//
// The "--" separator before cfg.Host is a security boundary: without it, a
// Host value beginning with "-" (e.g. "-oProxyCommand=<payload>") would be
// parsed by ssh as an option and execute arbitrary local commands
// (CVE-2017-1000117 class). LoadDeployConfig also rejects such hosts.
func RunRemote(cfg *DeployConfig, command string) error {
	args := append(sshBaseArgs(cfg), "--", cfg.Host, command)

	if cfg.DryRun {
		_, _ = fmt.Fprintf(printOut(), "   \033[90m[dry-run] ssh %s %q\033[0m\n", cfg.Host, command)
		return nil
	}

	cmd := execCommand("ssh", args...)
	cmd.Stdout = printOut()
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunRemoteInteractive executes a command on the remote server with full I/O passthrough.
func RunRemoteInteractive(cfg *DeployConfig, command string) error {
	args := append(sshBaseArgs(cfg), "--", cfg.Host, command)

	if cfg.DryRun {
		_, _ = fmt.Fprintf(printOut(), "   \033[90m[dry-run] ssh %s %q\033[0m\n", cfg.Host, command)
		return nil
	}

	cmd := execCommand("ssh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// RunRemoteCapture executes a command on the remote server and captures stdout.
func RunRemoteCapture(cfg *DeployConfig, command string) (string, error) {
	args := append(sshBaseArgs(cfg), "--", cfg.Host, command)

	if cfg.DryRun {
		_, _ = fmt.Fprintf(printOut(), "   \033[90m[dry-run] ssh %s %q\033[0m\n", cfg.Host, command)
		return "", nil
	}

	cmd := execCommand("ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// CopyFile transfers a local file to a remote path via scp. The "--" separator
// guards against a Host beginning with "-" being parsed as an scp option.
func CopyFile(cfg *DeployConfig, localPath, remotePath string) error {
	dest := cfg.Host + ":" + remotePath
	args := []string{
		"-P", strconv.Itoa(cfg.Port),
		"-o", "StrictHostKeyChecking=" + strictHostKeyPolicy(cfg),
		"--", localPath, dest,
	}

	if cfg.DryRun {
		_, _ = fmt.Fprintf(printOut(), "   \033[90m[dry-run] scp %s %s\033[0m\n", localPath, dest)
		return nil
	}

	cmd := execCommand("scp", args...)
	cmd.Stdout = printOut()
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CopyDir transfers a local directory recursively to a remote path via scp.
func CopyDir(cfg *DeployConfig, localDir, remoteDir string) error {
	dest := cfg.Host + ":" + remoteDir
	args := []string{
		"-r",
		"-P", strconv.Itoa(cfg.Port),
		"-o", "StrictHostKeyChecking=" + strictHostKeyPolicy(cfg),
		"--", localDir, dest,
	}

	if cfg.DryRun {
		_, _ = fmt.Fprintf(printOut(), "   \033[90m[dry-run] scp -r %s %s\033[0m\n", localDir, dest)
		return nil
	}

	cmd := execCommand("scp", args...)
	cmd.Stdout = printOut()
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunLocalPiped runs an already-constructed argv pipeline of two local
// commands (left | right), wiring left's stdout into right's stdin via an
// in-process pipe. It deliberately does NOT go through a shell — unlike a
// `sh -c "<string>"` call, no argument can be interpreted as shell syntax, so
// attacker-influenced values (e.g. a Host or app name from a cloned project's
// config.yaml) cannot inject commands.
func RunLocalPiped(cfg *DeployConfig, left, right []string) error {
	if cfg.DryRun {
		_, _ = fmt.Fprintf(printOut(), "   \033[90m[dry-run] %s | %s\033[0m\n",
			strings.Join(left, " "), strings.Join(right, " "))
		return nil
	}
	if len(left) == 0 || len(right) == 0 {
		return fmt.Errorf("RunLocalPiped: both command halves are required")
	}

	leftCmd := execCommand(left[0], left[1:]...)
	rightCmd := execCommand(right[0], right[1:]...)

	pipe, err := leftCmd.StdoutPipe()
	if err != nil {
		return err
	}
	rightCmd.Stdin = pipe
	rightCmd.Stdout = printOut()
	rightCmd.Stderr = os.Stderr
	leftCmd.Stderr = os.Stderr

	if err := rightCmd.Start(); err != nil {
		return err
	}
	if err := leftCmd.Run(); err != nil {
		_ = rightCmd.Wait()
		return err
	}
	return rightCmd.Wait()
}

// RunLocal runs a local command with stdout/stderr passthrough.
func RunLocal(cfg *DeployConfig, name string, args ...string) error {
	if cfg.DryRun {
		_, _ = fmt.Fprintf(printOut(), "   \033[90m[dry-run] %s %s\033[0m\n", name, strings.Join(args, " "))
		return nil
	}

	cmd := execCommand(name, args...)
	cmd.Stdout = printOut()
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
