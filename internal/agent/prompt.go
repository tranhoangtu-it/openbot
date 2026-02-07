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
	workspace string
	memory    domain.MemoryStore
	logger    *slog.Logger

	// Prompt cache keyed by channel:chatID
	promptCache sync.Map

	// Tool definitions cache (built once)
	toolDefsOnce sync.Once
	toolDefs     []domain.ToolDefinition
}

func NewPromptBuilder(workspace string, memory domain.MemoryStore, logger *slog.Logger) *PromptBuilder {
	pb := &PromptBuilder{
		workspace: workspace,
		memory:    memory,
		logger:    logger,
	}
	// Periodic cleanup of expired prompt cache entries to prevent unbounded growth.
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

	identity := fmt.Sprintf(`# OpenBot

You are OpenBot, a helpful AI assistant with access to tools. You can:
- Read, write, and edit files
- Execute shell commands (with security checks)
- Search the web and fetch web pages
- Control the computer (mouse, keyboard, screen) when enabled
- Remember important facts and preferences in long-term memory

## Current Time
%s

## Runtime
%s, Go %s

## Workspace
Your workspace is at: %s

## Current Session
Channel: %s | Chat ID: %s

IMPORTANT: Reply directly with your text response for normal conversation. Only use tools when you need to perform an action (run a command, read/write files, etc.). Be helpful, accurate, and concise. When using tools, explain what you're doing briefly.`,
		now, osArch, goVersion, workspacePath, channel, chatID)

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
