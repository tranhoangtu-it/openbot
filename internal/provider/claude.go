package provider

import (
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
	claudeAPIURL        = "https://api.anthropic.com/v1/messages"
	claudeAPIVersion    = "2023-06-01"
	claudeDefaultModel  = "claude-sonnet-4-5-20250514"
	defaultMaxTokens    = 4096
	defaultHTTPTimeout  = 120 * time.Second
)

// Claude implements domain.Provider for Anthropic Claude API.
type Claude struct {
	apiKey string
	model  string
	client *http.Client
	logger *slog.Logger
}

type ClaudeConfig struct {
	APIKey string
	Model  string
	Logger *slog.Logger
}

// NewClaude creates a new Claude provider.
func NewClaude(cfg ClaudeConfig) *Claude {
	if cfg.Model == "" {
		cfg.Model = claudeDefaultModel
	}
	return &Claude{
		apiKey: cfg.APIKey,
		model:  cfg.Model,
		client: &http.Client{Timeout: defaultHTTPTimeout},
		logger: cfg.Logger,
	}
}

func (c *Claude) Name() string                  { return "claude" }
func (c *Claude) Mode() domain.ProviderMode     { return domain.ModeAPI }
func (c *Claude) Models() []string               { return []string{"claude-sonnet-4-5-20250514", "claude-opus-4-5-20250514", "claude-3-5-haiku-20241022"} }
func (c *Claude) SupportsToolCalling() bool       { return true }

func (c *Claude) Healthy(ctx context.Context) error {
	if c.apiKey == "" {
		return fmt.Errorf("claude: no API key configured")
	}
	// Simple check: try a minimal request
	return nil
}

type claudeRequest struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	System    string         `json:"system,omitempty"`
	Messages  []claudeMsg    `json:"messages"`
	Tools     []claudeTool   `json:"tools,omitempty"`
}

type claudeMsg struct {
	Role    string        `json:"role"`
	Content any           `json:"content"` // string or []claudeContent
}

type claudeContent struct {
	Type      string `json:"type"`                 // "text" | "tool_use" | "tool_result"
	Text      string `json:"text,omitempty"`
	ID        string `json:"id,omitempty"`          // for tool_use
	Name      string `json:"name,omitempty"`        // for tool_use
	Input     any    `json:"input,omitempty"`       // for tool_use
	ToolUseID string `json:"tool_use_id,omitempty"` // for tool_result
	Content   string `json:"content,omitempty"`     // for tool_result (nested)
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

func (c *Claude) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = c.model
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	// Separate system message from conversation
	var systemPrompt string
	var msgs []claudeMsg
	for _, m := range req.Messages {
		if m.Role == "system" {
			systemPrompt = m.Content
			continue
		}

		if m.Role == "tool" {
			// Claude expects tool results as user messages with tool_result content
			msgs = append(msgs, claudeMsg{
				Role: "user",
				Content: []claudeContent{{
					Type:      "tool_result",
					ToolUseID: m.ToolCallID,
					Content:   m.Content,
				}},
			})
			continue
		}

		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			// Assistant message with tool calls → content blocks
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
			continue
		}

		msgs = append(msgs, claudeMsg{Role: m.Role, Content: m.Content})
	}

	body := claudeRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages:  msgs,
	}

	if len(req.Tools) > 0 {
		for _, t := range req.Tools {
			body.Tools = append(body.Tools, claudeTool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.Parameters,
			})
		}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", claudeAPIURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", claudeAPIVersion)

	resp, err := c.client.Do(httpReq)
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
			if block.Input != nil {
				if m, ok := block.Input.(map[string]any); ok {
					args = m
				}
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
