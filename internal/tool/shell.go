package tool

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultShellTimeout    = 30
	defaultMaxOutputBytes  = 65536
)

type ShellTool struct {
	workingDir          string
	timeoutSeconds      int
	maxOutputBytes      int
	restrictToWorkspace bool
}

type ShellConfig struct {
	WorkingDir          string
	TimeoutSeconds      int
	MaxOutputBytes      int
	RestrictToWorkspace bool
}

func NewShellTool(cfg ShellConfig) *ShellTool {
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = defaultShellTimeout
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = defaultMaxOutputBytes
	}
	return &ShellTool{
		workingDir:          cfg.WorkingDir,
		timeoutSeconds:       cfg.TimeoutSeconds,
		maxOutputBytes:       cfg.MaxOutputBytes,
		restrictToWorkspace:  cfg.RestrictToWorkspace,
	}
}

func (s *ShellTool) Name() string { return "shell" }

func (s *ShellTool) Description() string {
	return "Execute a shell command. Use for running terminal commands, scripts, or any CLI tool. Returns stdout and stderr."
}

func (s *ShellTool) Parameters() map[string]any {
	return ToolParameters(
		map[string]Param{
			"command": {Type: "string", Description: "The shell command to execute (e.g. 'ls -la', 'git status')"},
		},
		[]string{"command"},
	)
}

func (s *ShellTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	command := ArgsString(args, "command")
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("missing argument: command")
	}

	// Optional: restrict to workspace
	dir := s.workingDir
	if dir == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}

	timeout := time.Duration(s.timeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	// Always use sh -c for reliable handling of pipes, redirects, quotes, etc.
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = absDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("command timed out or cancelled")
		}
		return string(output), fmt.Errorf("exit: %w", err)
	}

	result := string(output)
	if s.maxOutputBytes > 0 && len(result) > s.maxOutputBytes {
		result = result[:s.maxOutputBytes] + "\n... (output truncated)"
	}
	return result, nil
}
