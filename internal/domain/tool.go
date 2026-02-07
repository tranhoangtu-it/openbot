package domain

import "context"

// Tool is the interface for agent capabilities (shell, file ops, search, etc).
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, args map[string]any) (string, error)
}
