package domain

import "context"

type SecurityAction string

const (
	ActionAllow   SecurityAction = "allow"
	ActionBlock   SecurityAction = "block"
	ActionConfirm SecurityAction = "confirm"
)

// SecurityEngine evaluates commands against blacklist/whitelist/confirm policies.
type SecurityEngine interface {
	Check(ctx context.Context, toolName string, command string) (SecurityAction, error)
	RequestConfirmation(ctx context.Context, toolName string, command string) (bool, error)
	LogAction(ctx context.Context, entry AuditEntry) error
}

type AuditEntry struct {
	Action   string // tool_exec | command_blocked | confirm_yes | confirm_no
	ToolName string
	Command  string
	Result   string // allowed | blocked | confirmed | denied
	Details  string
}

type SecurityRule struct {
	ID          int64  `json:"id"`
	RuleType    string `json:"rule_type"` // whitelist | blacklist | confirm
	Pattern     string `json:"pattern"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}
