// Package skill provides the Skills System — reusable, composable workflows.
package skill

import (
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"openbot/internal/domain"
)

// Registry manages available skills and matches them to user input.
type Registry struct {
	skills        []domain.SkillDefinition
	compiledRegex map[string]*regexp.Regexp // cached compiled patterns by skill name
	lowerKeywords map[string][]string       // cached lowercase keywords by skill name
	mu            sync.RWMutex
	logger        *slog.Logger
}

func NewRegistry(logger *slog.Logger) *Registry {
	return &Registry{
		compiledRegex: make(map[string]*regexp.Regexp),
		lowerKeywords: make(map[string][]string),
		logger:        logger,
	}
}

// Register adds a skill to the registry and pre-compiles its patterns.
func (r *Registry) Register(skill domain.SkillDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Pre-compute lowercase keywords
	kws := make([]string, len(skill.Trigger.Keywords))
	for i, kw := range skill.Trigger.Keywords {
		kws[i] = strings.ToLower(kw)
	}
	r.lowerKeywords[skill.Name] = kws

	// Pre-compile regex pattern
	if skill.Trigger.Pattern != "" {
		if re, err := regexp.Compile(skill.Trigger.Pattern); err == nil {
			r.compiledRegex[skill.Name] = re
		} else {
			r.logger.Warn("invalid skill trigger pattern", "skill", skill.Name, "pattern", skill.Trigger.Pattern, "err", err)
		}
	}

	// Replace existing skill with same name
	for i, s := range r.skills {
		if s.Name == skill.Name {
			r.skills[i] = skill
			r.logger.Info("skill updated", "name", skill.Name)
			return nil
		}
	}

	r.skills = append(r.skills, skill)
	r.logger.Info("skill registered", "name", skill.Name)
	return nil
}

// Match finds a skill that matches the given user input.
// Returns nil if no skill matches. Uses pre-compiled regexes and pre-lowered keywords.
func (r *Registry) Match(input string) *domain.SkillDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lowerInput := strings.ToLower(input)

	for i := range r.skills {
		skill := &r.skills[i]

		// Check pre-computed lowercase keywords
		if kws, ok := r.lowerKeywords[skill.Name]; ok {
			for _, kw := range kws {
				if strings.Contains(lowerInput, kw) {
					return skill
				}
			}
		}

		// Check pre-compiled regex
		if re, ok := r.compiledRegex[skill.Name]; ok {
			if re.MatchString(input) {
				return skill
			}
		}
	}

	return nil
}

// List returns all registered skills.
func (r *Registry) List() []domain.SkillDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]domain.SkillDefinition, len(r.skills))
	copy(result, r.skills)
	return result
}

// RegisterBuiltins loads the built-in skills.
func (r *Registry) RegisterBuiltins() {
	builtins := []domain.SkillDefinition{
		{
			Name:        "system_health",
			Description: "Check system health: CPU, memory, disk usage",
			BuiltIn:     true,
			Trigger: domain.SkillTrigger{
				Keywords: []string{"system health", "health check", "system status"},
			},
			Steps: []domain.SkillStep{
				{Action: "tool", Tool: "sysinfo"},
			},
		},
		{
			Name:        "code_review",
			Description: "Review code in a file for issues and improvements",
			BuiltIn:     true,
			Trigger: domain.SkillTrigger{
				Keywords: []string{"review code", "code review"},
				Pattern:  `(?i)review\s+(the\s+)?code`,
			},
			Steps: []domain.SkillStep{
				{Action: "tool", Tool: "read_file"},
				{Action: "llm", Prompt: "Review the following code for bugs, style issues, and potential improvements. Provide actionable suggestions."},
			},
		},
		{
			Name:        "research",
			Description: "Research a topic using web search",
			BuiltIn:     true,
			Trigger: domain.SkillTrigger{
				Keywords: []string{"research", "look up", "find information about"},
			},
			Steps: []domain.SkillStep{
				{Action: "tool", Tool: "web_search"},
				{Action: "llm", Prompt: "Summarize the search results into a clear, comprehensive answer."},
			},
		},
	}

	for _, s := range builtins {
		r.Register(s)
	}
}
