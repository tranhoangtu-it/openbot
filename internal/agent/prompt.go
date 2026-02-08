package agent

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"openbot/internal/domain"
)

const promptCacheTTL = 60 * time.Second

type cachedPrompt struct {
	content   string
	expiresAt time.Time
}

type PromptBuilder struct {
	workspace         string
	memory            domain.MemoryStore
	logger            *slog.Logger
	thinkingLevel     string // "concise" | "normal" | "detailed"
	systemPromptExtra string // custom text appended to system prompt

	// Prompt cache keyed by channel:chatID
	promptCache sync.Map

	// Tool definitions cache (built once)
	toolDefsOnce sync.Once
	toolDefs     []domain.ToolDefinition
}

// PromptConfig holds configuration for the prompt builder.
type PromptConfig struct {
	Workspace         string
	ThinkingLevel     string
	SystemPromptExtra string
}

func NewPromptBuilder(workspace string, memory domain.MemoryStore, logger *slog.Logger) *PromptBuilder {
	pb := &PromptBuilder{
		workspace:     workspace,
		memory:        memory,
		logger:        logger,
		thinkingLevel: "normal",
	}
	// Periodic cleanup of expired prompt cache entries to prevent unbounded growth.
	go pb.cleanupLoop()
	return pb
}

// NewPromptBuilderWithConfig creates a PromptBuilder with additional configuration.
func NewPromptBuilderWithConfig(cfg PromptConfig, memory domain.MemoryStore, logger *slog.Logger) *PromptBuilder {
	level := cfg.ThinkingLevel
	if level == "" {
		level = "normal"
	}
	pb := &PromptBuilder{
		workspace:         cfg.Workspace,
		memory:            memory,
		logger:            logger,
		thinkingLevel:     level,
		systemPromptExtra: cfg.SystemPromptExtra,
	}
	go pb.cleanupLoop()
	return pb
}

// cleanupLoop evicts expired entries from the prompt cache every 2 minutes.
func (p *PromptBuilder) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		p.promptCache.Range(func(key, value any) bool {
			if cp, ok := value.(*cachedPrompt); ok && now.After(cp.expiresAt) {
				p.promptCache.Delete(key)
			}
			return true
		})
	}
}

// CachedToolDefs returns cached tool definitions, building them once.
func (p *PromptBuilder) CachedToolDefs(tools []domain.ToolDefinition) []domain.ToolDefinition {
	p.toolDefsOnce.Do(func() {
		p.toolDefs = tools
	})
	return p.toolDefs
}

