package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"openbot/internal/domain"
)

// Registry holds all available tools and executes them.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]domain.Tool
	logger *slog.Logger
}

func NewRegistry(logger *slog.Logger) *Registry {
	return &Registry{
		tools:  make(map[string]domain.Tool),
		logger: logger,
	}
}

func (r *Registry) Register(t domain.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
	r.logger.Debug("registered tool", "name", t.Name())
}

func (r *Registry) Get(name string) domain.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	t := r.Get(name)
	if t == nil {
		return "", fmt.Errorf("unknown tool: %s (available: %v)", name, r.Names())
	}
	return t.Execute(ctx, args)
}

// GetDefinitions returns tool definitions in OpenAI-compatible format for the LLM.
func (r *Registry) GetDefinitions() []domain.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]domain.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, domain.ToolDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return defs
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	return names
}

// Param describes a single tool parameter.
type Param struct {
	Type        string
	Description string
}

// ToolParameters builds a JSON Schema "parameters" object for a tool.
func ToolParameters(properties map[string]Param, required []string) map[string]any {
	props := make(map[string]any)
	for name, p := range properties {
		props[name] = map[string]any{"type": p.Type, "description": p.Description}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func ArgsString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
