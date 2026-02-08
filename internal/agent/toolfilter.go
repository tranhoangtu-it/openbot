package agent

import "openbot/internal/domain"

// ToolFilter applies allow/deny rules to tool definitions and tool execution.
type ToolFilter struct {
	allowedTools map[string]bool // if non-empty, only these tools are allowed
	deniedTools  map[string]bool // these tools are always denied
}

// NewToolFilter creates a tool filter from allow/deny lists.
// If allowedTools is non-empty, only those tools are permitted.
// DeniedTools are always blocked regardless of allow list.
func NewToolFilter(allowed, denied []string) *ToolFilter {
	tf := &ToolFilter{
		allowedTools: make(map[string]bool),
		deniedTools:  make(map[string]bool),
	}
	for _, t := range allowed {
		tf.allowedTools[t] = true
	}
	for _, t := range denied {
		tf.deniedTools[t] = true
	}
	return tf
}

// FilterDefinitions returns only the tool definitions that pass the filter.
func (tf *ToolFilter) FilterDefinitions(defs []domain.ToolDefinition) []domain.ToolDefinition {
	if tf == nil || (len(tf.allowedTools) == 0 && len(tf.deniedTools) == 0) {
		return defs
	}

	filtered := make([]domain.ToolDefinition, 0, len(defs))
	for _, d := range defs {
		if tf.IsAllowed(d.Name) {
			filtered = append(filtered, d)
		}
	}
	return filtered
}

// IsAllowed returns true if the tool name passes the filter.
func (tf *ToolFilter) IsAllowed(name string) bool {
	if tf == nil {
		return true
	}
	// Deny list always wins.
	if tf.deniedTools[name] {
		return false
	}
	// If allow list is set, tool must be in it.
	if len(tf.allowedTools) > 0 {
		return tf.allowedTools[name]
	}
	return true
}

// IsEmpty returns true if the filter has no rules.
func (tf *ToolFilter) IsEmpty() bool {
	return tf == nil || (len(tf.allowedTools) == 0 && len(tf.deniedTools) == 0)
}
