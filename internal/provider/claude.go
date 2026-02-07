package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"openbot/internal/domain"
)

const (
	claudeAPIURL       = "https://api.anthropic.com/v1/messages"
	claudeAPIVersion   = "2023-06-01"
	claudeDefaultModel = "claude-sonnet-4-5-20250514"
	defaultMaxTokens   = 4096
	defaultHTTPTimeout = 120 * time.Second
)

// Claude implements domain.Provider for Anthropic Claude API.
type Claude struct {
	apiKey string
	model  string
	client *http.Client
	logger *slog.Logger
}

// ClaudeConfig holds settings for the Claude provider.
type ClaudeConfig struct {
	APIKey string
	Model  string
	Logger *slog.Logger
}

// NewClaude creates a new Claude provider with a shared, pooled HTTP client.
func NewClaude(cfg ClaudeConfig) *Claude {
	if cfg.Model == "" {
		cfg.Model = claudeDefaultModel
	}
	return &Claude{
		apiKey: cfg.APIKey,
		model:  cfg.Model,
		client: SharedHTTPClient(defaultHTTPTimeout),
		logger: cfg.Logger,
	}
}

func (c *Claude) Name() string              { return "claude" }
func (c *Claude) Mode() domain.ProviderMode { return domain.ModeAPI }
func (c *Claude) Models() []string {
	return []string{"claude-sonnet-4-5-20250514", "claude-opus-4-5-20250514", "claude-3-5-haiku-20241022"}
}
func (c *Claude) SupportsToolCalling() bool { return true }

// Healthy verifies that an API key is configured.
func (c *Claude) Healthy(ctx context.Context) error {
	if c.apiKey == "" {
		return fmt.Errorf("claude: no API key configured")
	}
	return nil
}

// --- Internal request/response types ---

type claudeRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	System    string       `json:"system,omitempty"`
	Messages  []claudeMsg  `json:"messages"`
	Tools     []claudeTool `json:"tools,omitempty"`
}

type claudeMsg struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []claudeContent
}

type claudeContent struct {
	Type      string `json:"type"`                  // "text" | "tool_use" | "tool_result"
	Text      string `json:"text,omitempty"`         // for text blocks
	ID        string `json:"id,omitempty"`           // for tool_use
	Name      string `json:"name,omitempty"`         // for tool_use
	Input     any    `json:"input,omitempty"`        // for tool_use
	ToolUseID string `json:"tool_use_id,omitempty"`  // for tool_result
	Content   string `json:"content,omitempty"`      // for tool_result (nested)
}

type claudeTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type claudeResponse struct {
	Content    []claudeContent `json:"content"`
	StopReason string          `json:"stop_reason"`
	Usage      claudeUsage     `json:"usage"`
}

type claudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// convertToClaudeMsgs separates the system prompt and converts domain messages to Claude format.
func convertToClaudeMsgs(messages []domain.Message) (string, []claudeMsg) {
	var systemPrompt string
	var msgs []claudeMsg
	for _, m := range messages {
		switch {
		case m.Role == "system":
			systemPrompt = m.Content

		case m.Role == "tool":
			msgs = append(msgs, claudeMsg{
				Role: "user",
				Content: []claudeContent{{
					Type:      "tool_result",
					ToolUseID: m.ToolCallID,
					Content:   m.Content,
				}},
			})

		case m.Role == "assistant" && len(m.ToolCalls) > 0:
			var blocks []claudeContent
			if m.Content != "" {
				blocks = append(blocks, claudeContent{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, claudeContent{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Arguments,
				})
			}
			msgs = append(msgs, claudeMsg{Role: "assistant", Content: blocks})

		default:
			msgs = append(msgs, claudeMsg{Role: m.Role, Content: m.Content})
		}
	}
	return systemPrompt, msgs
}

// convertToClaudeTools transforms domain tool definitions to Claude format.
func convertToClaudeTools(tools []domain.ToolDefinition) []claudeTool {
	if len(tools) == 0 {
		return nil
	}
	ct := make([]claudeTool, 0, len(tools))
	for _, t := range tools {
		ct = append(ct, claudeTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}
	return ct
}

// Chat sends a messages request to Claude with automatic retry on transient errors.
func (c *Claude) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = c.model
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	systemPrompt, msgs := convertToClaudeMsgs(req.Messages)

	body := claudeRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages:  msgs,
		Tools:     convertToClaudeTools(req.Tools),
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	buildReq := func() (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, "POST", claudeAPIURL, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("x-api-key", c.apiKey)
		httpReq.Header.Set("anthropic-version", claudeAPIVersion)
		return httpReq, nil
	}

	resp, err := doWithRetry(ctx, c.client, buildReq, c.logger)
	if err != nil {
		return nil, fmt.Errorf("claude request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("claude %d: %s", resp.StatusCode, string(respBody))
	}

	var claudeResp claudeResponse
	if err := json.NewDecoder(resp.Body).Decode(&claudeResp); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	out := &domain.ChatResponse{
		FinishReason: claudeResp.StopReason,
		Usage: domain.Usage{
			PromptTokens:     claudeResp.Usage.InputTokens,
			CompletionTokens: claudeResp.Usage.OutputTokens,
			TotalTokens:      claudeResp.Usage.InputTokens + claudeResp.Usage.OutputTokens,
		},
	}

	var textParts []string
	for _, block := range claudeResp.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			var args map[string]any
			if m, ok := block.Input.(map[string]any); ok {
				args = m
			}
			if args == nil {
				args = make(map[string]any)
			}
			out.ToolCalls = append(out.ToolCalls, domain.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: args,
			})
		}
	}
	out.Content = strings.Join(textParts, "")

	return out, nil
}

// --- Claude streaming types ---

type claudeStreamEvent struct {
	Type  string          `json:"type"`
	Delta json.RawMessage `json:"delta,omitempty"`
	Index int             `json:"index,omitempty"`
	ContentBlock *claudeContent `json:"content_block,omitempty"`
}

type claudeTextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ChatStream implements domain.StreamingProvider for Claude.
func (c *Claude) ChatStream(ctx context.Context, req domain.ChatRequest, out chan<- domain.StreamEvent) error {
	defer close(out)

	model := req.Model
	if model == "" {
		model = c.model
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	systemPrompt, msgs := convertToClaudeMsgs(req.Messages)

	// Build streaming request body
	type claudeStreamRequest struct {
		Model     string       `json:"model"`
		MaxTokens int          `json:"max_tokens"`
		System    string       `json:"system,omitempty"`
		Messages  []claudeMsg  `json:"messages"`
		Tools     []claudeTool `json:"tools,omitempty"`
		Stream    bool         `json:"stream"`
	}

	body := claudeStreamRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages:  msgs,
		Tools:     convertToClaudeTools(req.Tools),
		Stream:    true,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", claudeAPIURL, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", claudeAPIVersion)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("claude stream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("claude %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse SSE stream — Claude uses "event:" + "data:" lines
	scanner := bufio.NewScanner(resp.Body)
	var currentEvent string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		switch currentEvent {
		case "content_block_start":
			var evt claudeStreamEvent
			if err := json.Unmarshal([]byte(data), &evt); err == nil && evt.ContentBlock != nil {
				if evt.ContentBlock.Type == "tool_use" {
					out <- domain.StreamEvent{
						Type:   domain.StreamToolStart,
						Tool:   evt.ContentBlock.Name,
						ToolID: evt.ContentBlock.ID,
					}
				}
			}

		case "content_block_delta":
			var evt claudeStreamEvent
			if err := json.Unmarshal([]byte(data), &evt); err == nil && evt.Delta != nil {
				var delta claudeTextDelta
				if err := json.Unmarshal(evt.Delta, &delta); err == nil {
					if delta.Type == "text_delta" && delta.Text != "" {
						out <- domain.StreamEvent{
							Type:    domain.StreamToken,
							Content: delta.Text,
						}
					}
				}
			}

		case "message_stop":
			out <- domain.StreamEvent{Type: domain.StreamDone}
			return nil
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("claude stream scan: %w", err)
	}

	return nil
}

// Verify that Claude implements StreamingProvider.
var _ domain.StreamingProvider = (*Claude)(nil)
