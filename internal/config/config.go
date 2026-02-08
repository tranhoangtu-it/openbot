package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	Agents    AgentsConfig               `json:"agents"`
	Knowledge KnowledgeConfig            `json:"knowledge"`
	Metrics   MetricsConfig              `json:"metrics"`
	API       APIConfig                  `json:"api"`
	MCP       MCPConfig                  `json:"mcp,omitempty"`
}

// MCPConfig configures Model Context Protocol (MCP) server connections.
// When enabled, tools from MCP servers are registered in the agent tool registry (prefix: mcp_<server>_<tool>).
type MCPConfig struct {
	Enabled  bool             `json:"enabled"`
	Servers  []MCPServerEntry `json:"servers,omitempty"`
}

// MCPServerEntry configures a single MCP server (sync with internal/mcp.ServerConfig).
type MCPServerEntry struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"` // "stdio" | "http" | "sse"
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	URL       string            `json:"url,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

type GeneralConfig struct {
	Workspace             string   `json:"workspace"`
	LogLevel              string   `json:"logLevel"`
	LogFile               string   `json:"logFile,omitempty"`       // optional log file path
	MaxIterations         int      `json:"maxIterations"`
	DefaultProvider       string   `json:"defaultProvider"`
	FailoverChain         []string `json:"failoverChain,omitempty"` // provider failover order
	MaxConcurrentMessages int      `json:"maxConcurrentMessages"`
	MaxContextTokens      int      `json:"maxContextTokens,omitempty"`   // token budget for context window (default: 4096)
	MaxTokensPerSession  int      `json:"maxTokensPerSession,omitempty"` // 0 = disabled; per-conversation cap (R5)
	TokenBudgetAlert     int      `json:"tokenBudgetAlert,omitempty"`   // 0 = disabled; log warning when session reaches this (R5)
	ThinkingLevel         string   `json:"thinkingLevel,omitempty"` // "concise" | "normal" | "detailed"
	SystemPromptExtra     string   `json:"systemPromptExtra,omitempty"` // custom text appended to system prompt
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
	WhatsApp WhatsAppConfig `json:"whatsapp"`
	Discord  DiscordConfig  `json:"discord,omitempty"`
	Slack    SlackConfig    `json:"slack,omitempty"`
}

type DiscordConfig struct {
	Enabled  bool   `json:"enabled"`
	Token    string `json:"token"`
	GuildID  string `json:"guildId,omitempty"` // optional: restrict to specific guild
}

type SlackConfig struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"botToken"`
	AppToken string `json:"appToken"` // required for Socket Mode
}

type WhatsAppConfig struct {
	Enabled       bool   `json:"enabled"`
	AppID         string `json:"appId,omitempty"`
	AppSecret     string `json:"appSecret,omitempty"`
	AccessToken   string `json:"accessToken,omitempty"`
	VerifyToken   string `json:"verifyToken,omitempty"`
	PhoneNumberID string `json:"phoneNumberId,omitempty"`
	WebhookPath   string `json:"webhookPath,omitempty"`
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
	PairingRequired       bool     `json:"pairingRequired,omitempty"` // require DM pairing before interaction
	PairingTTLDays        int      `json:"pairingTTLDays,omitempty"`  // pairing expiry in days (default: 30)
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

// AgentsConfig configures the multi-agent routing system.
type AgentsConfig struct {
	Enabled        bool              `json:"enabled"`
	Mode           string            `json:"mode"`           // "single" | "multi"
	RouterStrategy string            `json:"routerStrategy"` // "keyword" | "llm" | "hybrid"
	Agents         map[string]AgentProfile `json:"agents,omitempty"`
}

// AgentProfile configures a specialized agent in multi-agent mode.
type AgentProfile struct {
	SystemPrompt string   `json:"systemPrompt,omitempty"`
	Provider     string   `json:"provider,omitempty"`
	Tools        []string `json:"tools,omitempty"`        // deprecated: use AllowedTools
	AllowedTools []string `json:"allowedTools,omitempty"` // whitelist of allowed tool names
	DeniedTools  []string `json:"deniedTools,omitempty"`  // blacklist of denied tool names
	Keywords     []string `json:"keywords,omitempty"`
}

// KnowledgeConfig configures the RAG knowledge engine.
type KnowledgeConfig struct {
	Enabled      bool   `json:"enabled"`
	MaxDocuments int    `json:"maxDocuments"`
	ChunkSize    int    `json:"chunkSize"`    // tokens per chunk
	ChunkOverlap int    `json:"chunkOverlap"` // overlapping tokens
	SearchTopK   int    `json:"searchTopK"`
	StoragePath  string `json:"storagePath,omitempty"`
}

// MetricsConfig configures the observability / Prometheus metrics.
type MetricsConfig struct {
	Enabled       bool   `json:"enabled"`
	Endpoint      string `json:"endpoint"`
	RetentionDays int    `json:"retentionDays"`
}

// APIConfig configures the OpenAI-compatible API gateway.
type APIConfig struct {
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"`
	APIKey  string `json:"apiKey,omitempty"`
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

	// Substitute environment variables: ${VAR} and ${VAR:-default}
	data = []byte(ExpandEnvVars(string(data)))

	cfg := Defaults()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("cannot parse config file %s: %w", path, err)
	}

	cfg.General.Workspace = expandPath(cfg.General.Workspace)
	cfg.Memory.DBPath = expandPath(cfg.Memory.DBPath)
	cfg.General.LogFile = expandPath(cfg.General.LogFile)

	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return cfg, nil
}

