package security

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"openbot/internal/config"
	"openbot/internal/domain"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// noopAudit discards audit entries.
type noopAudit struct{}

func (n *noopAudit) LogAudit(ctx context.Context, entry domain.AuditEntry) error { return nil }

func defaultTestCfg() config.SecurityConfig {
	return config.SecurityConfig{
		DefaultPolicy:         "ask",
		Blacklist:             []string{"rm -rf /", "mkfs"},
		Whitelist:             []string{"ls", "cat", "echo", "pwd"},
		ConfirmPatterns:       []string{"rm ", "sudo "},
		ConfirmTimeoutSeconds: 10,
		AuditLog:              true,
	}
}

func mustEngine(t *testing.T, cfg config.SecurityConfig, confirmResult bool) *Engine {
	t.Helper()
	confirmFn := func(ctx context.Context, q string) (bool, error) {
		return confirmResult, nil
	}
	e, err := NewEngine(cfg, confirmFn, &noopAudit{}, testLogger())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

// --- Check: Blacklist ---

func TestCheck_BlacklistBlocks(t *testing.T) {
	e := mustEngine(t, defaultTestCfg(), false)
	ctx := context.Background()

	action, err := e.Check(ctx, "shell", "rm -rf /")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if action != domain.ActionBlock {
		t.Fatalf("expected block, got %v", action)
	}
}

func TestCheck_BlacklistBlocksSubstring(t *testing.T) {
	e := mustEngine(t, defaultTestCfg(), false)
	ctx := context.Background()

	action, _ := e.Check(ctx, "shell", "sudo rm -rf / --no-preserve-root")
	if action != domain.ActionBlock {
		t.Fatalf("expected block for substring match, got %v", action)
	}
}

// --- Check: Whitelist ---

func TestCheck_WhitelistAllows(t *testing.T) {
	e := mustEngine(t, defaultTestCfg(), false)
	ctx := context.Background()

	action, err := e.Check(ctx, "shell", "ls -la")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if action != domain.ActionAllow {
		t.Fatalf("expected allow, got %v", action)
	}
}

func TestCheck_WhitelistExactMatch(t *testing.T) {
	e := mustEngine(t, defaultTestCfg(), false)
	ctx := context.Background()

	action, _ := e.Check(ctx, "shell", "echo hello")
	if action != domain.ActionAllow {
		t.Fatalf("expected allow for whitelisted 'echo', got %v", action)
	}
}

// --- Check: Confirm patterns ---

func TestCheck_ConfirmPattern(t *testing.T) {
	e := mustEngine(t, defaultTestCfg(), false)
	ctx := context.Background()

	action, _ := e.Check(ctx, "shell", "rm -r ./tmp")
	if action != domain.ActionConfirm {
		t.Fatalf("expected confirm, got %v", action)
	}
}

func TestCheck_SudoConfirm(t *testing.T) {
	e := mustEngine(t, defaultTestCfg(), false)
	ctx := context.Background()

	action, _ := e.Check(ctx, "shell", "sudo apt update")
	if action != domain.ActionConfirm {
		t.Fatalf("expected confirm for sudo, got %v", action)
	}
}

// --- Check: Default policy ---

func TestCheck_DefaultPolicyAllow(t *testing.T) {
	cfg := defaultTestCfg()
	cfg.DefaultPolicy = "allow"
	e := mustEngine(t, cfg, false)
	ctx := context.Background()

	// Command not matching any pattern
	action, _ := e.Check(ctx, "shell", "go build ./...")
	if action != domain.ActionAllow {
		t.Fatalf("expected allow (default policy), got %v", action)
	}
}

func TestCheck_DefaultPolicyDeny(t *testing.T) {
	cfg := defaultTestCfg()
	cfg.DefaultPolicy = "deny"
	e := mustEngine(t, cfg, false)
	ctx := context.Background()

	action, _ := e.Check(ctx, "shell", "go build ./...")
	if action != domain.ActionBlock {
		t.Fatalf("expected block (deny policy), got %v", action)
	}
}

func TestCheck_DefaultPolicyAsk(t *testing.T) {
	cfg := defaultTestCfg()
	cfg.DefaultPolicy = "ask"
	e := mustEngine(t, cfg, false)
	ctx := context.Background()

	action, _ := e.Check(ctx, "shell", "go build ./...")
	if action != domain.ActionConfirm {
		t.Fatalf("expected confirm (ask policy), got %v", action)
	}
}

// --- RequestConfirmation ---

func TestRequestConfirmation_Approved(t *testing.T) {
	e := mustEngine(t, defaultTestCfg(), true)
	ctx := context.Background()

	confirmed, err := e.RequestConfirmation(ctx, "shell", "rm something")
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !confirmed {
		t.Fatal("expected confirmed=true")
	}
}

func TestRequestConfirmation_Denied(t *testing.T) {
	e := mustEngine(t, defaultTestCfg(), false)
	ctx := context.Background()

	confirmed, _ := e.RequestConfirmation(ctx, "shell", "rm something")
	if confirmed {
		t.Fatal("expected confirmed=false")
	}
}

func TestRequestConfirmation_NoHandler(t *testing.T) {
	e, err := NewEngine(defaultTestCfg(), nil, &noopAudit{}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	confirmed, _ := e.RequestConfirmation(ctx, "shell", "rm something")
	if confirmed {
		t.Fatal("should deny when no handler")
	}
}

// --- Edge cases ---

func TestCheck_EmptyCommand(t *testing.T) {
	e := mustEngine(t, defaultTestCfg(), false)
	ctx := context.Background()

	// Empty command should fall through to default policy
	action, err := e.Check(ctx, "shell", "")
	if err != nil {
		t.Fatalf("check empty: %v", err)
	}
	// Default is "ask"
	if action != domain.ActionConfirm {
		t.Fatalf("expected confirm for empty command, got %v", action)
	}
}

func TestCheck_WhitespaceTrimmed(t *testing.T) {
	e := mustEngine(t, defaultTestCfg(), false)
	ctx := context.Background()

	action, _ := e.Check(ctx, "shell", "  rm -rf /  ")
	if action != domain.ActionBlock {
		t.Fatalf("expected block after trimming whitespace, got %v", action)
	}
}

// --- Priority: blacklist > whitelist > confirm > default ---

func TestCheck_BlacklistOverridesWhitelist(t *testing.T) {
	cfg := defaultTestCfg()
	// "ls" is whitelisted but "rm -rf /" is blacklisted
	// A command matching both should be blocked
	cfg.Blacklist = []string{"dangerous"}
	cfg.Whitelist = []string{"dangerous"}
	e := mustEngine(t, cfg, false)
	ctx := context.Background()

	action, _ := e.Check(ctx, "shell", "dangerous")
	if action != domain.ActionBlock {
		t.Fatalf("blacklist should take priority over whitelist, got %v", action)
	}
}

// --- NewEngine: Invalid patterns ---

func TestNewEngine_InvalidBlacklistPattern(t *testing.T) {
	cfg := defaultTestCfg()
	cfg.Blacklist = []string{"[invalid regex"}
	_, err := NewEngine(cfg, nil, &noopAudit{}, nil)
	if err == nil {
		t.Fatal("expected error for invalid blacklist regex")
	}
}
