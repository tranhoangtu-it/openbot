package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"openbot/internal/domain"
	"openbot/internal/security"
	"openbot/internal/tool"
)

const (
	defaultMaxIterations  = 20
	defaultHistoryLimit   = 50
	defaultLLMMaxTokens   = 4096
	defaultTemperature    = 0.7
	defaultConcurrency    = 3
	defaultMaxParallelTools = 5
	defaultRateBurst      = 5
	defaultRatePerMinute  = 30.0
)

// Loop is the core agent engine: receive message → call LLM → execute tools → respond.
type Loop struct {
	provider      domain.Provider
	sessions      *SessionManager
	prompt        *PromptBuilder
	tools         *tool.Registry
	security      *security.Engine
	bus           domain.MessageBus
	logger        *slog.Logger
	maxIterations int
	concurrency   int
	rateLimiter   *RateLimiter

	// providers is the provider factory for per-message provider switching
	providers ProviderResolver
}

// ProviderResolver resolves a provider by name. Used for per-message switching.
type ProviderResolver interface {
	Get(name string) (domain.Provider, error)
}

// LoopConfig holds all dependencies and tuning parameters for the agent loop.
type LoopConfig struct {
	Provider      domain.Provider
	Providers     ProviderResolver // optional: for per-message provider switching
	Sessions      *SessionManager
	Prompt        *PromptBuilder
	Tools         *tool.Registry
	Security      *security.Engine
	Bus           domain.MessageBus
	Logger        *slog.Logger
	MaxIterations int
	Concurrency   int // max parallel messages (default 3)
}

// NewLoop creates a new agent loop with the given configuration.
func NewLoop(cfg LoopConfig) *Loop {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = defaultMaxIterations
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = defaultConcurrency
	}
	return &Loop{
		provider:      cfg.Provider,
		providers:     cfg.Providers,
		sessions:      cfg.Sessions,
		prompt:        cfg.Prompt,
		tools:         cfg.Tools,
		security:      cfg.Security,
		bus:           cfg.Bus,
		logger:        cfg.Logger,
		maxIterations: cfg.MaxIterations,
		concurrency:   cfg.Concurrency,
		rateLimiter:   NewRateLimiter(defaultRateBurst, defaultRatePerMinute),
	}
}

// Run consumes inbound messages and processes them with bounded concurrency.
func (l *Loop) Run(ctx context.Context) {
	l.logger.Info("agent loop started", "concurrency", l.concurrency)

	sem := make(chan struct{}, l.concurrency)
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
// Used by CLI and other direct callers that need a blocking reply.
func (l *Loop) ProcessDirect(ctx context.Context, content, channel, chatID string) (string, error) {
	return l.handleMessage(ctx, domain.InboundMessage{
		Channel:   channel,
		ChatID:    chatID,
		SenderID:  "user",
		Content:   content,
		Timestamp: time.Now(),
	})
}

// processMessage handles a single inbound message and sends the response
// back through the message bus. It uses streaming when the provider supports it.
func (l *Loop) processMessage(ctx context.Context, msg domain.InboundMessage) {
	l.logger.Info("processing message",
		"channel", msg.Channel,
		"sender", msg.SenderID,
		"content_len", len(msg.Content),
	)

	// Send thinking event to signal the frontend.
	l.bus.SendOutbound(domain.OutboundMessage{
		Channel:     msg.Channel,
		ChatID:      msg.ChatID,
		StreamEvent: &domain.StreamEvent{Type: domain.StreamThinking},
	})

	response, err := l.handleMessage(ctx, msg)
	if err != nil {
		l.logger.Error("message processing failed", "error", err)
		response = fmt.Sprintf("Sorry, I encountered an error: %s", err.Error())
	}

	l.bus.SendOutbound(domain.OutboundMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Content: response,
		Format:  "markdown",
		StreamEvent: &domain.StreamEvent{Type: domain.StreamDone, Content: response},
	})
}

// resolveProvider returns the provider for this message, supporting per-message switching.
func (l *Loop) resolveProvider(msg domain.InboundMessage) domain.Provider {
	if msg.Provider != "" && l.providers != nil {
		if p, err := l.providers.Get(msg.Provider); err == nil {
			return p
		}
		l.logger.Warn("requested provider not available, using default", "requested", msg.Provider)
	}
	return l.provider
}

