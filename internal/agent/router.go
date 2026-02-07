package agent

import (
	"log/slog"
	"strings"

	"openbot/internal/config"
)

// Router classifies incoming messages and selects the appropriate agent profile.
type Router struct {
	profiles      map[string]config.AgentProfile
	lowerKeywords map[string][]string // pre-computed lowercase keywords per profile
	strategy      string              // "keyword" | "llm" | "hybrid"
	logger        *slog.Logger
}

func NewRouter(cfg config.AgentsConfig, logger *slog.Logger) *Router {
	profiles := cfg.Agents
	if profiles == nil {
		profiles = make(map[string]config.AgentProfile)
	}
	strategy := cfg.RouterStrategy
	if strategy == "" {
		strategy = "keyword"
	}

	// Pre-compute lowercase keywords to avoid repeated ToLower on every message.
	lowerKW := make(map[string][]string, len(profiles))
	for name, profile := range profiles {
		kws := make([]string, len(profile.Keywords))
		for i, kw := range profile.Keywords {
			kws[i] = strings.ToLower(kw)
		}
		lowerKW[name] = kws
	}

	return &Router{
		profiles:      profiles,
		lowerKeywords: lowerKW,
		strategy:      strategy,
		logger:        logger,
	}
}

// Route returns the name of the agent profile that should handle this message.
// Returns empty string if no specialized agent matches (use default).
func (r *Router) Route(message string) string {
	if len(r.profiles) == 0 {
		return ""
	}
	return r.routeByKeyword(message)
}

// routeByKeyword matches message content against pre-computed lowercase keywords.
func (r *Router) routeByKeyword(message string) string {
	lower := strings.ToLower(message)

	var bestMatch string
	var bestScore int

	for name, keywords := range r.lowerKeywords {
		score := 0
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			bestMatch = name
		}
	}

	if bestScore > 0 {
		r.logger.Debug("router matched agent", "agent", bestMatch, "score", bestScore)
	}
	return bestMatch
}

// GetProfile returns the agent profile for the given name.
func (r *Router) GetProfile(name string) (config.AgentProfile, bool) {
	p, ok := r.profiles[name]
	return p, ok
}
