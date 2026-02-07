package agent

import (
	"context"
	"log/slog"
	"strings"

	"openbot/internal/domain"
	"openbot/internal/knowledge"
	"openbot/internal/skill"
)

// ContextManager provides centralized context assembly from Memory, Skills, and Knowledge.
type ContextManager struct {
	memory    domain.MemoryStore
	skills    *skill.Registry
	knowledge *knowledge.Engine
	logger    *slog.Logger
}

type ContextManagerConfig struct {
	Memory    domain.MemoryStore
	Skills    *skill.Registry
	Knowledge *knowledge.Engine
	Logger    *slog.Logger
}

func NewContextManager(cfg ContextManagerConfig) *ContextManager {
	return &ContextManager{
		memory:    cfg.Memory,
		skills:    cfg.Skills,
		knowledge: cfg.Knowledge,
		logger:    cfg.Logger,
	}
}

// BuildContext assembles supplemental context for the LLM prompt based on the user message.
func (cm *ContextManager) BuildContext(ctx context.Context, userMessage string) string {
	var parts []string

	// 1. Knowledge retrieval (RAG)
	if cm.knowledge != nil {
		results, err := cm.knowledge.Search(ctx, userMessage, 3)
		if err != nil {
			cm.logger.Warn("knowledge search failed", "err", err)
		} else if len(results) > 0 {
			knowledgeCtx := cm.knowledge.BuildContext(results)
			if knowledgeCtx != "" {
				parts = append(parts, knowledgeCtx)
			}
		}
	}

	// 2. Relevant memories
	if cm.memory != nil {
		memories, err := cm.memory.SearchMemories(ctx, userMessage, 3)
		if err != nil {
			cm.logger.Warn("memory search failed", "err", err)
		} else if len(memories) > 0 {
			var memCtx strings.Builder
			memCtx.WriteString("## Relevant Memories\n\n")
			for _, m := range memories {
				memCtx.WriteString("- [" + m.Category + "] " + m.Content + "\n")
			}
			parts = append(parts, memCtx.String())
		}
	}

	// 3. Matched skill info
	if cm.skills != nil {
		if matched := cm.skills.Match(userMessage); matched != nil {
			parts = append(parts, "## Matched Skill: "+matched.Name+"\n"+matched.Description)
		}
	}

	return strings.Join(parts, "\n\n---\n\n")
}
