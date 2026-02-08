package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"openbot/internal/config"
	"openbot/internal/domain"
)

// AgentMessage represents a message passed between agents.
type AgentMessage struct {
	FromAgent string
	ToAgent   string
	Content   string
	TaskID    string
	Timestamp time.Time
}

// AgentResult is the result of an agent processing a delegated task.
type AgentResult struct {
	AgentName string
	TaskID    string
	Content   string
	Error     error
	Duration  time.Duration
}

// Orchestrator manages multi-agent communication and task delegation.
type Orchestrator struct {
	router   *Router
	agents   map[string]*agentContext
	provider domain.Provider
	logger   *slog.Logger
	mu       sync.RWMutex
}

// agentContext holds the isolated state for a specialized agent.
type agentContext struct {
	name        string
	profile     config.AgentProfile
	history     []domain.Message
	maxHistory  int
}

// OrchestratorConfig configures the orchestrator.
type OrchestratorConfig struct {
	Router   *Router
	Provider domain.Provider
	Logger   *slog.Logger
}

// NewOrchestrator creates a new multi-agent orchestrator.
func NewOrchestrator(cfg OrchestratorConfig) *Orchestrator {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Orchestrator{
		router:   cfg.Router,
		agents:   make(map[string]*agentContext),
		provider: cfg.Provider,
		logger:   cfg.Logger,
	}
}

// RouteMessage determines which agent should handle a message and returns its name.
func (o *Orchestrator) RouteMessage(content string) string {
	if o.router == nil {
		return ""
	}
	return o.router.Route(content)
}

// DelegateTask sends a task to a specific agent and waits for its response.
func (o *Orchestrator) DelegateTask(ctx context.Context, agentName, taskContent string) (*AgentResult, error) {
	o.mu.Lock()
	ac, ok := o.agents[agentName]
	if !ok {
		profile, profileOK := o.router.GetProfile(agentName)
		if !profileOK {
			o.mu.Unlock()
			return nil, fmt.Errorf("unknown agent: %s", agentName)
		}
		ac = &agentContext{
			name:       agentName,
			profile:    profile,
			maxHistory: 20,
		}
		o.agents[agentName] = ac
	}
	o.mu.Unlock()

	start := time.Now()

	// Build messages for this agent.
	messages := make([]domain.Message, 0, len(ac.history)+2)
	if ac.profile.SystemPrompt != "" {
		messages = append(messages, domain.Message{
			Role:    "system",
			Content: ac.profile.SystemPrompt,
		})
	}
	messages = append(messages, ac.history...)
	messages = append(messages, domain.Message{
		Role:    "user",
		Content: taskContent,
	})

	resp, err := o.provider.Chat(ctx, domain.ChatRequest{
		Messages:    messages,
		MaxTokens:   4096,
		Temperature: 0.7,
	})
	if err != nil {
		return &AgentResult{
			AgentName: agentName,
			Error:     err,
			Duration:  time.Since(start),
		}, err
	}

	// Update agent's history.
	o.mu.Lock()
	ac.history = append(ac.history, domain.Message{Role: "user", Content: taskContent})
	ac.history = append(ac.history, domain.Message{Role: "assistant", Content: resp.Content})
	// Trim history
	if len(ac.history) > ac.maxHistory {
		ac.history = ac.history[len(ac.history)-ac.maxHistory:]
	}
	o.mu.Unlock()

	o.logger.Info("agent task completed",
		"agent", agentName,
		"duration_ms", time.Since(start).Milliseconds(),
		"response_len", len(resp.Content),
	)

	return &AgentResult{
		AgentName: agentName,
		Content:   resp.Content,
		Duration:  time.Since(start),
	}, nil
}

// ClearAgentContext resets an agent's conversation history.
func (o *Orchestrator) ClearAgentContext(agentName string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if ac, ok := o.agents[agentName]; ok {
		ac.history = nil
	}
}

// ListAgents returns the names of all registered agents.
func (o *Orchestrator) ListAgents() []string {
	if o.router == nil {
		return nil
	}
	var names []string
	for name := range o.router.profiles {
		names = append(names, name)
	}
	return names
}
