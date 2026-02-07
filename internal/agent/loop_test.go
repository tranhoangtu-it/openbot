package agent

import (
	"testing"
)

// --- extractToolCallsFromContent ---

func TestExtractToolCalls_SingleObject(t *testing.T) {
	input := `{"name": "shell", "arguments": {"command": "ls -la"}}`
	calls := extractToolCallsFromContent(input)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "shell" {
		t.Fatalf("expected 'shell', got %q", calls[0].Name)
	}
	if calls[0].Arguments["command"] != "ls -la" {
		t.Fatalf("expected 'ls -la', got %v", calls[0].Arguments["command"])
	}
}

func TestExtractToolCalls_ParametersField(t *testing.T) {
	input := `{"name": "read_file", "parameters": {"path": "/tmp/test.txt"}}`
	calls := extractToolCallsFromContent(input)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Arguments["path"] != "/tmp/test.txt" {
		t.Fatalf("expected path, got %v", calls[0].Arguments)
	}
}

func TestExtractToolCalls_Array(t *testing.T) {
	input := `[{"name": "shell", "arguments": {"command": "ls"}}, {"name": "shell", "arguments": {"command": "pwd"}}]`
	calls := extractToolCallsFromContent(input)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
}

func TestExtractToolCalls_CodeFenceWrapped(t *testing.T) {
	input := "```json\n{\"name\": \"shell\", \"arguments\": {\"command\": \"echo hi\"}}\n```"
	calls := extractToolCallsFromContent(input)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call from code fence, got %d", len(calls))
	}
	if calls[0].Name != "shell" {
		t.Fatalf("expected 'shell', got %q", calls[0].Name)
	}
}

func TestExtractToolCalls_PlainText(t *testing.T) {
	input := "Sure, let me help you with that!"
	calls := extractToolCallsFromContent(input)
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls for plain text, got %d", len(calls))
	}
}

func TestExtractToolCalls_EmptyName(t *testing.T) {
	input := `{"name": "", "arguments": {}}`
	calls := extractToolCallsFromContent(input)
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls for empty name, got %d", len(calls))
	}
}

func TestExtractToolCalls_EmptyString(t *testing.T) {
	calls := extractToolCallsFromContent("")
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls for empty input, got %d", len(calls))
	}
}

func TestExtractToolCalls_NilArguments(t *testing.T) {
	input := `{"name": "system_info"}`
	calls := extractToolCallsFromContent(input)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Arguments == nil {
		t.Fatal("arguments should be initialized to empty map")
	}
}

// --- sanitizeJSONEscapes ---

func TestSanitizeJSONEscapes_ValidJSON(t *testing.T) {
	input := `{"key": "value with \"quotes\" and \\backslash"}`
	result := sanitizeJSONEscapes(input)
	if result != input {
		t.Fatalf("valid JSON should not change:\n  got:  %q\n  want: %q", result, input)
	}
}

func TestSanitizeJSONEscapes_InvalidEscape(t *testing.T) {
	// \% is invalid JSON escape — backslash should be dropped
	input := `{"key": "100\% done"}`
	result := sanitizeJSONEscapes(input)
	expected := `{"key": "100% done"}`
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestSanitizeJSONEscapes_MultipleInvalid(t *testing.T) {
	input := `{"msg": "Hello \World \! \?"}`
	result := sanitizeJSONEscapes(input)
	expected := `{"msg": "Hello World ! ?"}`
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestSanitizeJSONEscapes_PreservesValidEscapes(t *testing.T) {
	input := `{"text": "line1\nline2\ttab"}`
	result := sanitizeJSONEscapes(input)
	if result != input {
		t.Fatalf("valid escapes should be preserved: got %q", result)
	}
}

func TestSanitizeJSONEscapes_EmptyString(t *testing.T) {
	result := sanitizeJSONEscapes("")
	if result != "" {
		t.Fatalf("expected empty, got %q", result)
	}
}

func TestSanitizeJSONEscapes_NoStrings(t *testing.T) {
	input := `{}`
	result := sanitizeJSONEscapes(input)
	if result != input {
		t.Fatalf("expected unchanged, got %q", result)
	}
}

// --- extractToolCallsFromContent with invalid escapes ---

func TestExtractToolCalls_WithInvalidEscapes(t *testing.T) {
	// Simulates LLM returning JSON with \% inside
	input := `{"name": "shell", "arguments": {"command": "echo 100\%"}}`
	calls := extractToolCallsFromContent(input)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call after sanitization, got %d", len(calls))
	}
	if calls[0].Name != "shell" {
		t.Fatalf("expected 'shell', got %q", calls[0].Name)
	}
}
