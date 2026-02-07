package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the root configuration for OpenBot.
type Config struct {
	General   GeneralConfig              `json:"general"`
	Providers map[string]ProviderConfig  `json:"providers"`
	Channels  ChannelsConfig             `json:"channels"`
	Memory    MemoryConfig               `json:"memory"`
	Security  SecurityConfig             `json:"security"`
	Tools     ToolsConfig                `json:"tools"`
	Cron      CronConfig                 `json:"cron"`
}

type GeneralConfig struct {
	Workspace       string `json:"workspace"`
	LogLevel        string `json:"logLevel"`
	MaxIterations   int    `json:"maxIterations"`
	DefaultProvider string `json:"defaultProvider"`
}

type ProviderConfig struct {
	Enabled           bool              `json:"enabled"`
	Mode              string            `json:"mode"` // "api" | "browser"
	APIBase           string            `json:"apiBase,omitempty"`
	APIKey            string            `json:"apiKey,omitempty"`
	DefaultModel      string            `json:"defaultModel,omitempty"`
	ProfileDir        string            `json:"profileDir,omitempty"`
	Selectors         map[string]string `json:"selectors,omitempty"`
	RateLimitPerMin   int               `json:"rateLimitPerMinute,omitempty"`
}

type ChannelsConfig struct {
	Telegram TelegramConfig `json:"telegram"`
	Web      WebConfig      `json:"web"`
	CLI      CLIConfig      `json:"cli"`
}

type TelegramConfig struct {
	Enabled   bool           `json:"enabled"`
	Token     string         `json:"token"`
	AllowFrom FlexStringList `json:"allowFrom"`
	ParseMode string         `json:"parseMode"`
}

// FlexStringList is a []string that can unmarshal from JSON arrays containing
// both strings and numbers (e.g. ["123", 456] both become "123", "456").
type FlexStringList []string

func (f *FlexStringList) UnmarshalJSON(data []byte) error {
	// Try []string first
	var ss []string
	if err := json.Unmarshal(data, &ss); err == nil {
		*f = ss
		return nil
	}
	// Fallback: array of mixed types
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		var s string
		if err := json.Unmarshal(item, &s); err == nil {
			result = append(result, s)
			continue
		}
		var n float64
		if err := json.Unmarshal(item, &n); err == nil {
			result = append(result, strconv.FormatInt(int64(n), 10))
			continue
		}
		result = append(result, string(item))
	}
	*f = result
	return nil
}

type WebConfig struct {
	Enabled bool      `json:"enabled"`
	Host    string    `json:"host"`
	Port    int       `json:"port"`
	Auth    WebAuth   `json:"auth"`
}

type WebAuth struct {
	Enabled      bool   `json:"enabled"`
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
}

type CLIConfig struct {
	Enabled bool `json:"enabled"`
}

type MemoryConfig struct {
	Enabled                    bool   `json:"enabled"`
	DBPath                     string `json:"dbPath"`
	MaxHistoryPerConversation  int    `json:"maxHistoryPerConversation"`
	RetentionDays              int    `json:"retentionDays"`
}

type SecurityConfig struct {
	DefaultPolicy         string   `json:"defaultPolicy"` // "allow" | "deny" | "ask"
	WorkspaceSandbox      bool     `json:"workspaceSandbox"`
	Blacklist             []string `json:"blacklist"`
	Whitelist             []string `json:"whitelist"`
	ConfirmPatterns       []string `json:"confirmPatterns"`
	ConfirmTimeoutSeconds int      `json:"confirmTimeoutSeconds"`
	AuditLog              bool     `json:"auditLog"`
}

type ToolsConfig struct {
	Shell  ShellToolConfig  `json:"shell"`
	Screen ScreenToolConfig `json:"screen"`
	Web    WebToolConfig    `json:"web"`
}

type ShellToolConfig struct {
	Timeout        int `json:"timeout"`
	MaxOutputBytes int `json:"maxOutputBytes"`
}

type ScreenToolConfig struct {
	Enabled bool `json:"enabled"`
}

type WebToolConfig struct {
	SearchProvider string `json:"searchProvider"`
	SearchAPIKey   string `json:"searchApiKey"`
}

type CronConfig struct {
	Enabled bool       `json:"enabled"`
	Tasks   []CronTask `json:"tasks"`
}

type CronTask struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Message   string `json:"message"`
	CronExpr  string `json:"cronExpr,omitempty"`
	IntervalS int    `json:"intervalSeconds,omitempty"`
	Channel   string `json:"channel"`
	ChatID    string `json:"chatId"`
	Enabled   bool   `json:"enabled"`
}

// DefaultConfigDir returns the default config directory (~/.openbot).
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".openbot"
	}
	return filepath.Join(home, ".openbot")
}

func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir(), "config.json")
}

func Load(path string) (*Config, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot resolve home directory: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read config file %s: %w", path, err)
	}

	cfg := Defaults()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("cannot parse config file %s: %w", path, err)
	}

	cfg.General.Workspace = expandPath(cfg.General.Workspace)
	cfg.Memory.DBPath = expandPath(cfg.Memory.DBPath)

	return cfg, nil
}

func Save(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal config: %w", err)
	}

	return os.WriteFile(path, data, 0o644)
}

// Validate checks that the config has valid values.
func Validate(cfg *Config) error {
	if cfg.General.MaxIterations < 1 || cfg.General.MaxIterations > 200 {
		return fmt.Errorf("general.maxIterations must be between 1 and 200")
	}
	if cfg.Channels.Web.Port < 0 || cfg.Channels.Web.Port > 65535 {
		return fmt.Errorf("channels.web.port must be between 0 and 65535")
	}
	if cfg.Memory.MaxHistoryPerConversation < 1 {
		return fmt.Errorf("memory.maxHistoryPerConversation must be >= 1")
	}
	if cfg.Memory.RetentionDays < 1 {
		return fmt.Errorf("memory.retentionDays must be >= 1")
	}
	if cfg.Tools.Shell.Timeout < 1 {
		return fmt.Errorf("tools.shell.timeout must be >= 1")
	}
	switch cfg.Security.DefaultPolicy {
	case "allow", "deny", "ask":
		// valid
	default:
		return fmt.Errorf("security.defaultPolicy must be one of: allow, deny, ask")
	}
	return nil
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