// envVarPattern matches ${VAR} and ${VAR:-default} patterns in config strings.
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-(.*?))?\}`)

// ExpandEnvVars replaces ${VAR} with the environment variable value.
// Supports default values: ${VAR:-default} uses "default" when VAR is unset or empty.
func ExpandEnvVars(input string) string {
	return envVarPattern.ReplaceAllStringFunc(input, func(match string) string {
		groups := envVarPattern.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		varName := groups[1]
		defaultVal := ""
		hasDefault := len(groups) >= 3 && groups[2] != ""
		if hasDefault {
			defaultVal = groups[2]
		}

		val, exists := os.LookupEnv(varName)
		if !exists || val == "" {
			if hasDefault {
				return defaultVal
			}
			return match // Keep original if no env var and no default
		}
		return val
	})
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
	var errs []string

	if cfg.General.MaxIterations < 1 || cfg.General.MaxIterations > 200 {
		errs = append(errs, "general.maxIterations must be between 1 and 200")
	}
	if cfg.General.MaxConcurrentMessages < 1 || cfg.General.MaxConcurrentMessages > 100 {
		errs = append(errs, "general.maxConcurrentMessages must be between 1 and 100")
	}
	switch cfg.General.ThinkingLevel {
	case "", "concise", "normal", "detailed":
		// valid
	default:
		errs = append(errs, "general.thinkingLevel must be one of: concise, normal, detailed")
	}

	if cfg.Channels.Web.Port < 0 || cfg.Channels.Web.Port > 65535 {
		errs = append(errs, "channels.web.port must be between 0 and 65535")
	}
	if cfg.API.Port < 0 || cfg.API.Port > 65535 {
		errs = append(errs, "api.port must be between 0 and 65535")
	}

	if cfg.Memory.MaxHistoryPerConversation < 1 {
		errs = append(errs, "memory.maxHistoryPerConversation must be >= 1")
	}
	if cfg.Memory.RetentionDays < 1 {
		errs = append(errs, "memory.retentionDays must be >= 1")
	}
	if cfg.Tools.Shell.Timeout < 1 {
		errs = append(errs, "tools.shell.timeout must be >= 1")
	}
	switch cfg.Security.DefaultPolicy {
	case "allow", "deny", "ask":
		// valid
	default:
		errs = append(errs, "security.defaultPolicy must be one of: allow, deny, ask")
	}

	// Validate failover chain references exist in providers.
	for _, provName := range cfg.General.FailoverChain {
		if _, ok := cfg.Providers[provName]; !ok {
			errs = append(errs, fmt.Sprintf("general.failoverChain references unknown provider: %s", provName))
		}
	}

	// Validate provider configs.
	for name, pc := range cfg.Providers {
		if pc.Enabled && pc.Mode == "api" && pc.APIBase == "" {
			// Skip validation for providers that might have defaults (ollama)
			if name != "ollama" && name != "ollama-cloud" {
				errs = append(errs, fmt.Sprintf("providers.%s: apiBase is required for API mode", name))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation errors:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

func expandPath(path string) string {
	return ExpandPath(path)
}

// ExpandPath resolves ~/ to the user's home directory (used by wizard and Load).
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
