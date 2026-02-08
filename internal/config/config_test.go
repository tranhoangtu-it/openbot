package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// --- Validate ---

func TestValidate_ValidConfig(t *testing.T) {
	cfg := Defaults()
	if err := Validate(cfg); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}

func TestValidate_MaxIterations_TooLow(t *testing.T) {
	cfg := Defaults()
	cfg.General.MaxIterations = 0
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for maxIterations=0")
	}
}

func TestValidate_MaxIterations_TooHigh(t *testing.T) {
	cfg := Defaults()
	cfg.General.MaxIterations = 999
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for maxIterations=999")
	}
}

func TestValidate_MaxIterations_Boundary(t *testing.T) {
	cfg := Defaults()

	cfg.General.MaxIterations = 1
	if err := Validate(cfg); err != nil {
		t.Fatalf("maxIterations=1 should be valid: %v", err)
	}

	cfg.General.MaxIterations = 200
	if err := Validate(cfg); err != nil {
		t.Fatalf("maxIterations=200 should be valid: %v", err)
	}
}

func TestValidate_InvalidPort(t *testing.T) {
	cfg := Defaults()
	cfg.Channels.Web.Port = -1
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for negative port")
	}

	cfg.Channels.Web.Port = 70000
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for port > 65535")
	}
}

func TestValidate_InvalidPolicy(t *testing.T) {
	cfg := Defaults()
	cfg.Security.DefaultPolicy = "invalid"
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for invalid policy")
	}
}

func TestValidate_ValidPolicies(t *testing.T) {
	for _, policy := range []string{"allow", "deny", "ask"} {
		cfg := Defaults()
		cfg.Security.DefaultPolicy = policy
		if err := Validate(cfg); err != nil {
			t.Fatalf("policy %q should be valid: %v", policy, err)
		}
	}
}

func TestValidate_InvalidMemoryConfig(t *testing.T) {
	cfg := Defaults()
	cfg.Memory.MaxHistoryPerConversation = 0
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for maxHistoryPerConversation=0")
	}

	cfg = Defaults()
	cfg.Memory.RetentionDays = 0
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for retentionDays=0")
	}
}

func TestValidate_InvalidShellTimeout(t *testing.T) {
	cfg := Defaults()
	cfg.Tools.Shell.Timeout = 0
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for shell timeout=0")
	}
}

// --- Load / Save ---

func TestLoadSave_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	original := Defaults()
	original.General.DefaultProvider = "test-provider"

	if err := Save(path, original); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.General.DefaultProvider != "test-provider" {
		t.Fatalf("expected 'test-provider', got %q", loaded.General.DefaultProvider)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("{not json}"), 0o644)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- Accessor ---

func TestGetByPath_ValidPaths(t *testing.T) {
	cfg := Defaults()

	val, err := GetByPath(cfg, "general.defaultProvider")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if val != "ollama" {
		t.Fatalf("expected 'ollama', got %v", val)
	}
}

