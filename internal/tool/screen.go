package tool

import (
	"context"
	"fmt"
	"strings"
)

// ScreenTool provides screen control capabilities (mouse, keyboard, screenshot).
// This is a stub implementation. When robotgo is available (with CGO),
// replace with actual robotgo calls.
type ScreenTool struct {
	enabled bool
}

func NewScreenTool(enabled bool) *ScreenTool {
	return &ScreenTool{enabled: enabled}
}

func (t *ScreenTool) Name() string { return "screen_control" }
func (t *ScreenTool) Description() string {
	return "Control the computer screen, mouse, and keyboard. Actions: 'screenshot' (take a screenshot), 'mouse_move' (move mouse to x,y), 'mouse_click' (click at x,y), 'type_text' (type text), 'key_press' (press a key combination like ctrl+c)."
}
func (t *ScreenTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{"type": "string", "description": "Action: screenshot, mouse_move, mouse_click, type_text, key_press"},
			"x":      map[string]any{"type": "number", "description": "X coordinate (for mouse actions)"},
			"y":      map[string]any{"type": "number", "description": "Y coordinate (for mouse actions)"},
			"text":   map[string]any{"type": "string", "description": "Text to type (for type_text)"},
			"key":    map[string]any{"type": "string", "description": "Key combination (for key_press, e.g. 'ctrl+c', 'enter', 'tab')"},
		},
		"required": []string{"action"},
	}
}

func (t *ScreenTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if !t.enabled {
		return "Screen control is disabled. Enable it in config: tools.screen.enabled = true", nil
	}

	action := ArgsString(args, "action")

	switch action {
	case "screenshot":
		return t.takeScreenshot()
	case "mouse_move":
		x := getInt(args, "x")
		y := getInt(args, "y")
		return t.mouseMove(x, y)
	case "mouse_click":
		x := getInt(args, "x")
		y := getInt(args, "y")
		return t.mouseClick(x, y)
	case "type_text":
		text := ArgsString(args, "text")
		return t.typeText(text)
	case "key_press":
		key := ArgsString(args, "key")
		return t.keyPress(key)
	default:
		return fmt.Sprintf("Unknown screen action: %s. Available: screenshot, mouse_move, mouse_click, type_text, key_press", action), nil
	}
}

func (t *ScreenTool) takeScreenshot() (string, error) {
	// Stub: use os/exec to call screencapture on macOS or import on Linux
	return "Screenshot capability requires robotgo (CGO). Use 'shell' tool with 'screencapture /tmp/screenshot.png' on macOS or 'import -window root /tmp/screenshot.png' on Linux.", nil
}

func (t *ScreenTool) mouseMove(x, y int) (string, error) {
	return fmt.Sprintf("Mouse move to (%d, %d) — requires robotgo (CGO build). Stub only.", x, y), nil
}

func (t *ScreenTool) mouseClick(x, y int) (string, error) {
	return fmt.Sprintf("Mouse click at (%d, %d) — requires robotgo (CGO build). Stub only.", x, y), nil
}

func (t *ScreenTool) typeText(text string) (string, error) {
	if text == "" {
		return "Error: no text provided.", nil
	}
	return fmt.Sprintf("Type text %q — requires robotgo (CGO build). Stub only.", truncate(text, 50)), nil
}

func (t *ScreenTool) keyPress(key string) (string, error) {
	if key == "" {
		return "Error: no key provided.", nil
	}
	return fmt.Sprintf("Key press %s — requires robotgo (CGO build). Stub only.", key), nil
}

func getInt(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
