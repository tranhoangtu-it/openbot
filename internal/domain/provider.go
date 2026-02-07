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

type ChatRequest struct {
	Messages    []Message
	Tools       []ToolDefinition
	Model       string
	MaxTokens   int
	Temperature float64
	Stream      bool
	StreamCh    chan<- string
}

type ChatResponse struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string // stop | tool_calls | length
	Usage        Usage
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
