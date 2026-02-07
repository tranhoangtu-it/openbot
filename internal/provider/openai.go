package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"openbot/internal/domain"
)

// OpenAI implements domain.Provider for OpenAI-compatible APIs (GPT-4o, GPT-4o-mini, etc.).
type OpenAI struct {
	apiKey  string
	apiBase string
	model   string
	client  *http.Client
	logger  *slog.Logger
}

type OpenAIConfig struct {
	APIKey  string
	APIBase string
	Model   string
	Logger  *slog.Logger
}

func NewOpenAI(cfg OpenAIConfig) *OpenAI {
	if cfg.APIBase == "" {
		cfg.APIBase = "https://api.openai.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	return &OpenAI{
		apiKey:  cfg.APIKey,
		apiBase: cfg.APIBase,
		model:   cfg.Model,
		client:  &http.Client{Timeout: defaultHTTPTimeout},
		logger:  cfg.Logger,
	}
}

func (o *OpenAI) Name() string                  { return "openai" }
func (o *OpenAI) Mode() domain.ProviderMode     { return domain.ModeAPI }
func (o *OpenAI) Models() []string               { return []string{"gpt-4o", "gpt-4o-mini", "gpt-4.1", "o3-mini"} }
func (o *OpenAI) SupportsToolCalling() bool       { return true }

func (o *OpenAI) Healthy(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", o.apiBase+"/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("openai not reachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("openai: invalid API key")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openai returned %d", resp.StatusCode)
	}
	return nil
}

type oaiRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	Tools       []oaiTool    `json:"tools,omitempty"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
	Stream      bool         `json:"stream"`
}

type oaiMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	ToolCalls  []oaiToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
}

type oaiTool struct {
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type oaiToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function oaiToolCallFn  `json:"function"`
}

type oaiToolCallFn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiResponse struct {
	Choices []oaiChoice `json:"choices"`
	Usage   oaiUsage    `json:"usage"`
}

type oaiChoice struct {
	Message      oaiMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

type oaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (o *OpenAI) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = o.model
	}

	msgs := make([]oaiMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		om := oaiMessage{Role: m.Role, Content: m.Content}
		if m.ToolCallID != "" {
			om.ToolCallID = m.ToolCallID
			om.Name = m.ToolName
		}
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				args, _ := json.Marshal(tc.Arguments)
				om.ToolCalls = append(om.ToolCalls, oaiToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: oaiToolCallFn{
						Name:      tc.Name,
						Arguments: string(args),
					},
				})
			}
		}
		msgs = append(msgs, om)
	}

	body := oaiRequest{
		Model:    model,
		Messages: msgs,
		Stream:   false,
	}
	if req.MaxTokens > 0 {
		body.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		body.Temperature = &req.Temperature
	}

	if len(req.Tools) > 0 {
		for _, t := range req.Tools {
			body.Tools = append(body.Tools, oaiTool{
				Type: "function",
				Function: oaiFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
				},
			})
		}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.apiBase+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai %d: %s", resp.StatusCode, string(respBody))
	}

	var oaiResp oaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	if len(oaiResp.Choices) == 0 {
		return &domain.ChatResponse{Content: "", FinishReason: "stop"}, nil
	}

	choice := oaiResp.Choices[0]
	out := &domain.ChatResponse{
		Content:      choice.Message.Content,
		FinishReason: choice.FinishReason,
		Usage: domain.Usage{
			PromptTokens:     oaiResp.Usage.PromptTokens,
			CompletionTokens: oaiResp.Usage.CompletionTokens,
			TotalTokens:      oaiResp.Usage.TotalTokens,
		},
	}

	for _, tc := range choice.Message.ToolCalls {
		var args map[string]any
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		if args == nil {
			args = make(map[string]any)
		}
		out.ToolCalls = append(out.ToolCalls, domain.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return out, nil
}
