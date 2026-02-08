package tool

import (
	"context"
	"strings"
	"testing"
)

func TestNewShellTool_Defaults(t *testing.T) {
	s := NewShellTool(ShellConfig{})
	if s == nil {
		t.Fatal("NewShellTool returned nil")
	}
	if s.Name() != "shell" {
		t.Errorf("Name: got %q", s.Name())
	}
	if s.Description() == "" {
		t.Error("Description should not be empty")
	}
	params := s.Parameters()
	if params == nil {
		t.Fatal("Parameters returned nil")
	}
}

func TestShellTool_Execute_EmptyCommand_Error(t *testing.T) {
	s := NewShellTool(ShellConfig{TimeoutSeconds: 5, MaxOutputBytes: 4096})
	ctx := context.Background()
	out, err := s.Execute(ctx, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	if out != "" {
		t.Errorf("expected empty output, got %q", out)
	}

	out, err = s.Execute(ctx, map[string]any{"command": "   "})
	if err == nil {
		t.Fatal("expected error for whitespace-only command")
	}
	if out != "" {
		t.Errorf("expected empty output, got %q", out)
	}
}

func TestShellTool_Execute_Echo_Success(t *testing.T) {
	s := NewShellTool(ShellConfig{TimeoutSeconds: 5, MaxOutputBytes: 4096})
	ctx := context.Background()
	out, err := s.Execute(ctx, map[string]any{"command": "echo hello"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("output should contain 'hello', got %q", out)
	}
}

func TestShellTool_Execute_ExitNonZero_ReturnsError(t *testing.T) {
	s := NewShellTool(ShellConfig{TimeoutSeconds: 5, MaxOutputBytes: 4096})
	ctx := context.Background()
	_, err := s.Execute(ctx, map[string]any{"command": "exit 1"})
	if err == nil {
		t.Fatal("expected error for exit 1")
	}
}
