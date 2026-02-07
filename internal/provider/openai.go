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

	"openbot/internal/domain"
)

const (
	openaiDefaultBase  = "https://api.openai.com/v1"
	openaiDefaultModel = "gpt-4o-mini"
)

// OpenAI implements domain.Provider for OpenAI-compatible APIs (GPT-4o, GPT-4o-mini, etc.).
type OpenAI struct {
	apiKey  string
	apiBase string
	model   string
	client  *http.Client
	logger  *slog.Logger
}

// OpenAIConfig holds the settings for the OpenAI provider.
type OpenAIConfig struct {
	APIKey  string
	APIBase string
	Model   string
	Logger  *slog.Logger
}

// NewOpenAI creates an OpenAI provider with a shared, pooled HTTP client.
func NewOpenAI(cfg OpenAIConfig) *OpenAI {
	if cfg.APIBase == "" {
		cfg.APIBase = openaiDefaultBase
	}
	if cfg.Model == "" {
		cfg.Model = openaiDefaultModel
	}
	return &OpenAI{
		apiKey:  cfg.APIKey,
		apiBase: cfg.APIBase,
		model:   cfg.Model,
		client:  SharedHTTPClient(defaultHTTPTimeout),
		logger:  cfg.Logger,
	}
}

func (o *OpenAI) Name() string              { return "openai" }
func (o *OpenAI) Mode() domain.ProviderMode { return domain.ModeAPI }
func (o *OpenAI) Models() []string {
	return []string{"gpt-4o", "gpt-4o-mini", "gpt-4.1", "o3-mini"}
}
func (o *OpenAI) SupportsToolCalling() bool { return true }

// Healthy checks connectivity and API key validity.
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

// --- Internal request/response types ---

type oaiRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	Tools       []oaiTool    `json:"tools,omitempty"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
	Stream      bool         `json:"stream"`
}

type oaiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	Name       string        `json:"name,omitempty"`
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
	Index    int           `json:"index"` // used in streaming deltas to correlate fragments
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Function oaiToolCallFn `json:"function"`
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

// convertMessages transforms domain messages to OpenAI format.
func convertToOAIMessages(messages []domain.Message) []oaiMessage {
	msgs := make([]oaiMessage, 0, len(messages))
	for _, m := range messages {
		om := oaiMessage{Role: m.Role, Content: m.Content}
		if m.ToolCallID != "" {
			om.ToolCallID = m.ToolCallID
			om.Name = m.ToolName
		}
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
		msgs = append(msgs, om)
	}
	return msgs
}

// convertToOAITools transforms domain tool definitions to OpenAI format.
func convertToOAITools(tools []domain.ToolDefinition) []oaiTool {
	if len(tools) == 0 {
		return nil
	}
	oaiTools := make([]oaiTool, 0, len(tools))
	for _, t := range tools {
		oaiTools = append(oaiTools, oaiTool{
			Type: "function",
			Function: oaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return oaiTools
}

// buildOAIRequest creates a common request body.
func (o *OpenAI) buildOAIRequest(req domain.ChatRequest, stream bool) oaiRequest {
	model := req.Model
	if model == "" {
		model = o.model
	}
	body := oaiRequest{
		Model:    model,
		Messages: convertToOAIMessages(req.Messages),
		Tools:    convertToOAITools(req.Tools),
		Stream:   stream,
	}
	if req.MaxTokens > 0 {
		body.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		body.Temperature = &req.Temperature
	}
	return body
}

// Chat sends a chat completion request with automatic retry on transient errors.
func (o *OpenAI) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	body := o.buildOAIRequest(req, false)

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	endpoint := o.apiBase + "/chat/completions"
	buildReq := func() (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
		return httpReq, nil
	}

	resp, err := doWithRetry(ctx, o.client, buildReq, o.logger)
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

// --- Streaming types ---

type oaiStreamDelta struct {
	Role      string        `json:"role,omitempty"`
	Content   string        `json:"content,omitempty"`
	ToolCalls []oaiToolCall `json:"tool_calls,omitempty"`
}

type oaiStreamChoice struct {
	Delta        oaiStreamDelta `json:"delta"`
	FinishReason *string        `json:"finish_reason"`
}

type oaiStreamChunk struct {
	Choices []oaiStreamChoice `json:"choices"`
	Usage   *oaiUsage         `json:"usage,omitempty"`
}

// oaiPendingToolCall accumulates streamed tool-call deltas for a single call.
type oaiPendingToolCall struct {
	ID       string
	Name     string
	ArgsJSON strings.Builder
}

// ChatStream implements domain.StreamingProvider for OpenAI.
// It sends token-by-token events through the provided channel and
// accumulates streamed tool-call deltas so the final StreamDone event
// carries complete ToolCalls that the agent loop can execute.
func (o *OpenAI) ChatStream(ctx context.Context, req domain.ChatRequest, out chan<- domain.StreamEvent) error {
	defer close(out)

	body := o.buildOAIRequest(req, true)

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	endpoint := o.apiBase + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("openai stream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai %d: %s", resp.StatusCode, string(respBody))
	}

	// Accumulator for tool-call fragments streamed across multiple SSE chunks.
	// OpenAI sends tool_calls deltas with an "index" field to correlate fragments.
	var pendingCalls []oaiPendingToolCall

	// Parse SSE stream
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			// Emit final event with any accumulated tool calls.
			out <- domain.StreamEvent{
				Type:      domain.StreamDone,
				ToolCalls: o.finalizePendingCalls(pendingCalls),
			}
			return nil
		}

		var chunk oaiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			o.logger.Warn("openai stream: invalid chunk", "error", err)
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			out <- domain.StreamEvent{
				Type:    domain.StreamToken,
				Content: delta.Content,
			}
		}

		// Accumulate tool-call deltas (index-correlated).
		for _, tc := range delta.ToolCalls {
			// Grow accumulator slice to fit this index.
			for len(pendingCalls) <= tc.Index {
				pendingCalls = append(pendingCalls, oaiPendingToolCall{})
			}
			pc := &pendingCalls[tc.Index]

			if tc.ID != "" {
				pc.ID = tc.ID
			}
			if tc.Function.Name != "" {
				pc.Name = tc.Function.Name
				// Notify the frontend that a tool call is starting.
				out <- domain.StreamEvent{
					Type:   domain.StreamToolStart,
					Tool:   tc.Function.Name,
					ToolID: tc.ID,
				}
			}
			if tc.Function.Arguments != "" {
				pc.ArgsJSON.WriteString(tc.Function.Arguments)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("openai stream scan: %w", err)
	}

	// Stream ended without [DONE] — still finalize any pending calls.
	if len(pendingCalls) > 0 {
		out <- domain.StreamEvent{
			Type:      domain.StreamDone,
			ToolCalls: o.finalizePendingCalls(pendingCalls),
		}
	}

	return nil
}

// finalizePendingCalls converts accumulated tool-call fragments into domain.ToolCall values.
func (o *OpenAI) finalizePendingCalls(pending []oaiPendingToolCall) []domain.ToolCall {
	if len(pending) == 0 {
		return nil
	}
	var calls []domain.ToolCall
	for _, pc := range pending {
		if pc.Name == "" {
			continue
		}
		var args map[string]any
		if raw := pc.ArgsJSON.String(); raw != "" {
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				o.logger.Warn("openai stream: invalid tool args JSON", "tool", pc.Name, "error", err)
			}
		}
		if args == nil {
			args = make(map[string]any)
		}
		calls = append(calls, domain.ToolCall{
			ID:        pc.ID,
			Name:      pc.Name,
			Arguments: args,
		})
	}
	return calls
}

// Verify that OpenAI implements StreamingProvider.
var _ domain.StreamingProvider = (*OpenAI)(nil)
