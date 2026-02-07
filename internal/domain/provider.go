package domain

import "context"

type ProviderMode string

const (
	ModeAPI     ProviderMode = "api"
	ModeBrowser ProviderMode = "browser"
)

// Provider is the interface all LLM providers must implement.
type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	Name() string
	Mode() ProviderMode
	Models() []string
	SupportsToolCalling() bool
	Healthy(ctx context.Context) error
}

// StreamingProvider is an optional extension for providers that support
// token-by-token streaming. Providers that implement this can deliver
// incremental responses through StreamEvent channels.
type StreamingProvider interface {
	Provider
	ChatStream(ctx context.Context, req ChatRequest, out chan<- StreamEvent) error
}

// StreamEventType classifies a streaming event.
type StreamEventType string

const (
	StreamToken     StreamEventType = "token"
	StreamThinking  StreamEventType = "thinking"
	StreamToolStart StreamEventType = "tool_start"
	StreamToolEnd   StreamEventType = "tool_end"
	StreamDone      StreamEventType = "done"
	StreamError     StreamEventType = "error"
)

// StreamEvent represents a single streaming event from an LLM provider.
type StreamEvent struct {
	Type      StreamEventType `json:"type"`
	Content   string          `json:"content,omitempty"`     // token text or error message
	Tool      string          `json:"tool,omitempty"`        // tool name for tool_start/tool_end
	ToolID    string          `json:"tool_id,omitempty"`     // tool call ID
	ToolCalls []ToolCall      `json:"tool_calls,omitempty"`  // complete tool calls (emitted with StreamDone)
}

type ChatRequest struct {
	Messages    []Message
	Tools       []ToolDefinition
	Model       string
	MaxTokens   int
	Temperature float64
	Stream      bool
	StreamCh    chan<- string // deprecated: use StreamingProvider.ChatStream instead
	Provider    string        // optional: override default provider for this request
}

type ChatResponse struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string // stop | tool_calls | length
	Usage        Usage
	LatencyMs    int64 // time taken for this LLM call in milliseconds
}

func (r *ChatResponse) HasToolCalls() bool {
	return len(r.ToolCalls) > 0
}

type Message struct {
	Role       string     `json:"role"`                   // system | user | assistant | tool
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
}

type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
