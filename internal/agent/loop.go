package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"openbot/internal/domain"
	"openbot/internal/security"
	"openbot/internal/tool"
)

const (
	defaultMaxIterations   = 20
	defaultHistoryLimit    = 50
	defaultLLMMaxTokens    = 4096
	defaultTemperature     = 0.7
	maxConcurrentMessages  = 3
	defaultRateBurst       = 5
	defaultRatePerMinute   = 30.0
)

// Loop is the core agent: receive message → call LLM → execute tools → respond.
type Loop struct {
	provider      domain.Provider
	sessions      *SessionManager
	prompt        *PromptBuilder
	tools         *tool.Registry
	security      *security.Engine
	bus           domain.MessageBus
	logger        *slog.Logger
	maxIterations int
	rateLimiter   *RateLimiter
}

type LoopConfig struct {
	Provider      domain.Provider
	Sessions      *SessionManager
	Prompt        *PromptBuilder
	Tools         *tool.Registry
	Security      *security.Engine
	Bus           domain.MessageBus
	Logger        *slog.Logger
	MaxIterations int
}

func NewLoop(cfg LoopConfig) *Loop {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = defaultMaxIterations
	}
	return &Loop{
		provider:      cfg.Provider,
		sessions:      cfg.Sessions,
		prompt:        cfg.Prompt,
		tools:         cfg.Tools,
		security:      cfg.Security,
		bus:           cfg.Bus,
		logger:        cfg.Logger,
		maxIterations: cfg.MaxIterations,
		rateLimiter:   NewRateLimiter(defaultRateBurst, defaultRatePerMinute),
	}
}

// Run consumes messages from the bus, processing up to maxConcurrentMessages in parallel.
func (l *Loop) Run(ctx context.Context) {
	l.logger.Info("agent loop started")

	sem := make(chan struct{}, maxConcurrentMessages)

	inbound := l.bus.Subscribe()
	for {
		select {
		case <-ctx.Done():
			l.logger.Info("agent loop stopping")
			return
		case msg, ok := <-inbound:
			if !ok {
				l.logger.Info("inbound channel closed, agent loop stopping")
				return
			}
			sem <- struct{}{}
			go func(m domain.InboundMessage) {
				defer func() { <-sem }()
				l.processMessage(ctx, m)
			}(msg)
		}
	}
}

// ProcessDirect processes a message synchronously and returns the response.
// Used by CLI and other direct callers.
func (l *Loop) ProcessDirect(ctx context.Context, content string, channel string, chatID string) (string, error) {
	msg := domain.InboundMessage{
		Channel:   channel,
		ChatID:    chatID,
		SenderID:  "user",
		Content:   content,
		Timestamp: time.Now(),
	}

	return l.handleMessage(ctx, msg)
}

func (l *Loop) processMessage(ctx context.Context, msg domain.InboundMessage) {
	l.logger.Info("processing message",
		"channel", msg.Channel,
		"sender", msg.SenderID,
		"content_len", len(msg.Content),
	)

	response, err := l.handleMessage(ctx, msg)
	if err != nil {
		l.logger.Error("error processing message", "error", err)
		response = fmt.Sprintf("Sorry, I encountered an error: %s", err.Error())
	}

	l.bus.SendOutbound(domain.OutboundMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Content: response,
		Format:  "markdown",
	})
}

func (l *Loop) handleMessage(ctx context.Context, msg domain.InboundMessage) (string, error) {
	sessionKey := fmt.Sprintf("%s:%s", msg.Channel, msg.ChatID)

	convID, err := l.sessions.GetOrCreateConversation(
		ctx, sessionKey,
		l.provider.Name(),
		"",
	)
	if err != nil {
		return "", fmt.Errorf("session error: %w", err)
	}

	history, err := l.sessions.GetHistory(ctx, convID, defaultHistoryLimit)
	if err != nil {
		l.logger.Warn("failed to load history", "error", err)
		history = nil
	}

	messages, err := l.prompt.BuildMessages(ctx, convID, history, msg.Content, msg.Channel, msg.ChatID)
	if err != nil {
		return "", fmt.Errorf("build messages: %w", err)
	}

	var toolDefs []domain.ToolDefinition
	if l.tools != nil {
		toolDefs = l.tools.GetDefinitions()
	}

	var finalContent string
	for iteration := 0; iteration < l.maxIterations; iteration++ {
		l.logger.Debug("agent iteration",
			"iteration", iteration+1,
			"messages", len(messages),
		)

		if err := l.rateLimiter.Wait(ctx); err != nil {
			return "", fmt.Errorf("rate limit: %w", err)
		}

		resp, err := l.provider.Chat(ctx, domain.ChatRequest{
			Messages:    messages,
			Tools:       toolDefs,
			MaxTokens:   defaultLLMMaxTokens,
			Temperature: defaultTemperature,
		})
		if err != nil {
			return "", fmt.Errorf("LLM error: %w", err)
		}

		// Some smaller models return tool calls as JSON text instead of structured tool_calls
		if !resp.HasToolCalls() && resp.Content != "" {
			if extracted := extractToolCallsFromContent(resp.Content); len(extracted) > 0 {
				resp.ToolCalls = extracted
				resp.Content = "" // Clear the raw JSON content
				l.logger.Info("extracted tool calls from content text", "count", len(extracted))
			}
		}

		if !resp.HasToolCalls() {
			finalContent = resp.Content
			break
		}

		assistantMsg := domain.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		messages = append(messages, assistantMsg)

		for _, tc := range resp.ToolCalls {
			result, err := l.executeTool(ctx, tc)
			if err != nil {
				result = fmt.Sprintf("Error executing tool %s: %s", tc.Name, err.Error())
			}

			messages = append(messages, domain.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
			})
		}
	}

	if finalContent == "" {
		finalContent = "I've completed processing but have no additional response."
	}

	if err := l.sessions.SaveMessage(ctx, convID, domain.Message{Role: "user", Content: msg.Content}); err != nil {
		l.logger.Warn("failed to save user message", "error", err, "convID", convID)
	}
	if err := l.sessions.SaveMessage(ctx, convID, domain.Message{Role: "assistant", Content: finalContent}); err != nil {
		l.logger.Warn("failed to save assistant message", "error", err, "convID", convID)
	}

	if len(history) == 0 {
		l.sessions.UpdateTitle(ctx, convID, msg.Content)
	}

	return finalContent, nil
}

