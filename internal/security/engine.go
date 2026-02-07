package security

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"openbot/internal/config"
	"openbot/internal/domain"
)

// ConfirmFunc is a callback to request user confirmation.
// It sends the question and returns true if the user confirmed.
type ConfirmFunc func(ctx context.Context, question string) (bool, error)

// AuditLogger is the interface for writing audit entries.
type AuditLogger interface {
	LogAudit(ctx context.Context, entry domain.AuditEntry) error
}

// Engine implements the security engine with blacklist/whitelist/confirm pattern matching.
type Engine struct {
	cfg         config.SecurityConfig
	confirmFn   ConfirmFunc
	auditLogger AuditLogger
	logger      *slog.Logger

	blacklistRe []*regexp.Regexp
	whitelistRe []*regexp.Regexp
	confirmRe   []*regexp.Regexp
}

func NewEngine(cfg config.SecurityConfig, confirmFn ConfirmFunc, auditLogger AuditLogger, logger *slog.Logger) (*Engine, error) {
	e := &Engine{
		cfg:         cfg,
		confirmFn:   confirmFn,
		auditLogger: auditLogger,
		logger:      logger,
	}

	var err error
	e.blacklistRe, err = compilePatterns(cfg.Blacklist)
	if err != nil {
		return nil, fmt.Errorf("invalid blacklist pattern: %w", err)
	}

	e.whitelistRe, err = compilePatterns(cfg.Whitelist)
	if err != nil {
		return nil, fmt.Errorf("invalid whitelist pattern: %w", err)
	}

	e.confirmRe, err = compilePatterns(cfg.ConfirmPatterns)
	if err != nil {
		return nil, fmt.Errorf("invalid confirm pattern: %w", err)
	}

	return e, nil
}

func (e *Engine) Check(ctx context.Context, toolName string, command string) (domain.SecurityAction, error) {
	cmd := strings.TrimSpace(command)

	// Step 1: Check blacklist (always block)
	for _, re := range e.blacklistRe {
		if re.MatchString(cmd) {
			e.logger.Warn("command BLOCKED by blacklist",
				"tool", toolName,
				"command", cmd,
				"pattern", re.String(),
			)
			e.logAction(ctx, "command_blocked", toolName, cmd, "blocked", "blacklist match: "+re.String())
			return domain.ActionBlock, nil
		}
	}

	// Step 2: Check whitelist (always allow)
	for _, re := range e.whitelistRe {
		if re.MatchString(cmd) {
			e.logAction(ctx, "tool_exec", toolName, cmd, "allowed", "whitelist match: "+re.String())
			return domain.ActionAllow, nil
		}
	}

	// Step 3: Check confirm patterns
	for _, re := range e.confirmRe {
		if re.MatchString(cmd) {
			e.logger.Info("command requires confirmation",
				"tool", toolName,
				"command", cmd,
			)
			return domain.ActionConfirm, nil
		}
	}

	// Step 4: Default policy
	switch e.cfg.DefaultPolicy {
	case "allow":
		e.logAction(ctx, "tool_exec", toolName, cmd, "allowed", "default policy: allow")
		return domain.ActionAllow, nil
	case "deny":
		e.logAction(ctx, "command_blocked", toolName, cmd, "blocked", "default policy: deny")
		return domain.ActionBlock, nil
	default: // "ask"
		return domain.ActionConfirm, nil
	}
}

func (e *Engine) RequestConfirmation(ctx context.Context, toolName string, command string) (bool, error) {
	if e.confirmFn == nil {
		// No confirmation handler registered — deny by default
		e.logAction(ctx, "confirm_no", toolName, command, "denied", "no confirmation handler")
		return false, nil
	}

	question := fmt.Sprintf("🔒 Security Confirmation\n\nTool: %s\nCommand: %s\n\nAllow this action? (yes/no)", toolName, command)
	confirmed, err := e.confirmFn(ctx, question)
	if err != nil {
		e.logAction(ctx, "confirm_no", toolName, command, "denied", "confirmation error: "+err.Error())
		return false, err
	}

	if confirmed {
		e.logAction(ctx, "confirm_yes", toolName, command, "confirmed", "user confirmed")
	} else {
		e.logAction(ctx, "confirm_no", toolName, command, "denied", "user denied")
	}

	return confirmed, nil
}

func (e *Engine) LogAction(ctx context.Context, entry domain.AuditEntry) error {
	return e.logAction(ctx, entry.Action, entry.ToolName, entry.Command, entry.Result, entry.Details)
}

func (e *Engine) logAction(ctx context.Context, action, toolName, command, result, details string) error {
	if !e.cfg.AuditLog || e.auditLogger == nil {
		return nil
	}
	return e.auditLogger.LogAudit(ctx, domain.AuditEntry{
		Action:   action,
		ToolName: toolName,
		Command:  command,
		Result:   result,
		Details:  details,
	})
}

// Simple strings are converted to substring-match patterns.
func compilePatterns(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		// If it looks like a regex (has special chars), compile directly
		// Otherwise, treat as literal substring match
		var re *regexp.Regexp
		var err error
		if isRegex(p) {
			re, err = regexp.Compile(p)
		} else {
			re, err = regexp.Compile(`(?i)` + regexp.QuoteMeta(p))
		}
		if err != nil {
			return nil, fmt.Errorf("pattern %q: %w", p, err)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

func isRegex(s string) bool {
	for _, c := range s {
		switch c {
		case '(', ')', '[', ']', '{', '}', '|', '^', '$', '.', '*', '+', '?', '\\':
			return true
		}
	}
	return false
}
