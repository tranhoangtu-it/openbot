package domain

import (
	"context"
	"time"
)

// MemoryStore handles persistent storage of conversations, messages, and long-term memory.
type MemoryStore interface {
	CreateConversation(ctx context.Context, conv Conversation) error
	GetConversation(ctx context.Context, id string) (*Conversation, error)
	UpdateConversation(ctx context.Context, conv Conversation) error
	ListConversations(ctx context.Context, limit int) ([]Conversation, error)
	DeleteConversation(ctx context.Context, id string) error

	AddMessage(ctx context.Context, convID string, msg MessageRecord) error
	GetMessages(ctx context.Context, convID string, limit int) ([]MessageRecord, error)

	SaveMemory(ctx context.Context, mem MemoryEntry) error
	SearchMemories(ctx context.Context, query string, limit int) ([]MemoryEntry, error)
	GetRecentMemories(ctx context.Context, limit int) ([]MemoryEntry, error)

	Close() error
}

type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type MessageRecord struct {
	ID             int64     `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	ToolCalls      string    `json:"tool_calls,omitempty"`
	ToolCallID     string    `json:"tool_call_id,omitempty"`
	ToolName       string    `json:"tool_name,omitempty"`
	TokensIn       int       `json:"tokens_in"`
	TokensOut      int       `json:"tokens_out"`
	Provider       string    `json:"provider,omitempty"`
	Model          string    `json:"model,omitempty"`
	LatencyMs      int64     `json:"latency_ms,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type MemoryEntry struct {
	ID         int64      `json:"id"`
	Category   string     `json:"category"`   // fact | preference | summary | instruction
	Content    string     `json:"content"`
	Source     string     `json:"source"`      // conversation ID that generated this
	Importance int        `json:"importance"`  // 1-10
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}