// extractToolCallsFromContent attempts to parse tool calls from LLM content text.
// Some models (especially smaller ones) return tool calls as JSON in the content
// instead of using the structured tool_calls field.
func extractToolCallsFromContent(content string) []domain.ToolCall {
	content = strings.TrimSpace(content)

	// Strip markdown code fences if present
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) >= 3 {
			// Remove first and last line (code fences)
			inner := strings.Join(lines[1:len(lines)-1], "\n")
			if strings.HasPrefix(lines[len(lines)-1], "```") {
				content = strings.TrimSpace(inner)
			}
		}
	}

	// Try to parse as a single tool call object.
	// If initial parse fails, try sanitizing invalid JSON escape sequences
	// (common in smaller models, e.g. \% instead of %).
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
		args := single.Parameters
		if args == nil {
			args = single.Arguments
		}
		if args == nil {
			args = make(map[string]any)
		}
		return []domain.ToolCall{{
			ID:        fmt.Sprintf("extracted_%d", time.Now().UnixNano()),
			Name:      single.Name,
			Arguments: args,
		}}
	}

	// Try to parse as an array of tool calls
	var multi []struct {
		Name       string         `json:"name"`
		Parameters map[string]any `json:"parameters"`
		Arguments  map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(raw), &multi); err != nil {
		_ = json.Unmarshal([]byte(sanitizeJSONEscapes(content)), &multi)
	}
	if len(multi) > 0 {
		var calls []domain.ToolCall
		for i, tc := range multi {
			if tc.Name == "" {
				continue
			}
			args := tc.Parameters
			if args == nil {
				args = tc.Arguments
			}
			if args == nil {
				args = make(map[string]any)
			}
			calls = append(calls, domain.ToolCall{
				ID:        fmt.Sprintf("extracted_%d_%d", time.Now().UnixNano(), i),
				Name:      tc.Name,
				Arguments: args,
			})
		}
		if len(calls) > 0 {
			return calls
		}
	}

	return nil
}

// sanitizeJSONEscapes fixes invalid JSON escape sequences produced by some LLMs.
// Valid escapes: \", \\, \/, \b, \f, \n, \r, \t, \uXXXX.
// Invalid ones like \% or \Y are replaced with just the character after the backslash.
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
				buf.WriteByte(ch) // valid escape, keep the backslash
			default:
				// Invalid escape — drop the backslash
				continue
			}
		} else {
			buf.WriteByte(ch)
		}
	}
	return buf.String()
}

func (l *Loop) executeTool(ctx context.Context, tc domain.ToolCall) (string, error) {
	l.logger.Info("executing tool",
		"tool", tc.Name,
		"args", fmt.Sprintf("%v", tc.Arguments),
	)

	command := ""
	if tc.Name == "shell" || tc.Name == "exec" {
		if cmd, ok := tc.Arguments["command"]; ok {
			command = fmt.Sprintf("%v", cmd)
		}
	}

	if l.security != nil && command != "" {
		action, err := l.security.Check(ctx, tc.Name, command)
		if err != nil {
			return "", fmt.Errorf("security check error: %w", err)
		}

		switch action {
		case domain.ActionBlock:
			return fmt.Sprintf("Command blocked by security policy: %s", command), nil
		case domain.ActionConfirm:
			confirmed, err := l.security.RequestConfirmation(ctx, tc.Name, command)
			if err != nil {
				return "", fmt.Errorf("confirmation error: %w", err)
			}
			if !confirmed {
				return fmt.Sprintf("Command denied by user: %s", command), nil
			}
		}
	}

	if l.tools == nil {
		return "", fmt.Errorf("tool registry not initialized")
	}
	argsJSON, err := json.Marshal(tc.Arguments)
	if err != nil {
		l.logger.Warn("failed to marshal tool args for logging", "tool", tc.Name, "error", err)
	} else {
		l.logger.Debug("tool execution",
			"tool", tc.Name,
			"args", string(argsJSON),
		)
	}

	result, err := l.tools.Execute(ctx, tc.Name, tc.Arguments)
	if err != nil {
		return "", err
	}

	l.logger.Debug("tool result",
		"tool", tc.Name,
		"result_len", len(result),
	)

	return result, nil
}