// handleMessage is the main agent logic: build prompt → call LLM → loop on tool calls → return text.
func (l *Loop) handleMessage(ctx context.Context, msg domain.InboundMessage) (string, error) {
	sessionKey := fmt.Sprintf("%s:%s", msg.Channel, msg.ChatID)
	provider := l.resolveProvider(msg)

	convID, err := l.sessions.GetOrCreateConversation(ctx, sessionKey, provider.Name(), "")
	if err != nil {
		return "", fmt.Errorf("session error: %w", err)
	}

	history, err := l.sessions.GetHistory(ctx, convID, defaultHistoryLimit)
	if err != nil {
		l.logger.Warn("failed to load history, continuing without it", "error", err)
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

	// Helper: send a streaming event to the frontend.
	sendStreamEvent := func(evt domain.StreamEvent) {
		l.bus.SendOutbound(domain.OutboundMessage{
			Channel:     msg.Channel,
			ChatID:      msg.ChatID,
			StreamEvent: &evt,
		})
	}

	// Reusable semaphore for parallel tool execution (avoids re-allocation per iteration).
	toolSem := make(chan struct{}, defaultMaxParallelTools)

	// Main agent loop: call LLM, execute tools if requested, repeat.
	var finalContent string
	for iteration := 0; iteration < l.maxIterations; iteration++ {
		l.logger.Debug("agent iteration", "iteration", iteration+1, "messages", len(messages))

		if err := l.rateLimiter.Wait(ctx); err != nil {
			return "", fmt.Errorf("rate limit: %w", err)
		}

		startTime := time.Now()

		// Try streaming if the provider supports it.
		var resp *domain.ChatResponse
		if sp, ok := provider.(domain.StreamingProvider); ok {
			streamCh := make(chan domain.StreamEvent, 64)
			streamErrCh := make(chan error, 1)
			go func() {
				streamErrCh <- sp.ChatStream(ctx, domain.ChatRequest{
					Messages:    messages,
					Tools:       toolDefs,
					MaxTokens:   defaultLLMMaxTokens,
					Temperature: defaultTemperature,
				}, streamCh)
			}()

			var accumulated strings.Builder
			var streamedToolCalls []domain.ToolCall
			for evt := range streamCh {
				if evt.Type == domain.StreamToken {
					accumulated.WriteString(evt.Content)
				}
				// Collect complete tool calls from the final StreamDone event.
				if len(evt.ToolCalls) > 0 {
					streamedToolCalls = evt.ToolCalls
				}
				sendStreamEvent(evt)
			}
			// ChatStream closes streamCh (via defer) before returning, so
			// range exits first. Block on streamErrCh to guarantee the
			// goroutine's return value is visible before we inspect it.
			if err := <-streamErrCh; err != nil {
				return "", fmt.Errorf("LLM stream error: %w", err)
			}
			latency := time.Since(startTime).Milliseconds()
			resp = &domain.ChatResponse{
				Content:   accumulated.String(),
				ToolCalls: streamedToolCalls,
				LatencyMs: latency,
			}
		} else {
			var chatErr error
			resp, chatErr = provider.Chat(ctx, domain.ChatRequest{
				Messages:    messages,
				Tools:       toolDefs,
				MaxTokens:   defaultLLMMaxTokens,
				Temperature: defaultTemperature,
			})
			if chatErr != nil {
				return "", fmt.Errorf("LLM error: %w", chatErr)
			}
			resp.LatencyMs = time.Since(startTime).Milliseconds()
		}

		// Fallback: some smaller models embed tool calls as JSON in the content field.
		if !resp.HasToolCalls() && resp.Content != "" {
			if extracted := extractToolCallsFromContent(resp.Content); len(extracted) > 0 {
				resp.ToolCalls = extracted
				resp.Content = ""
				l.logger.Info("extracted tool calls from content text", "count", len(extracted))
			}
		}

		// No tool calls — we have our final answer.
		if !resp.HasToolCalls() {
			finalContent = resp.Content
			break
		}

		// Append assistant message with tool calls to the conversation.
		messages = append(messages, domain.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute tool calls in parallel with bounded concurrency.
		type toolResult struct {
			Index  int
			TC     domain.ToolCall
			Result string
		}

		results := make([]toolResult, len(resp.ToolCalls))
		var wg sync.WaitGroup

		for i, tc := range resp.ToolCalls {
			sendStreamEvent(domain.StreamEvent{Type: domain.StreamToolStart, Tool: tc.Name, ToolID: tc.ID})

			wg.Add(1)
			go func(idx int, tc domain.ToolCall) {
				defer wg.Done()
				toolSem <- struct{}{}
				defer func() { <-toolSem }()

				result, toolErr := l.executeTool(ctx, tc)
				if toolErr != nil {
					result = fmt.Sprintf("Error executing tool %s: %s", tc.Name, toolErr.Error())
				}
				results[idx] = toolResult{Index: idx, TC: tc, Result: result}

				sendStreamEvent(domain.StreamEvent{Type: domain.StreamToolEnd, Tool: tc.Name, ToolID: tc.ID})
			}(i, tc)
		}
		wg.Wait()

		// Append results in order
		for _, r := range results {
			messages = append(messages, domain.Message{
				Role:       "tool",
				Content:    r.Result,
				ToolCallID: r.TC.ID,
				ToolName:   r.TC.Name,
			})
		}
	}

	if finalContent == "" {
		finalContent = "I've completed processing but have no additional response."
	}

	// Persist conversation history.
	if err := l.sessions.SaveMessage(ctx, convID, domain.Message{Role: "user", Content: msg.Content}); err != nil {
		l.logger.Warn("failed to save user message", "error", err, "convID", convID)
	}
	if err := l.sessions.SaveMessage(ctx, convID, domain.Message{Role: "assistant", Content: finalContent}); err != nil {
		l.logger.Warn("failed to save assistant message", "error", err, "convID", convID)
	}

	// Auto-generate title from the first user message.
	if len(history) == 0 {
		l.sessions.UpdateTitle(ctx, convID, msg.Content)
	}

	return finalContent, nil
}

// executeTool runs a single tool call with security checks.
func (l *Loop) executeTool(ctx context.Context, tc domain.ToolCall) (string, error) {
	l.logger.Info("executing tool", "tool", tc.Name)

	// Determine the security-relevant command string to evaluate.
	command := extractSecurityCommand(tc)

	// Run through the security engine when there is something to evaluate.
	if l.security != nil && command != "" {
		action, err := l.security.Check(ctx, tc.Name, command)
		if err != nil {
			return "", fmt.Errorf("security check error: %w", err)
		}
		switch action {
		case domain.ActionBlock:
			return fmt.Sprintf("Action blocked by security policy: %s", command), nil
		case domain.ActionConfirm:
			confirmed, err := l.security.RequestConfirmation(ctx, tc.Name, command)
			if err != nil {
				return "", fmt.Errorf("confirmation error: %w", err)
			}
			if !confirmed {
				return fmt.Sprintf("Action denied by user: %s", command), nil
			}
		}
	}

	if l.tools == nil {
		return "", fmt.Errorf("tool registry not initialized")
	}

	if l.logger.Enabled(ctx, slog.LevelDebug) {
		if argsJSON, err := json.Marshal(tc.Arguments); err == nil {
			l.logger.Debug("tool arguments", "tool", tc.Name, "args", string(argsJSON))
		}
	}

	result, err := l.tools.Execute(ctx, tc.Name, tc.Arguments)
	if err != nil {
		return "", err
	}

	l.logger.Debug("tool completed", "tool", tc.Name, "result_len", len(result))
	return result, nil
}

// extractSecurityCommand builds a human-readable command string from a tool call
// so the security engine can evaluate it. Covers shell, file writes, and web fetches.
func extractSecurityCommand(tc domain.ToolCall) string {
	argStr := func(key string) string {
		if v, ok := tc.Arguments[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}

	switch tc.Name {
	case "shell", "exec":
		return argStr("command")
	case "write_file":
		if path := argStr("path"); path != "" {
			return "write " + path
		}
	case "web_fetch":
		if url := argStr("url"); url != "" {
			return "fetch " + url
		}
	}
	return ""
}
