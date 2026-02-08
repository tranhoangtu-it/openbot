package tool

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestNewSysInfoTool(t *testing.T) {
	tool := NewSysInfoTool()
	if tool == nil {
		t.Fatal("NewSysInfoTool returned nil")
	}
	if tool.Name() != "system_info" {
		t.Errorf("Name: got %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
	if tool.Parameters() == nil {
		t.Error("Parameters should not be nil")
	}
}

func TestSysInfoTool_Execute_ReturnsInfo(t *testing.T) {
	tool := NewSysInfoTool()
	ctx := context.Background()
	out, err := tool.Execute(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out == "" {
		t.Fatal("output should not be empty")
	}
	// Expected sections from Execute()
	if !strings.Contains(out, "System Information") {
		t.Errorf("output should contain 'System Information', got: %s", out[:min(200, len(out))])
	}
	if !strings.Contains(out, "CPU") {
		t.Errorf("output should contain 'CPU'")
	}
	if !strings.Contains(out, runtime.GOOS) {
		t.Errorf("output should contain GOOS %q", runtime.GOOS)
	}
}
