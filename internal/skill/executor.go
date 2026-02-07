package skill

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"openbot/internal/domain"
	"openbot/internal/tool"
)

// Executor runs a matched skill by executing its steps sequentially.
type Executor struct {
	tools    *tool.Registry
	provider domain.Provider
	logger   *slog.Logger
}

func NewExecutor(tools *tool.Registry, provider domain.Provider, logger *slog.Logger) *Executor {
	return &Executor{
		tools:    tools,
		provider: provider,
		logger:   logger,
	}
}

// Execute runs all steps in a skill definition and returns the combined output.
func (e *Executor) Execute(ctx context.Context, skill domain.SkillDefinition, input domain.SkillInput) (*domain.SkillOutput, error) {
	e.logger.Info("executing skill", "name", skill.Name, "steps", len(skill.Steps))

	var accumulated []string

	for i, step := range skill.Steps {
		e.logger.Debug("skill step", "index", i, "action", step.Action, "tool", step.Tool)

		switch step.Action {
		case "tool":
			if e.tools == nil {
				return nil, fmt.Errorf("tool registry not available")
			}
			args := make(map[string]any)
			for k, v := range step.Args {
				args[k] = v
			}
			// If no args provided, pass the user message as the primary arg
			if len(args) == 0 {
				args["query"] = input.UserMessage
				args["command"] = input.UserMessage
			}

			result, err := e.tools.Execute(ctx, step.Tool, args)
			if err != nil {
				e.logger.Warn("skill tool failed", "tool", step.Tool, "err", err)
				accumulated = append(accumulated, fmt.Sprintf("Tool %s error: %s", step.Tool, err))
			} else {
				accumulated = append(accumulated, result)
			}

		case "llm":
			if e.provider == nil {
				return nil, fmt.Errorf("LLM provider not available")
			}
			prompt := step.Prompt
			if len(accumulated) > 0 {
				prompt += "\n\n---\n" + strings.Join(accumulated, "\n---\n")
			}

			resp, err := e.provider.Chat(ctx, domain.ChatRequest{
				Messages: []domain.Message{
					{Role: "system", Content: "You are executing a skill step. Be concise and focused."},
					{Role: "user", Content: prompt},
				},
				MaxTokens:   2048,
				Temperature: 0.5,
			})
			if err != nil {
				return nil, fmt.Errorf("LLM step error: %w", err)
			}
			accumulated = append(accumulated, resp.Content)

		case "transform":
			// Placeholder for future data transformation steps
			e.logger.Debug("transform step (no-op)")

		default:
			e.logger.Warn("unknown skill step action", "action", step.Action)
		}
	}

	content := strings.Join(accumulated, "\n\n")
	return &domain.SkillOutput{
		Content: content,
		Metadata: map[string]any{
			"skill":      skill.Name,
			"steps":      len(skill.Steps),
		},
	}, nil
}