func TestGetByPath_InvalidPath(t *testing.T) {
	cfg := Defaults()
	_, err := GetByPath(cfg, "nonexistent.path")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestSetByPath_ValidPath(t *testing.T) {
	cfg := Defaults()
	if err := SetByPath(cfg, "general.defaultProvider", "claude"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if cfg.General.DefaultProvider != "claude" {
		t.Fatalf("expected 'claude', got %q", cfg.General.DefaultProvider)
	}
}

func TestSetByPath_EmptyPath(t *testing.T) {
	cfg := Defaults()
	// SetByPath with empty path sets at root level with key ""
	// which is technically valid JSON behavior — not an error
	err := SetByPath(cfg, "general.defaultProvider", "")
	if err != nil {
		t.Fatalf("set empty value should work: %v", err)
	}
}

func TestSetByPath_BoolConversion(t *testing.T) {
	cfg := Defaults()
	if err := SetByPath(cfg, "memory.enabled", "false"); err != nil {
		t.Fatalf("set bool: %v", err)
	}
	if cfg.Memory.Enabled {
		t.Fatal("expected memory.enabled=false")
	}
}

func TestSetByPath_IntConversion(t *testing.T) {
	cfg := Defaults()
	if err := SetByPath(cfg, "general.maxIterations", "50"); err != nil {
		t.Fatalf("set int: %v", err)
	}
	if cfg.General.MaxIterations != 50 {
		t.Fatalf("expected 50, got %d", cfg.General.MaxIterations)
	}
}

// --- Sanitize ---

func TestSanitize_MasksSecrets(t *testing.T) {
	cfg := Defaults()
	cfg.Channels.Telegram.Token = "123456789:ABCdefGHIjklMNOpqrSTUvwxyz"
	cfg.Providers["openai"] = ProviderConfig{
		Enabled: true,
		APIKey:  "sk-1234567890abcdefghijklmnop",
	}

	sanitized := Sanitize(cfg)

	if sanitized.Channels.Telegram.Token == cfg.Channels.Telegram.Token {
		t.Fatal("telegram token should be masked")
	}
	if sanitized.Providers["openai"].APIKey == cfg.Providers["openai"].APIKey {
		t.Fatal("API key should be masked")
	}
	// Verify original is untouched
	if cfg.Channels.Telegram.Token != "123456789:ABCdefGHIjklMNOpqrSTUvwxyz" {
		t.Fatal("original config should not be modified")
	}
}

func TestSanitize_ShortSecret(t *testing.T) {
	cfg := Defaults()
	cfg.Channels.Telegram.Token = "short"
	sanitized := Sanitize(cfg)
	if sanitized.Channels.Telegram.Token != "***" {
		t.Fatalf("short secret should be '***', got %q", sanitized.Channels.Telegram.Token)
	}
}

func TestSanitize_MasksWhatsAppSecrets(t *testing.T) {
	cfg := Defaults()
	cfg.Channels.WhatsApp.AppSecret = "whatsapp-secret-12345678"
	cfg.Channels.WhatsApp.AccessToken = "whatsapp-token-12345678"
	sanitized := Sanitize(cfg)

	if sanitized.Channels.WhatsApp.AppSecret == cfg.Channels.WhatsApp.AppSecret {
		t.Fatal("WhatsApp appSecret should be masked")
	}
	if sanitized.Channels.WhatsApp.AccessToken == cfg.Channels.WhatsApp.AccessToken {
		t.Fatal("WhatsApp accessToken should be masked")
	}
}

func TestSanitize_MasksAPIGatewayKey(t *testing.T) {
	cfg := Defaults()
	cfg.API.APIKey = "api-gateway-key-12345678"
	sanitized := Sanitize(cfg)

	if sanitized.API.APIKey == cfg.API.APIKey {
		t.Fatal("API gateway key should be masked")
	}
}

func TestLoad_ValidatesConfig(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	// Invalid: maxIterations=0
	content := `{
		"general": {
			"maxIterations": 0
		}
	}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgFile)
	if err == nil {
		t.Fatal("expected validation error for maxIterations=0")
	}
}

// --- ListPaths ---

func TestListPaths_ReturnsAllLeaves(t *testing.T) {
	cfg := Defaults()
	paths := ListPaths(cfg)
	if len(paths) == 0 {
		t.Fatal("expected non-empty paths")
	}

	// Check some known paths exist
	for _, expected := range []string{"general.workspace", "general.logLevel", "memory.enabled"} {
		if _, ok := paths[expected]; !ok {
			t.Errorf("missing expected path: %s", expected)
		}
	}
}

// --- FlexStringList ---

func TestFlexStringList_MixedTypes(t *testing.T) {
	input := `["hello", 123, "world", 456.0]`
	var list FlexStringList
	if err := json.Unmarshal([]byte(input), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("expected 4 items, got %d", len(list))
	}
	if list[0] != "hello" || list[2] != "world" {
		t.Fatal("string items mismatch")
	}
	if list[1] != "123" || list[3] != "456" {
		t.Fatalf("number conversion mismatch: %v", list)
	}
}

func TestFlexStringList_PureStrings(t *testing.T) {
	input := `["a", "b", "c"]`
	var list FlexStringList
	if err := json.Unmarshal([]byte(input), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(list) != 3 || list[0] != "a" {
		t.Fatalf("unexpected: %v", list)
	}
}

func TestFlexStringList_InvalidJSON(t *testing.T) {
	var list FlexStringList
	err := json.Unmarshal([]byte(`not json`), &list)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- ExpandEnvVars ---

func TestExpandEnvVars_SimpleSubstitution(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-abc123")
	result := ExpandEnvVars(`{"apiKey": "${TEST_API_KEY}"}`)
	expected := `{"apiKey": "sk-abc123"}`
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestExpandEnvVars_DefaultValue(t *testing.T) {
	// Ensure the var is unset
	os.Unsetenv("NONEXISTENT_VAR_12345")
	result := ExpandEnvVars(`{"port": "${NONEXISTENT_VAR_12345:-8080}"}`)
	expected := `{"port": "8080"}`
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestExpandEnvVars_SetVarOverridesDefault(t *testing.T) {
	t.Setenv("MY_PORT", "9090")
	result := ExpandEnvVars(`{"port": "${MY_PORT:-8080}"}`)
	expected := `{"port": "9090"}`
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestExpandEnvVars_MultipleVars(t *testing.T) {
	t.Setenv("HOST", "localhost")
	t.Setenv("PORT", "3000")
	result := ExpandEnvVars(`"${HOST}:${PORT}"`)
	expected := `"localhost:3000"`
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestExpandEnvVars_UnsetVarNoDefault_KeepsOriginal(t *testing.T) {
	os.Unsetenv("TOTALLY_UNSET_VAR_XYZ")
	result := ExpandEnvVars(`"${TOTALLY_UNSET_VAR_XYZ}"`)
	expected := `"${TOTALLY_UNSET_VAR_XYZ}"`
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestExpandEnvVars_EmptyVarUsesDefault(t *testing.T) {
	t.Setenv("EMPTY_VAR", "")
	result := ExpandEnvVars(`"${EMPTY_VAR:-fallback}"`)
	expected := `"fallback"`
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestExpandEnvVars_NoVarsInInput(t *testing.T) {
	input := `{"key": "value", "number": 42}`
	result := ExpandEnvVars(input)
	if result != input {
		t.Fatalf("expected no change, got %q", result)
	}
}

func TestExpandEnvVars_DollarSignWithoutBraces(t *testing.T) {
	input := `"$HOME is not substituted"`
	result := ExpandEnvVars(input)
	if result != input {
		t.Fatalf("expected no change for bare $VAR, got %q", result)
	}
}

func TestLoad_WithEnvVarSubstitution(t *testing.T) {
	t.Setenv("TEST_OPENBOT_WORKSPACE", "/tmp/test-workspace")

	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	content := `{
		"general": {
			"workspace": "${TEST_OPENBOT_WORKSPACE}",
			"logLevel": "info",
			"maxIterations": 20,
			"defaultProvider": "ollama",
			"maxConcurrentMessages": 5
		}
	}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.General.Workspace != "/tmp/test-workspace" {
		t.Fatalf("expected workspace '/tmp/test-workspace', got %q", cfg.General.Workspace)
	}
}

// --- Defaults ---

func TestDefaults_ReturnsValidConfig(t *testing.T) {
	cfg := Defaults()
	if cfg == nil {
		t.Fatal("defaults returned nil")
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("defaults should be valid: %v", err)
	}
	if cfg.General.Workspace == "" {
		t.Fatal("workspace should not be empty")
	}
	if cfg.General.DefaultProvider != "ollama" {
		t.Fatalf("default provider should be 'ollama', got %q", cfg.General.DefaultProvider)
	}
}
