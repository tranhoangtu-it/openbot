package tool

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// DockerSandboxConfig configures the Docker sandbox.
type DockerSandboxConfig struct {
	Enabled   bool
	Image     string // Docker image to use (default: "alpine:latest")
	Timeout   time.Duration
	MaxMemory string // e.g., "256m"
	MaxCPU    string // e.g., "0.5"
	Logger    *slog.Logger
}

// DockerSandbox executes commands inside isolated Docker containers.
type DockerSandbox struct {
	enabled   bool
	image     string
	timeout   time.Duration
	maxMemory string
	maxCPU    string
	logger    *slog.Logger
}

// NewDockerSandbox creates a new Docker sandbox executor.
func NewDockerSandbox(cfg DockerSandboxConfig) *DockerSandbox {
	if cfg.Image == "" {
		cfg.Image = "alpine:latest"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxMemory == "" {
		cfg.MaxMemory = "256m"
	}
	if cfg.MaxCPU == "" {
		cfg.MaxCPU = "0.5"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &DockerSandbox{
		enabled:   cfg.Enabled,
		image:     cfg.Image,
		timeout:   cfg.Timeout,
		maxMemory: cfg.MaxMemory,
		maxCPU:    cfg.MaxCPU,
		logger:    cfg.Logger,
	}
}

// IsEnabled returns whether the sandbox is enabled.
func (ds *DockerSandbox) IsEnabled() bool {
	return ds.enabled
}

// Execute runs a command inside a Docker container with resource limits.
func (ds *DockerSandbox) Execute(ctx context.Context, command string) (string, error) {
	if !ds.enabled {
		return "", fmt.Errorf("docker sandbox is disabled")
	}

	// Check Docker availability.
	if err := ds.checkDocker(ctx); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, ds.timeout)
	defer cancel()

	args := []string{
		"run", "--rm",
		"--network", "none", // no network access
		"--memory", ds.maxMemory,
		"--cpus", ds.maxCPU,
		"--pids-limit", "100",
		"--read-only",
		"--tmpfs", "/tmp:rw,size=64m",
		ds.image,
		"sh", "-c", command,
	}

	ds.logger.Info("sandbox executing", "command", command, "image", ds.image)

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n[stderr] " + stderr.String()
	}

	// Trim output size.
	if len(output) > 100000 {
		output = output[:100000] + "\n... (output truncated)"
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return output, fmt.Errorf("command timed out after %s", ds.timeout)
		}
		return output, fmt.Errorf("command failed: %w", err)
	}

	return strings.TrimSpace(output), nil
}

// checkDocker verifies that Docker is available.
func (ds *DockerSandbox) checkDocker(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker not available: %w", err)
	}
	return nil
}
