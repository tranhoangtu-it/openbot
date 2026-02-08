package agent

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"openbot/internal/domain"
)

// ChatCommand represents a parsed chat command.
type ChatCommand struct {
	Name string   // command name without "/"
	Args []string // arguments after the command
	Raw  string   // original full text
}

// CommandResult holds the response for a handled command.
type CommandResult struct {
	Response string // text response to send back
	Handled  bool   // true if the command was handled (don't send to LLM)
}

// startTime records when the process started for /uptime.
var startTime = time.Now()

// ParseCommand checks if a message starts with "/" and parses it into a ChatCommand.
// Returns nil if the message is not a command.
func ParseCommand(text string) *ChatCommand {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return nil
	}

	// Split into parts
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return nil
	}

	name := strings.TrimPrefix(parts[0], "/")
	name = strings.ToLower(name)

	var args []string
	if len(parts) > 1 {
		args = parts[1:]
	}

	return &ChatCommand{
		Name: name,
		Args: args,
		Raw:  text,
	}
}

// HandleCommand processes a chat command and returns a result.
// If the command is not recognized, returns Handled=false so the message
// can be forwarded to the LLM as a normal message.
func (l *Loop) HandleCommand(cmd *ChatCommand, msg domain.InboundMessage) CommandResult {
	switch cmd.Name {
	case "help":
		return CommandResult{Response: helpText(), Handled: true}

	case "new", "clear":
		l.sessions.ClearSession(msg.ChatID)
		return CommandResult{Response: "Conversation cleared. Starting fresh.", Handled: true}

	case "status":
		return CommandResult{Response: l.statusText(), Handled: true}

	case "uptime":
		uptime := time.Since(startTime).Round(time.Second)
		return CommandResult{Response: fmt.Sprintf("Uptime: %s", uptime), Handled: true}

	case "version":
		return CommandResult{Response: fmt.Sprintf("OpenBot v%s (%s/%s, Go %s)", version, runtime.GOOS, runtime.GOARCH, runtime.Version()), Handled: true}

	case "model":
		if len(cmd.Args) == 0 {
			return CommandResult{Response: fmt.Sprintf("Current provider: %s", l.provider.Name()), Handled: true}
		}
		return CommandResult{Response: "Provider switching: set provider in next message. Use /providers to list.", Handled: true}

	case "providers":
		return CommandResult{Response: l.providersText(), Handled: true}

	case "tools":
		return CommandResult{Response: l.toolsText(), Handled: true}

	case "compact":
		// Placeholder for context compaction (Sprint 1)
		l.sessions.ClearSession(msg.ChatID)
		return CommandResult{Response: "Context compacted (conversation reset). Full compaction coming in v0.3.0.", Handled: true}

	case "usage":
		// Placeholder for token usage (will be enhanced with real data)
		return CommandResult{Response: "Token usage tracking will be available in v0.3.0. Use /status for current info.", Handled: true}

	default:
		// Unknown command — pass through to LLM as normal message
		return CommandResult{Handled: false}
	}
}

// version is set by the build system. Default fallback.
var version = "0.2.0"

// SetVersion sets the version string used by commands.
func SetVersion(v string) {
	version = v
}

func helpText() string {
	return `**OpenBot Commands**

/help — Show this help message
/new — Start a new conversation (clear history)
/clear — Same as /new
/status — Show bot status and info
/uptime — Show bot uptime
/version — Show version info
/model — Show current LLM provider
/providers — List available providers
/tools — List available tools
/compact — Compact conversation context
/usage — Show token usage (coming soon)`
}

func (l *Loop) statusText() string {
	uptime := time.Since(startTime).Round(time.Second)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**OpenBot v%s**\n\n", version))
	sb.WriteString(fmt.Sprintf("Provider: %s\n", l.provider.Name()))
	sb.WriteString(fmt.Sprintf("Tools: %d registered\n", len(l.tools.Names())))
	sb.WriteString(fmt.Sprintf("Uptime: %s\n", uptime))
	sb.WriteString(fmt.Sprintf("Runtime: %s/%s, Go %s\n", runtime.GOOS, runtime.GOARCH, runtime.Version()))
	return sb.String()
}

func (l *Loop) providersText() string {
	var sb strings.Builder
	sb.WriteString("**Available Providers**\n\n")
	sb.WriteString(fmt.Sprintf("• %s (active)\n", l.provider.Name()))
	return sb.String()
}

func (l *Loop) toolsText() string {
	names := l.tools.Names()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Available Tools** (%d)\n\n", len(names)))
	for _, name := range names {
		t := l.tools.Get(name)
		if t != nil {
			sb.WriteString(fmt.Sprintf("• **%s** — %s\n", name, t.Description()))
		}
	}
	return sb.String()
}
