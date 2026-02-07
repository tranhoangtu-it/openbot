package domain

import "context"

// Channel is the interface for user-facing I/O (Telegram, CLI, Web).
type Channel interface {
	Name() string
	Start(ctx context.Context, bus MessageBus) error
	Stop() error
	Send(ctx context.Context, chatID string, content string) error
}
