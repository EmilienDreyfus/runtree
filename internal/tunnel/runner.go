package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/EmilienDreyfus/runtree/internal/cloudapi"
)

const (
	overrideEnv = "RUNTREE_TUNNEL_BINARY"
	binaryName  = "cloudflared"
)

type Runner struct {
	HomeDir string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

func (r Runner) Run(ctx context.Context, launch cloudapi.TunnelLaunchConfig) error {
	binaryPath, err := ResolveBinaryPath(r.HomeDir)
	if err != nil {
		return err
	}

	configPath, cleanup, err := r.prepareFiles(launch)
	if err != nil {
		return err
	}
	defer cleanup()

	args := resolveArgs(launch.Args, configPath)
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Stdin = r.stdin()
	cmd.Stdout = r.stdout()
	cmd.Stderr = r.stderr()
	cmd.Env = append(os.Environ(), formatEnv(launch.Env)...)

	if err := cmd.Run(); err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("run tunnel command: %w", err)
	}
	return nil
}

func ResolveBinaryPath(home string) (string, error) {
	if override := strings.TrimSpace(os.Getenv(overrideEnv)); override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("stat %s: %w", override, err)
		}
		return override, nil
	}

	if home == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home: %w", err)
		}
		home = filepath.Join(homeDir, ".runtree")
	}

	bundled := filepath.Join(home, "bin", binaryName)
	if _, err := os.Stat(bundled); err == nil {
		return bundled, nil
	}
	if path, err := exec.LookPath(binaryName); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("tunnel binary not found: install runtree again or set %s", overrideEnv)
}

func (r Runner) prepareFiles(launch cloudapi.TunnelLaunchConfig) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "runtree-tunnel-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create tunnel temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }

	credentialsPath := filepath.Join(tempDir, "credentials.json")
	if err := os.WriteFile(credentialsPath, []byte(launch.CredentialsJSON), 0o600); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("write credentials: %w", err)
	}

	configPath := filepath.Join(tempDir, "config.yml")
	config := strings.ReplaceAll(launch.ConfigTemplate, "__CREDENTIALS_FILE__", credentialsPath)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("write config: %w", err)
	}
	return configPath, cleanup, nil
}

func resolveArgs(args []string, configPath string) []string {
	if len(args) == 0 {
		return []string{"tunnel", "--config", configPath, "run"}
	}

	resolved := make([]string, len(args))
	for i, arg := range args {
		resolved[i] = strings.ReplaceAll(arg, "__CONFIG_FILE__", configPath)
	}
	return resolved
}

func formatEnv(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}

	env := make([]string, 0, len(values))
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}

func (r Runner) stdin() io.Reader {
	if r.Stdin != nil {
		return r.Stdin
	}
	return os.Stdin
}

func (r Runner) stdout() io.Writer {
	if r.Stdout != nil {
		return r.Stdout
	}
	return os.Stdout
}

func (r Runner) stderr() io.Writer {
	if r.Stderr != nil {
		return r.Stderr
	}
	return os.Stderr
}
