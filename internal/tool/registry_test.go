package tool

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"openbot/internal/domain"
)

// stubTool is a minimal tool for testing the registry.
type stubTool struct {
	name   string
	result string
	err    error
}

func (s *stubTool) Name() string                                              { return s.name }
func (s *stubTool) Description() string                                       { return "stub: " + s.name }
func (s *stubTool) Parameters() map[string]any                                { return map[string]any{"type": "object", "properties": map[string]any{}} }
func (s *stubTool) Execute(ctx context.Context, args map[string]any) (string, error) { return s.result, s.err }

var _ domain.Tool = (*stubTool)(nil)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry(testLogger())
	tool := &stubTool{name: "test_tool", result: "ok"}
	reg.Register(tool)

	got := reg.Get("test_tool")
	if got == nil {
		t.Fatal("expected to find registered tool")
	}
	if got.Name() != "test_tool" {
		t.Fatalf("expected 'test_tool', got %q", got.Name())
	}
}

func TestRegistry_GetUnknown(t *testing.T) {
	reg := NewRegistry(testLogger())
	got := reg.Get("nonexistent")
	if got != nil {
		t.Fatal("expected nil for unknown tool")
	}
}

func TestRegistry_Execute(t *testing.T) {
	reg := NewRegistry(testLogger())
	reg.Register(&stubTool{name: "echo", result: "hello"})

	result, err := reg.Execute(context.Background(), "echo", nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result != "hello" {
		t.Fatalf("expected 'hello', got %q", result)
	}
}

func TestRegistry_ExecuteUnknown(t *testing.T) {
	reg := NewRegistry(testLogger())
	_, err := reg.Execute(context.Background(), "missing", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestRegistry_Names(t *testing.T) {
	reg := NewRegistry(testLogger())
	reg.Register(&stubTool{name: "alpha"})
	reg.Register(&stubTool{name: "beta"})

	names := reg.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["alpha"] || !nameSet["beta"] {
		t.Fatalf("missing expected names: %v", names)
	}
}

func TestRegistry_GetDefinitions(t *testing.T) {
	reg := NewRegistry(testLogger())
	reg.Register(&stubTool{name: "tool1"})
	reg.Register(&stubTool{name: "tool2"})

	defs := reg.GetDefinitions()
	if len(defs) != 2 {
		t.Fatalf("expected 2 definitions, got %d", len(defs))
	}
}

func TestRegistry_OverwriteRegistration(t *testing.T) {
	reg := NewRegistry(testLogger())
	reg.Register(&stubTool{name: "dup", result: "v1"})
	reg.Register(&stubTool{name: "dup", result: "v2"})

	result, _ := reg.Execute(context.Background(), "dup", nil)
	if result != "v2" {
		t.Fatalf("expected overwritten tool result 'v2', got %q", result)
	}
}

// --- ToolParameters ---

func TestToolParameters_WithRequired(t *testing.T) {
	params := ToolParameters(
		map[string]Param{
			"name": {Type: "string", Description: "The name"},
			"age":  {Type: "number", Description: "The age in years"},
		},
		[]string{"name"},
	)

	if params["type"] != "object" {
		t.Fatal("expected type=object")
	}
	props := params["properties"].(map[string]any)
	if len(props) != 2 {
		t.Fatalf("expected 2 properties, got %d", len(props))
	}

	nameParam := props["name"].(map[string]any)
	if nameParam["description"] != "The name" {
		t.Fatalf("expected 'The name', got %q", nameParam["description"])
	}

	required := params["required"].([]string)
	if len(required) != 1 || required[0] != "name" {
		t.Fatalf("unexpected required: %v", required)
	}
}

func TestToolParameters_NoRequired(t *testing.T) {
	params := ToolParameters(
		map[string]Param{
			"query": {Type: "string", Description: "Search query"},
		},
		nil,
	)
	if _, ok := params["required"]; ok {
		t.Fatal("should not have 'required' key when nil")
	}
}

// --- ArgsString ---

func TestArgsString_StringValue(t *testing.T) {
	args := map[string]any{"key": "value"}
	if got := ArgsString(args, "key"); got != "value" {
		t.Fatalf("expected 'value', got %q", got)
	}
}

func TestArgsString_MissingKey(t *testing.T) {
	args := map[string]any{"other": "value"}
	if got := ArgsString(args, "key"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestArgsString_NilArgs(t *testing.T) {
	if got := ArgsString(nil, "key"); got != "" {
		t.Fatalf("expected empty for nil args, got %q", got)
	}
}

func TestArgsString_NonStringValue(t *testing.T) {
	args := map[string]any{"num": 42.0}
	got := ArgsString(args, "num")
	if got == "" {
		t.Fatal("expected non-empty for numeric value")
	}
}
