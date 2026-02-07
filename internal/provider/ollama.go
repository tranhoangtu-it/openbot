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
	ollamaDefaultBase  = "http://localhost:11434"
	ollamaDefaultModel = "llama3.1:8b"
	ollamaMaxRetries   = 3
)

// Ollama implements domain.Provider for Ollama (local or cloud).
type Ollama struct {
	apiBase      string
	defaultModel string
	client       *http.Client
	logger       *slog.Logger
}

type OllamaConfig struct {
	APIBase      string
	DefaultModel string
	Logger       *slog.Logger
}

func NewOllama(cfg OllamaConfig) *Ollama {
	if cfg.APIBase == "" {
		cfg.APIBase = ollamaDefaultBase
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = ollamaDefaultModel
	}
	return &Ollama{
		apiBase:      cfg.APIBase,
		defaultModel: cfg.DefaultModel,
		client:       &http.Client{Timeout: defaultHTTPTimeout},
		logger:       cfg.Logger,
	}
}

func NewOllamaWithClient(cfg OllamaConfig, client *http.Client) *Ollama {
	if cfg.APIBase == "" {
		cfg.APIBase = ollamaDefaultBase
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = ollamaDefaultModel
	}
	if client == nil {
		client = &http.Client{}
	}
	return &Ollama{
		apiBase:      cfg.APIBase,
		defaultModel: cfg.DefaultModel,
		client:       client,
		logger:       cfg.Logger,
	}
}

func (o *Ollama) Name() string { return "ollama" }

func (o *Ollama) Mode() domain.ProviderMode { return domain.ModeAPI }

// Models returns available models (we list from API or use a default).
func (o *Ollama) Models() []string {
	// Common defaults; full list would require GET /api/tags
	return []string{"llama3.1:8b", "llama3.1:70b", "llama3.2:3b", "mistral", "codellama", "phi3"}
}

func (o *Ollama) SupportsToolCalling() bool { return true }

func (o *Ollama) Healthy(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", o.apiBase+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("ollama not reachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}
	return nil
}

// ollamaRequest matches the Ollama /api/chat request body.
type ollamaRequest struct {
	Model       string        `json:"model"`
	Messages    []ollamaMsg   `json:"messages"`
	Stream      bool          `json:"stream"`
	Tools       []ollamaTool  `json:"tools,omitempty"`
	Options     map[string]any `json:"options,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
}

type ollamaMsg struct {
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	ToolCalls  []ollamaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
}

type ollamaTool struct {
	Type     string       `json:"type"`
	Function ollamaFunc   `json:"function"`
}

type ollamaFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ollamaToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function ollamaFuncCall `json:"function"`
}

type ollamaFuncCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"` // Can be JSON object or JSON string
}

type ollamaResponse struct {
	Message    ollamaMsg  `json:"message"`
	Done       bool       `json:"done"`
	DoneReason string     `json:"done_reason"`
}

func (o *Ollama) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = o.defaultModel
	}

	msgs := make([]ollamaMsg, 0, len(req.Messages))
	for _, m := range req.Messages {
		om := ollamaMsg{Role: m.Role, Content: m.Content}
		if m.ToolCallID != "" {
			om.ToolCallID = m.ToolCallID
			om.Name = m.ToolName
		}
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				argsRaw, err := json.Marshal(tc.Arguments)
				if err != nil {
					argsRaw = []byte("{}")
				}
				om.ToolCalls = append(om.ToolCalls, ollamaToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: ollamaFuncCall{
						Name:      tc.Name,
						Arguments: json.RawMessage(argsRaw),
					},
				})
			}
		}
		msgs = append(msgs, om)
	}

	streaming := req.Stream && req.StreamCh != nil
	body := ollamaRequest{
		Model:    model,
		Messages: msgs,
		Stream:   streaming,
	}
	if req.Temperature > 0 {
		body.Temperature = &req.Temperature
	}

	if len(req.Tools) > 0 {
		body.Tools = make([]ollamaTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			body.Tools = append(body.Tools, ollamaTool{
				Type: "function",
				Function: ollamaFunc{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
				},
			})
		}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Retry logic for transient errors (connection refused, 5xx, timeout)
	var ollamaResp ollamaResponse
	for attempt := 0; attempt <= ollamaMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*attempt) * time.Second
			o.logger.Warn("retrying ollama request", "attempt", attempt+1, "backoff", backoff)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", o.apiBase+"/api/chat", bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("new request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := o.client.Do(httpReq)
		if err != nil {
			if attempt < ollamaMaxRetries {
				o.logger.Warn("ollama request failed, will retry", "err", err)
				continue
			}
			return nil, fmt.Errorf("ollama request (after %d retries): %w", ollamaMaxRetries, err)
		}

		if resp.StatusCode >= 500 {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if attempt < ollamaMaxRetries {
				o.logger.Warn("ollama server error, will retry", "status", resp.StatusCode, "body", string(respBody))
				continue
			}
			return nil, fmt.Errorf("ollama returned %d (after %d retries): %s", resp.StatusCode, ollamaMaxRetries, string(respBody))
		}

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("ollama returned %d: %s", resp.StatusCode, string(respBody))
		}

		// Streaming mode: read NDJSON line by line
		if streaming {
			return o.readStream(resp, req.StreamCh)
		}

		if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
			resp.Body.Close()
			if attempt < ollamaMaxRetries {
				o.logger.Warn("ollama decode error, will retry", "err", err)
				continue
			}
			return nil, fmt.Errorf("decode response (after %d retries): %w", ollamaMaxRetries, err)
		}
		resp.Body.Close()
		break
	}

	return o.buildResponse(ollamaResp), nil
}

func (o *Ollama) readStream(resp *http.Response, streamCh chan<- string) (*domain.ChatResponse, error) {
	defer resp.Body.Close()

	var fullContent strings.Builder
	var lastResp ollamaResponse

	decoder := json.NewDecoder(resp.Body)
	for decoder.More() {
		var chunk ollamaResponse
		if err := decoder.Decode(&chunk); err != nil {
			if fullContent.Len() > 0 {
				break // Partial success
			}
			return nil, fmt.Errorf("stream decode: %w", err)
		}

		if chunk.Message.Content != "" {
			fullContent.WriteString(chunk.Message.Content)
			select {
			case streamCh <- chunk.Message.Content:
			default:
				// Channel full — skip this token (consumer too slow)
			}
		}

		if chunk.Done {
			lastResp = chunk
			lastResp.Message.Content = fullContent.String()
			break
		}
	}

	return o.buildResponse(lastResp), nil
}

func (o *Ollama) buildResponse(ollamaResp ollamaResponse) *domain.ChatResponse {
	out := &domain.ChatResponse{
		Content:      ollamaResp.Message.Content,
		FinishReason: ollamaResp.DoneReason,
	}

	for _, tc := range ollamaResp.Message.ToolCalls {
		var args map[string]any
		if len(tc.Function.Arguments) > 0 {
			raw := tc.Function.Arguments
			// Ollama may return arguments as a JSON string or a JSON object.
			if raw[0] == '"' {
				var s string
				if err := json.Unmarshal(raw, &s); err == nil {
					_ = json.Unmarshal([]byte(s), &args)
				}
			} else {
				_ = json.Unmarshal(raw, &args)
			}
		}
		if args == nil {
			args = make(map[string]any)
		}
		out.ToolCalls = append(out.ToolCalls, domain.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return out
}
