package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"openbot/internal/domain"
)

const (
	defaultMaxContextTokens = 4096
	// Keep at least this many recent messages when compacting.
	minRecentMessages = 4
	// Words-to-tokens ratio approximation (1 token ~ 0.75 words for English).
	wordsPerToken = 0.75
)

// Compactor manages context window compaction to prevent token overflow.
// It uses a sliding-window strategy: when the context exceeds the token budget,
// the oldest messages are summarized via an LLM call and replaced with a
// single "[summary]" message.
type Compactor struct {
	provider       domain.Provider
	maxTokens      int
	logger         *slog.Logger
}

// CompactorConfig configures the context compactor.
type CompactorConfig struct {
	Provider  domain.Provider
	MaxTokens int
	Logger    *slog.Logger
}

// NewCompactor creates a new Compactor.
func NewCompactor(cfg CompactorConfig) *Compactor {
	max := cfg.MaxTokens
	if max <= 0 {
		max = defaultMaxContextTokens
	}
	lgr := cfg.Logger
	if lgr == nil {
		lgr = slog.Default()
	}
	return &Compactor{
		provider:  cfg.Provider,
		maxTokens: max,
		logger:    lgr,
	}
}

// EstimateTokens returns a rough token count for a message slice.
// Uses a simple word-based heuristic: split on whitespace, multiply by 1.33.
func EstimateTokens(messages []domain.Message) int {
	total := 0
	for _, m := range messages {
		total += estimateStringTokens(m.Content)
		// Tool call arguments count too
		for _, tc := range m.ToolCalls {
			for _, v := range tc.Arguments {
				total += estimateStringTokens(fmt.Sprintf("%v", v))
			}
		}
	}
	return total
}

// estimateStringTokens counts approximate tokens in a string.
func estimateStringTokens(s string) int {
	if s == "" {
		return 0
	}
	words := len(strings.Fields(s))
	// Rough approximation: 1 token ≈ 0.75 words
	tokens := int(float64(words) / wordsPerToken)
	if tokens == 0 && words > 0 {
		tokens = 1
	}
	return tokens
}

// Compact checks if the messages exceed the token budget and, if so,
// compacts them by summarizing the oldest messages.
// It returns the (possibly compacted) message slice.
// The first message (system prompt) is always preserved.
func (c *Compactor) Compact(ctx context.Context, messages []domain.Message) []domain.Message {
	if len(messages) <= minRecentMessages+1 {
		// Too few messages to compact (system + a few exchanges).
		return messages
	}

	totalTokens := EstimateTokens(messages)
	if totalTokens <= c.maxTokens {
		return messages
	}

	c.logger.Info("context compaction triggered",
		"total_tokens", totalTokens,
		"max_tokens", c.maxTokens,
		"message_count", len(messages),
	)

	// Strategy: preserve system prompt (index 0) and the last N messages.
	// Summarize everything in between.
	systemMsg := messages[0]
	recentStart := len(messages) - minRecentMessages
	if recentStart < 1 {
		recentStart = 1
	}

	oldMessages := messages[1:recentStart]
	recentMessages := messages[recentStart:]

	// Check if old messages are worth summarizing
	if len(oldMessages) == 0 {
		return messages
	}

	summary, err := c.summarize(ctx, oldMessages)
	if err != nil {
		c.logger.Warn("compaction summarization failed, keeping full context", "err", err)
		return messages
	}

	// Build compacted message list.
	compacted := make([]domain.Message, 0, 2+len(recentMessages))
	compacted = append(compacted, systemMsg)
	compacted = append(compacted, domain.Message{
		Role:    "system",
		Content: fmt.Sprintf("[Conversation Summary]\n%s", summary),
	})
	compacted = append(compacted, recentMessages...)

	newTokens := EstimateTokens(compacted)
	c.logger.Info("context compacted",
		"old_tokens", totalTokens,
		"new_tokens", newTokens,
		"old_messages", len(messages),
		"new_messages", len(compacted),
		"summarized_messages", len(oldMessages),
	)

	return compacted
}

// summarize asks the LLM to produce a concise summary of the given messages.
func (c *Compactor) summarize(ctx context.Context, messages []domain.Message) (string, error) {
	// Build a text representation of the messages to summarize.
	var sb strings.Builder
	for _, m := range messages {
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		if m.Content != "" {
			sb.WriteString(m.Content)
		}
		if len(m.ToolCalls) > 0 {
			sb.WriteString(fmt.Sprintf(" [called tools: %s]", toolNames(m.ToolCalls)))
		}
		sb.WriteString("\n")
	}

	summaryReq := domain.ChatRequest{
		Messages: []domain.Message{
			{
				Role: "system",
				Content: `You are a conversation summarizer. Summarize the following conversation 
concisely, preserving key facts, decisions, tool results, and context. 
Keep the summary under 200 words. Focus on information that would be 
needed to continue the conversation naturally.`,
			},
			{
				Role:    "user",
				Content: "Summarize this conversation:\n\n" + sb.String(),
			},
		},
		MaxTokens:   512,
		Temperature: 0.3,
	}

	resp, err := c.provider.Chat(ctx, summaryReq)
	if err != nil {
		return "", fmt.Errorf("summarization LLM call: %w", err)
	}

	return resp.Content, nil
}

// toolNames returns a comma-separated list of tool names from tool calls.
func toolNames(calls []domain.ToolCall) string {
	names := make([]string, len(calls))
	for i, tc := range calls {
		names[i] = tc.Name
	}
	return strings.Join(names, ", ")
}
