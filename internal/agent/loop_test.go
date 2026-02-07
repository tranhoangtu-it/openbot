package agent

import (
	"testing"

	"openbot/internal/domain"
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

// --- extractToolCallsFromContent with prefix text ---

func TestExtractToolCalls_PrefixedAssistant(t *testing.T) {
	// Ollama llama models sometimes prepend "assistant\n" before tool call JSON
	input := "assistant\n{\"name\": \"web_search\", \"parameters\": {\"query\": \"hello world\"}}"
	calls := extractToolCallsFromContent(input)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call from prefixed content, got %d", len(calls))
	}
	if calls[0].Name != "web_search" {
		t.Fatalf("expected 'web_search', got %q", calls[0].Name)
	}
	if calls[0].Arguments["query"] != "hello world" {
		t.Fatalf("expected 'hello world', got %v", calls[0].Arguments["query"])
	}
}

func TestExtractToolCalls_PrefixedNaturalLanguage(t *testing.T) {
	// Model says something before producing the JSON tool call
	input := "I'll search for that.\n{\"name\": \"web_search\", \"parameters\": {\"query\": \"OpenBot features\"}}"
	calls := extractToolCallsFromContent(input)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call from NL-prefixed content, got %d", len(calls))
	}
	if calls[0].Name != "web_search" {
		t.Fatalf("expected 'web_search', got %q", calls[0].Name)
	}
}

func TestExtractToolCalls_PrefixedArray(t *testing.T) {
	input := "Here are the tools:\n[{\"name\": \"shell\", \"arguments\": {\"command\": \"ls\"}}]"
	calls := extractToolCallsFromContent(input)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call from prefixed array, got %d", len(calls))
	}
	if calls[0].Name != "shell" {
		t.Fatalf("expected 'shell', got %q", calls[0].Name)
	}
}

func TestExtractToolCalls_PrefixedWithUnicode(t *testing.T) {
	// Exact pattern from the Ollama bug: "assistant\n" + JSON with unicode escapes
	input := "assistant\n{\"name\": \"web_search\", \"parameters\": {\"query\": \"t\\u00f4i ki\\u1ec3m tra\"}}"
	calls := extractToolCallsFromContent(input)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "web_search" {
		t.Fatalf("expected 'web_search', got %q", calls[0].Name)
	}
}

func TestExtractToolCalls_NoBracesInPlainText(t *testing.T) {
	input := "I cannot help with that request."
	calls := extractToolCallsFromContent(input)
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls for plain text without braces, got %d", len(calls))
	}
}

func TestExtractToolCalls_JSONWithTrailingText(t *testing.T) {
	// Exact pattern from the Ollama bug: JSON tool call followed by natural language
	input := "{\"name\": \"webfetch\", \"parameters\": {\"url\": \"https://vnexpress.net/\"}}\n\nSau đó, bạn có thể sử dụng kết quả của hàm webfetch."
	calls := extractToolCallsFromContent(input)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call from JSON with trailing text, got %d", len(calls))
	}
	if calls[0].Name != "web_fetch" {
		t.Fatalf("expected 'web_fetch' (normalized), got %q", calls[0].Name)
	}
	if calls[0].Arguments["url"] != "https://vnexpress.net/" {
		t.Fatalf("expected url arg, got %v", calls[0].Arguments)
	}
}

func TestExtractToolCalls_PrefixAndSuffix(t *testing.T) {
	input := "assistant\n{\"name\": \"shell\", \"arguments\": {\"command\": \"ls\"}}\nI'll run that for you."
	calls := extractToolCallsFromContent(input)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call from prefix+suffix text, got %d", len(calls))
	}
	if calls[0].Name != "shell" {
		t.Fatalf("expected 'shell', got %q", calls[0].Name)
	}
}

// --- normalizeToolName ---

func TestNormalizeToolName_WebFetch(t *testing.T) {
	if got := normalizeToolName("webfetch"); got != "web_fetch" {
		t.Fatalf("expected 'web_fetch', got %q", got)
	}
}

func TestNormalizeToolName_WebSearch(t *testing.T) {
	if got := normalizeToolName("websearch"); got != "web_search" {
		t.Fatalf("expected 'web_search', got %q", got)
	}
}

func TestNormalizeToolName_AlreadyCorrect(t *testing.T) {
	if got := normalizeToolName("web_fetch"); got != "web_fetch" {
		t.Fatalf("expected 'web_fetch' unchanged, got %q", got)
	}
}

func TestNormalizeToolName_CaseInsensitive(t *testing.T) {
	if got := normalizeToolName("WebFetch"); got != "web_fetch" {
		t.Fatalf("expected 'web_fetch', got %q", got)
	}
}

func TestNormalizeToolName_Unknown(t *testing.T) {
	if got := normalizeToolName("custom_tool"); got != "custom_tool" {
		t.Fatalf("expected 'custom_tool' unchanged, got %q", got)
	}
}

// --- findJSONBounds ---

func TestFindJSONBounds_SimpleObject(t *testing.T) {
	s := `{"name": "test"}`
	start, end := findJSONBounds(s)
	if start != 0 || end != len(s) {
		t.Fatalf("expected 0:%d, got %d:%d", len(s), start, end)
	}
}

