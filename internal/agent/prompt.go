package agent

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"time"
	"openbot/internal/domain"
)

type PromptBuilder struct {
	workspace string
	memory    domain.MemoryStore
	logger    *slog.Logger
}

func NewPromptBuilder(workspace string, memory domain.MemoryStore, logger *slog.Logger) *PromptBuilder {
	return &PromptBuilder{
		workspace: workspace,
		memory:    memory,
		logger:    logger,
	}
}

func (p *PromptBuilder) BuildSystemPrompt(ctx context.Context, convID string, channel, chatID string) (string, error) {
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
		identity += "\n\n## Long-term Memory (recent)\n"
		for _, m := range memories {
			identity += fmt.Sprintf("- [%s] %s\n", m.Category, m.Content)
		}
	}

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