func (p *PromptBuilder) BuildSystemPrompt(ctx context.Context, convID string, channel, chatID string) (string, error) {
	cacheKey := channel + ":" + chatID
	if cached, ok := p.promptCache.Load(cacheKey); ok {
		if cp, ok := cached.(*cachedPrompt); ok && time.Now().Before(cp.expiresAt) {
			return cp.content, nil
		}
	}

	now := time.Now().Format("2006-01-02 15:04 (Monday)")
	workspacePath, err := filepath.Abs(p.workspace)
	if err != nil {
		workspacePath = p.workspace
	}
	goVersion := runtime.Version()
	osArch := fmt.Sprintf("%s %s", runtime.GOOS, runtime.GOARCH)

	// Build OS-specific hints so the model knows what shell commands are available.
	osHint := ""
	switch runtime.GOOS {
	case "darwin":
		osHint = `
## macOS Commands (use with shell tool)
- Open apps: open -a "AppName" (e.g. open -a Calculator, open -a Safari, open -a "Google Chrome")
- Open URLs: open "https://example.com"
- Open files: open /path/to/file
- Spotlight search: mdfind "keyword"
- System info: sw_vers, system_profiler SPHardwareDataType
- Process management: ps aux, top -l 1, kill PID`
	case "linux":
		osHint = `
## Linux Commands (use with shell tool)
- Open apps: xdg-open, or run the command directly (e.g. firefox, nautilus)
- System info: uname -a, lsb_release -a, free -h, df -h
- Process management: ps aux, top -bn1, kill PID`
	case "windows":
		osHint = `
## Windows Commands (use with shell tool)
- Open apps: start "AppName" (e.g. start calc, start notepad)
- Open URLs: start "https://example.com"
- System info: systeminfo, wmic os get caption`
	}

	identity := fmt.Sprintf(`# OpenBot

You are OpenBot, a helpful AI assistant with access to tools. You can:
- Read, write, and edit files in the workspace
- Execute shell commands to perform ANY system operation
- Open applications and URLs on the user's computer
- Search the web and fetch web page content
- Control the computer (mouse, keyboard, screenshots) when enabled
- Manage scheduled tasks (cron)

## Current Time
%s

## Runtime
%s (%s), Go %s

## Workspace
%s
%s
## Session
Channel: %s | Chat ID: %s

## RULES
1. When the user asks you to DO something (open an app, run a command, fetch a URL, check system info, etc.), ALWAYS use the appropriate tool. Never say "I can't" without trying first.
2. Use the shell tool for system operations: opening apps, running commands, checking processes, etc.
3. Use web_search to search the internet, web_fetch to read a specific URL.
4. Do NOT output raw JSON in your response. Use the tool calling mechanism.
5. After tool execution, present results clearly. Do not mention tool names to the user.
6. Respond in the same language the user writes in.
7. Be helpful, accurate, and concise.`,
		now, osArch, runtime.GOOS, goVersion, workspacePath, osHint, channel, chatID)

	// Add thinking level directive
	thinkingDirective := ""
	switch p.thinkingLevel {
	case "concise":
		thinkingDirective = "\n\n## Thinking Level: Concise\nKeep responses short and direct. Minimize explanations. One-line answers when possible."
	case "detailed":
		thinkingDirective = "\n\n## Thinking Level: Detailed\nProvide thorough, step-by-step explanations. Show reasoning. Include context and alternatives."
	default: // "normal"
		thinkingDirective = "\n\n## Thinking Level: Normal\nBalance clarity with brevity. Explain when helpful, be concise when straightforward."
	}
	identity += thinkingDirective

	// Add custom system prompt extension
	if p.systemPromptExtra != "" {
		identity += "\n\n## Custom Instructions\n" + p.systemPromptExtra
	}

	memories, err := p.memory.GetRecentMemories(ctx, 5)
	if err != nil {
		p.logger.Warn("failed to load recent memories for prompt", "err", err)
	} else if len(memories) > 0 {
		var memBuf strings.Builder
		memBuf.WriteString("\n\n## Long-term Memory (recent)\n")
		for _, m := range memories {
			memBuf.WriteString("- [")
			memBuf.WriteString(m.Category)
			memBuf.WriteString("] ")
			memBuf.WriteString(m.Content)
			memBuf.WriteByte('\n')
		}
		identity += memBuf.String()
	}

	// Cache the result
	p.promptCache.Store(cacheKey, &cachedPrompt{
		content:   identity,
		expiresAt: time.Now().Add(promptCacheTTL),
	})

	return identity, nil
}

// BuildMessages constructs [system + history + user message] for an LLM call.
func (p *PromptBuilder) BuildMessages(ctx context.Context, convID string, history []domain.Message, currentMessage string, channel, chatID string) ([]domain.Message, error) {
	systemPrompt, err := p.BuildSystemPrompt(ctx, convID, channel, chatID)
	if err != nil {
		return nil, err
	}

	messages := []domain.Message{
		{Role: "system", Content: systemPrompt},
	}

	for _, m := range history {
		msg := domain.Message{
			Role:    m.Role,
			Content: m.Content,
		}
		if m.ToolCallID != "" {
			msg.ToolCallID = m.ToolCallID
			msg.ToolName = m.ToolName
		}
		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = m.ToolCalls
		}
		messages = append(messages, msg)
	}

	messages = append(messages, domain.Message{Role: "user", Content: currentMessage})
	return messages, nil
}

func (p *PromptBuilder) AddAssistantMessage(messages []domain.Message, content string, toolCalls []domain.ToolCall) []domain.Message {
	msg := domain.Message{Role: "assistant", Content: content}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	return append(messages, msg)
}

func (p *PromptBuilder) AddToolResult(messages []domain.Message, toolCallID, toolName, result string) []domain.Message {
	return append(messages, domain.Message{
		Role:       "tool",
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Content:    result,
	})
}