func TestFindJSONBounds_WithPrefix(t *testing.T) {
	s := `hello {"name": "test"} world`
	start, end := findJSONBounds(s)
	if start != 6 || end != 22 {
		t.Fatalf("expected 6:22, got %d:%d", start, end)
	}
	extracted := s[start:end]
	if extracted != `{"name": "test"}` {
		t.Fatalf("expected JSON object, got %q", extracted)
	}
}

func TestFindJSONBounds_NoJSON(t *testing.T) {
	s := "just plain text"
	start, end := findJSONBounds(s)
	if start != -1 || end != -1 {
		t.Fatalf("expected -1:-1, got %d:%d", start, end)
	}
}

func TestFindJSONBounds_NestedBraces(t *testing.T) {
	s := `{"outer": {"inner": "val"}} trailing`
	start, end := findJSONBounds(s)
	expected := `{"outer": {"inner": "val"}}`
	if start != 0 || end != len(expected) {
		t.Fatalf("expected 0:%d, got %d:%d", len(expected), start, end)
	}
}

func TestFindJSONBounds_StringWithBraces(t *testing.T) {
	// Braces inside a JSON string should not confuse the parser
	s := `{"text": "hello {world}"} after`
	start, end := findJSONBounds(s)
	expected := `{"text": "hello {world}"}`
	if s[start:end] != expected {
		t.Fatalf("expected %q, got %q", expected, s[start:end])
	}
}

// --- stripRolePrefix ---

func TestStripRolePrefix_AssistantNewline(t *testing.T) {
	input := "assistant\nHello, how can I help?"
	result := stripRolePrefix(input)
	if result != "Hello, how can I help?" {
		t.Fatalf("expected stripped content, got %q", result)
	}
}

func TestStripRolePrefix_AssistantColonSpace(t *testing.T) {
	input := "Assistant: Here is the info."
	result := stripRolePrefix(input)
	if result != "Here is the info." {
		t.Fatalf("expected stripped content, got %q", result)
	}
}

func TestStripRolePrefix_NoPrefix(t *testing.T) {
	input := "Hello, I am your AI assistant."
	result := stripRolePrefix(input)
	if result != input {
		t.Fatalf("content without prefix should be unchanged, got %q", result)
	}
}

func TestStripRolePrefix_Empty(t *testing.T) {
	result := stripRolePrefix("")
	if result != "" {
		t.Fatalf("empty string should stay empty, got %q", result)
	}
}

func TestStripRolePrefix_OnlyPrefix(t *testing.T) {
	result := stripRolePrefix("assistant\n")
	if result != "" {
		t.Fatalf("content with only prefix should be empty, got %q", result)
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

// --- extractSecurityCommand ---

func TestExtractSecurityCommand_Shell(t *testing.T) {
	tc := domain.ToolCall{Name: "shell", Arguments: map[string]any{"command": "rm -rf /"}}
	result := extractSecurityCommand(tc)
	if result != "rm -rf /" {
		t.Fatalf("expected 'rm -rf /', got %q", result)
	}
}

func TestExtractSecurityCommand_WriteFile(t *testing.T) {
	tc := domain.ToolCall{Name: "write_file", Arguments: map[string]any{"path": "/etc/passwd", "content": "hacked"}}
	result := extractSecurityCommand(tc)
	if result != "write /etc/passwd" {
		t.Fatalf("expected 'write /etc/passwd', got %q", result)
	}
}

func TestExtractSecurityCommand_WebFetch(t *testing.T) {
	tc := domain.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": "http://evil.com"}}
	result := extractSecurityCommand(tc)
	if result != "fetch http://evil.com" {
		t.Fatalf("expected 'fetch http://evil.com', got %q", result)
	}
}

func TestExtractSecurityCommand_ReadFile(t *testing.T) {
	tc := domain.ToolCall{Name: "read_file", Arguments: map[string]any{"path": "/etc/passwd"}}
	result := extractSecurityCommand(tc)
	if result != "" {
		t.Fatalf("read_file should not produce security command, got %q", result)
	}
}

func TestExtractSecurityCommand_NoArgs(t *testing.T) {
	tc := domain.ToolCall{Name: "shell", Arguments: map[string]any{}}
	result := extractSecurityCommand(tc)
	if result != "" {
		t.Fatalf("expected empty for shell without command arg, got %q", result)
	}
}

// --- coalesce ---

func TestCoalesce_FirstNonNil(t *testing.T) {
	a := map[string]any{"key": "a"}
	b := map[string]any{"key": "b"}
	result := coalesce(a, b)
	if result["key"] != "a" {
		t.Fatalf("expected 'a', got %v", result["key"])
	}
}

func TestCoalesce_SecondWhenFirstNil(t *testing.T) {
	b := map[string]any{"key": "b"}
	result := coalesce(nil, b)
	if result["key"] != "b" {
		t.Fatalf("expected 'b', got %v", result["key"])
	}
}

func TestCoalesce_BothNil(t *testing.T) {
	result := coalesce(nil, nil)
	if result == nil {
		t.Fatal("expected empty map, got nil")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %v", result)
	}
}
