package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"openbot/internal/domain"
)

// extractToolCallsFromContent attempts to parse tool calls from LLM content text.
// Some models (especially smaller ones) return tool calls as JSON in the content
// instead of using the structured tool_calls field. Handles several patterns:
//   - Pure JSON: `{"name":"shell","arguments":{...}}`
//   - Code-fenced: ```json\n{...}\n```
//   - Prefixed text: `assistant\n{"name":"shell",...}` (common with llama models)
//   - Suffixed text: `{"name":"shell",...}\n\nI'll execute that.`
//   - Mixed text:   `Sure.\n{"name":"shell",...}\nLet me do that.`
func extractToolCallsFromContent(content string) []domain.ToolCall {
	content = strings.TrimSpace(content)

	// Strip markdown code fences if present.
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) >= 3 && strings.HasPrefix(lines[len(lines)-1], "```") {
			content = strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}

	// Fast path: try full content as JSON.
	if calls := tryParseToolJSON(content); len(calls) > 0 {
		return calls
	}

	// Fallback: find JSON object/array boundaries within surrounding text.
	// This handles prefix text, suffix text, or both (e.g. "assistant\n{...}\nI'll do that.").
	if start, end := findJSONBounds(content); start >= 0 && end > start {
		candidate := content[start:end]
		if calls := tryParseToolJSON(candidate); len(calls) > 0 {
			return calls
		}
	}

	return nil
}

// findJSONBounds locates the first top-level JSON object ({}) or array ([]) in s.
// Returns the start index and end+1 index, or (-1, -1) if not found.
func findJSONBounds(s string) (int, int) {
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return -1, -1
	}

	openChar := s[start]
	var closeChar byte
	if openChar == '{' {
		closeChar = '}'
	} else {
		closeChar = ']'
	}

	depth := 0
	inStr := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inStr {
			if ch == '\\' {
				i++ // skip escaped character
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case openChar:
			depth++
		case closeChar:
			depth--
			if depth == 0 {
				return start, i + 1
			}
		}
	}
	return -1, -1
}

// tryParseToolJSON attempts to parse raw as a single tool call object or an array.
func tryParseToolJSON(raw string) []domain.ToolCall {
	// Try single object.
	var single struct {
		Name       string         `json:"name"`
		Parameters map[string]any `json:"parameters"`
		Arguments  map[string]any `json:"arguments"`
	}
	text := raw
	if err := json.Unmarshal([]byte(text), &single); err != nil {
		text = sanitizeJSONEscapes(text)
		_ = json.Unmarshal([]byte(text), &single)
	}
	if single.Name != "" {
		args := coalesce(single.Parameters, single.Arguments)
		return []domain.ToolCall{{
			ID:        fmt.Sprintf("extracted_%d", time.Now().UnixNano()),
			Name:      normalizeToolName(single.Name),
			Arguments: args,
		}}
	}

	// Try array.
	var multi []struct {
		Name       string         `json:"name"`
		Parameters map[string]any `json:"parameters"`
		Arguments  map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(text), &multi); err != nil {
		_ = json.Unmarshal([]byte(sanitizeJSONEscapes(raw)), &multi)
	}
	var calls []domain.ToolCall
	for i, tc := range multi {
		if tc.Name == "" {
			continue
		}
		calls = append(calls, domain.ToolCall{
			ID:        fmt.Sprintf("extracted_%d_%d", time.Now().UnixNano(), i),
			Name:      normalizeToolName(tc.Name),
			Arguments: coalesce(tc.Parameters, tc.Arguments),
		})
	}
	if len(calls) > 0 {
		return calls
	}

	return nil
}

// normalizeToolName maps common model-generated tool name variations to the
// actual registered names. Smaller models often drop underscores or use hyphens.
func normalizeToolName(name string) string {
	aliases := map[string]string{
		"webfetch":    "web_fetch",
		"web-fetch":   "web_fetch",
		"websearch":   "web_search",
		"web-search":  "web_search",
		"readfile":    "read_file",
		"read-file":   "read_file",
		"writefile":   "write_file",
		"write-file":  "write_file",
		"listdir":     "list_dir",
		"list-dir":    "list_dir",
		"systeminfo":  "system_info",
		"system-info": "system_info",
	}
	if mapped, ok := aliases[strings.ToLower(name)]; ok {
		return mapped
	}
	return name
}

// stripRolePrefix removes role-name prefixes that some LLMs (especially smaller
// Ollama models) leak into their content. Examples: "assistant\nHello" → "Hello",
// "Assistant: Hello" → "Hello".
func stripRolePrefix(content string) string {
	// Common leaked prefixes from chat-template-aware models.
	prefixes := []string{
		"assistant\n",
		"Assistant\n",
		"assistant:\n",
		"Assistant:\n",
		"assistant: ",
		"Assistant: ",
	}
	trimmed := content
	for _, p := range prefixes {
		if strings.HasPrefix(trimmed, p) {
			trimmed = strings.TrimSpace(trimmed[len(p):])
			break
		}
	}
	return trimmed
}

// coalesce returns the first non-nil map, or an empty map if both are nil.
func coalesce(a, b map[string]any) map[string]any {
	if a != nil {
		return a
	}
	if b != nil {
		return b
	}
	return make(map[string]any)
}

// sanitizeJSONEscapes fixes invalid JSON escape sequences produced by some LLMs.
// Valid JSON escapes: \", \\, \/, \b, \f, \n, \r, \t, \uXXXX.
// Invalid ones (e.g. \% or \Y) are corrected by dropping the backslash.
func sanitizeJSONEscapes(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	inString := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '"' && (i == 0 || s[i-1] != '\\') {
			inString = !inString
			buf.WriteByte(ch)
			continue
		}
		if inString && ch == '\\' && i+1 < len(s) {
			next := s[i+1]
			switch next {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
				buf.WriteByte(ch) // valid escape — keep the backslash
			default:
				continue // invalid escape — drop the backslash
			}
		} else {
			buf.WriteByte(ch)
		}
	}
	return buf.String()
}
