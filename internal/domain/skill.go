package domain

import "context"

// SkillTrigger defines how a skill is activated.
type SkillTrigger struct {
	Keywords []string `json:"keywords,omitempty" yaml:"keywords,omitempty"`
	Pattern  string   `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	Intent   string   `json:"intent,omitempty" yaml:"intent,omitempty"`
}

// SkillStep represents a single step in a skill execution flow.
type SkillStep struct {
	Action string         `json:"action" yaml:"action"` // tool | llm | transform
	Tool   string         `json:"tool,omitempty" yaml:"tool,omitempty"`
	Prompt string         `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	Args   map[string]any `json:"args,omitempty" yaml:"args,omitempty"`
}

// SkillDefinition describes a reusable, composable workflow.
type SkillDefinition struct {
	Name        string       `json:"name" yaml:"name"`
	Description string       `json:"description" yaml:"description"`
	Version     string       `json:"version,omitempty" yaml:"version,omitempty"`
	Trigger     SkillTrigger `json:"trigger" yaml:"trigger"`
	Steps       []SkillStep  `json:"steps" yaml:"steps"`
	BuiltIn     bool         `json:"built_in" yaml:"-"`
}

// SkillInput is the input passed to a skill execution.
type SkillInput struct {
	UserMessage string
	ChatID      string
	Provider    string
	Context     map[string]any
}

// SkillOutput is the result of a skill execution.
type SkillOutput struct {
	Content  string
	Metadata map[string]any
}

// SkillRegistry manages available skills and matches them to user input.
type SkillRegistry interface {
	Register(skill SkillDefinition) error
	Match(input string) *SkillDefinition
	List() []SkillDefinition
}

// SkillExecutor runs a matched skill.
type SkillExecutor interface {
	Execute(ctx context.Context, skill SkillDefinition, input SkillInput) (*SkillOutput, error)
}
