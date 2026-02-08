package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"openbot/internal/domain"
)

type SessionManager struct {
	store       domain.MemoryStore
	logger      *slog.Logger
	mu          sync.RWMutex
	tokenUsage  map[string]int64 // convID -> total tokens this session (in-memory, resets on restart)
	tokenUsageMu sync.RWMutex
}

func NewSessionManager(store domain.MemoryStore, logger *slog.Logger) *SessionManager {
	return &SessionManager{
		store:      store,
		logger:     logger,
		tokenUsage: make(map[string]int64),
	}
}

// AddTokenUsage adds tokens used in a completion to the conversation total (R5: session token tracking).
func (sm *SessionManager) AddTokenUsage(convID string, tokens int) {
	if tokens <= 0 {
		return
	}
	sm.tokenUsageMu.Lock()
	sm.tokenUsage[convID] += int64(tokens)
	sm.tokenUsageMu.Unlock()
}

// GetTokenUsage returns the total tokens used so far for this conversation (in-memory only).
func (sm *SessionManager) GetTokenUsage(convID string) int64 {
	sm.tokenUsageMu.RLock()
	defer sm.tokenUsageMu.RUnlock()
	return sm.tokenUsage[convID]
}

func (sm *SessionManager) GetOrCreateConversation(ctx context.Context, sessionKey, provider, model string) (string, error) {
	// Fast path: read lock (most calls hit here)
	sm.mu.RLock()
	conv, err := sm.store.GetConversation(ctx, sessionKey)
	sm.mu.RUnlock()
	if err != nil {
		return "", err
	}
	if conv != nil {
		return conv.ID, nil
	}

	// Slow path: write lock, double-check
	sm.mu.Lock()
	defer sm.mu.Unlock()

	conv, err = sm.store.GetConversation(ctx, sessionKey)
	if err != nil {
		return "", err
	}
	if conv != nil {
		return conv.ID, nil
	}

	newConv := domain.Conversation{
		ID:       sessionKey,
		Title:    "New conversation",
		Provider: provider,
		Model:    model,
	}
	if err := sm.store.CreateConversation(ctx, newConv); err != nil {
		return "", err
	}

	sm.logger.Info("created new conversation",
		"session", sessionKey,
		"provider", provider,
		"model", model,
	)

	return sessionKey, nil
}

func (sm *SessionManager) GetHistory(ctx context.Context, convID string, limit int) ([]domain.Message, error) {
	records, err := sm.store.GetMessages(ctx, convID, limit)
	if err != nil {
		return nil, err
	}

	messages := make([]domain.Message, 0, len(records))
	for _, r := range records {
		msg := domain.Message{
			Role:       r.Role,
			Content:    r.Content,
			ToolCallID: r.ToolCallID,
			ToolName:   r.ToolName,
		}

		if r.ToolCalls != "" {
			var toolCalls []domain.ToolCall
			if err := json.Unmarshal([]byte(r.ToolCalls), &toolCalls); err == nil {
				msg.ToolCalls = toolCalls
			}
		}

		messages = append(messages, msg)
	}

	return messages, nil
}

func (sm *SessionManager) UpdateTitle(ctx context.Context, convID string, firstUserMsg string) {
	conv, err := sm.store.GetConversation(ctx, convID)
	if err != nil || conv == nil {
		return
	}
	if conv.Title != "" && conv.Title != "New conversation" {
		return
	}
	title := generateTitle(firstUserMsg)
	conv.Title = title
	if err := sm.store.UpdateConversation(ctx, *conv); err != nil {
		sm.logger.Warn("failed to update conversation title", "convID", convID, "err", err)
	}
}

func generateTitle(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "New conversation"
	}
	if idx := strings.IndexAny(msg, "\n\r"); idx > 0 {
		msg = msg[:idx]
	}
	if len(msg) > 60 {
		cut := strings.LastIndex(msg[:60], " ")
		if cut < 20 {
			cut = 60
		}
		msg = msg[:cut] + "..."
	}
	return msg
}

// ClearSession deletes a conversation and its messages, effectively starting fresh.
func (sm *SessionManager) ClearSession(sessionKey string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	ctx := context.Background()
	if err := sm.store.DeleteConversation(ctx, sessionKey); err != nil {
		sm.logger.Warn("failed to clear session", "session", sessionKey, "err", err)
	} else {
		sm.logger.Info("session cleared", "session", sessionKey)
	}
}

func (sm *SessionManager) SaveMessage(ctx context.Context, convID string, msg domain.Message) error {
	record := domain.MessageRecord{
		ConversationID: convID,
		Role:           msg.Role,
		Content:        msg.Content,
		ToolCallID:     msg.ToolCallID,
		ToolName:       msg.ToolName,
	}

	if len(msg.ToolCalls) > 0 {
		data, err := json.Marshal(msg.ToolCalls)
		if err == nil {
			record.ToolCalls = string(data)
		}
	}

	return sm.store.AddMessage(ctx, convID, record)
}

func (sm *SessionManager) SaveMemory(ctx context.Context, entry domain.MemoryEntry) error {
	return sm.store.SaveMemory(ctx, entry)
}

func (sm *SessionManager) GetRelevantMemories(ctx context.Context, query string, limit int) ([]domain.MemoryEntry, error) {
	if query == "" {
		return sm.store.GetRecentMemories(ctx, limit)
	}
	return sm.store.SearchMemories(ctx, query, limit)
}
