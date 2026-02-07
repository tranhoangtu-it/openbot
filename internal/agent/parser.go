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
// instead of using the structured tool_calls field.
func extractToolCallsFromContent(content string) []domain.ToolCall {
	content = strings.TrimSpace(content)

	// Strip markdown code fences if present.
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) >= 3 && strings.HasPrefix(lines[len(lines)-1], "```") {
			content = strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}

	// Try to parse as a single tool call object.
	// If the initial parse fails, sanitize invalid JSON escape sequences
	// that smaller models sometimes produce (e.g. \% instead of %).
	var single struct {
		Name       string         `json:"name"`
		Parameters map[string]any `json:"parameters"`
		Arguments  map[string]any `json:"arguments"`
	}
	raw := content
	if err := json.Unmarshal([]byte(raw), &single); err != nil {
		raw = sanitizeJSONEscapes(raw)
		_ = json.Unmarshal([]byte(raw), &single)
	}
	if single.Name != "" {
		args := coalesce(single.Parameters, single.Arguments)
		return []domain.ToolCall{{
			ID:        fmt.Sprintf("extracted_%d", time.Now().UnixNano()),
			Name:      single.Name,
			Arguments: args,
		}}
	}

	// Try to parse as an array of tool calls.
	var multi []struct {
		Name       string         `json:"name"`
		Parameters map[string]any `json:"parameters"`
		Arguments  map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(raw), &multi); err != nil {
		_ = json.Unmarshal([]byte(sanitizeJSONEscapes(content)), &multi)
	}
	var calls []domain.ToolCall
	for i, tc := range multi {
		if tc.Name == "" {
			continue
		}
		calls = append(calls, domain.ToolCall{
			ID:        fmt.Sprintf("extracted_%d_%d", time.Now().UnixNano(), i),
			Name:      tc.Name,
			Arguments: coalesce(tc.Parameters, tc.Arguments),
		})
	}
	if len(calls) > 0 {
		return calls
	}

	return nil
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
