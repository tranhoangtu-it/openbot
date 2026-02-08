package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"openbot/internal/config"

	"github.com/spf13/cobra"
)

// providerMeta describes a provider option for the wizard.
type providerMeta struct {
	Name      string
	NeedsKey  bool
	EnvVar    string
	APIBase   string
	DefaultModel string
}

var knownProviders = []providerMeta{
	{Name: "ollama", NeedsKey: false, APIBase: "http://localhost:11434", DefaultModel: "llama3.1:8b"},
	{Name: "openai", NeedsKey: true, EnvVar: "OPENAI_API_KEY", APIBase: "https://api.openai.com/v1", DefaultModel: "gpt-4o"},
	{Name: "claude", NeedsKey: true, EnvVar: "ANTHROPIC_API_KEY", APIBase: "https://api.anthropic.com", DefaultModel: "claude-sonnet-4-20250514"},
	{Name: "deepseek", NeedsKey: true, EnvVar: "DEEPSEEK_API_KEY", APIBase: "https://api.deepseek.com/v1", DefaultModel: "deepseek-chat"},
	{Name: "openrouter", NeedsKey: true, EnvVar: "OPENROUTER_API_KEY", APIBase: "https://openrouter.ai/api/v1", DefaultModel: "openai/gpt-4o"},
	{Name: "groq", NeedsKey: true, EnvVar: "GROQ_API_KEY", APIBase: "https://api.groq.com/openai/v1", DefaultModel: "llama-3.3-70b-versatile"},
}

var knownChannels = []struct {
	ID   string
	Desc string
}{{"cli", "Interactive terminal chat"}, {"web", "Web UI (browser)"}, {"telegram", "Telegram bot"}}

func wizardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "wizard",
		Short: "Interactive setup: workspace → provider → channel → save config",
		Long:  "Guides you through workspace path, default LLM provider (and API key if needed), and channel (CLI/Web/Telegram). Writes config to the path used by --config or default.",
		RunE:  runWizard,
	}
}

func runWizard(cmd *cobra.Command, args []string) error {
	cfgPath := resolveConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.Defaults()
	}
	ensureProvidersFromExample(cfg)

	reader := bufio.NewReader(os.Stdin)
	prompt := func(def string) (string, error) {
		if def != "" {
			fmt.Fprintf(os.Stdout, " [%s]: ", def)
		} else {
			fmt.Fprint(os.Stdout, ": ")
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		s := strings.TrimSpace(line)
		if s == "" && def != "" {
			return def, nil
		}
		return s, nil
	}

	// Step 1: Workspace
	fmt.Println("\n--- Step 1: Workspace ---")
	workspace := cfg.General.Workspace
	if workspace == "" {
		workspace = "~/.openbot/workspace"
	}
	fmt.Fprintf(os.Stdout, "Directory for bot data (conversations, etc.)")
	ws, err := prompt(workspace)
	if err != nil {
		return err
	}
	if ws != "" {
		cfg.General.Workspace = ws
	}
	cfg.General.Workspace = config.ExpandPath(cfg.General.Workspace)
	if err := os.MkdirAll(cfg.General.Workspace, 0o755); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	fmt.Fprintf(os.Stdout, "  Using workspace: %s\n", cfg.General.Workspace)

	// Step 2: Provider
	fmt.Println("\n--- Step 2: Default LLM provider ---")
	for i, p := range knownProviders {
		fmt.Fprintf(os.Stdout, "  %d) %s", i+1, p.Name)
		if p.NeedsKey {
			fmt.Fprintf(os.Stdout, " (set %s)", p.EnvVar)
		}
		fmt.Println()
	}
	fmt.Fprint(os.Stdout, "Choose provider (1–"+fmt.Sprint(len(knownProviders))+")")
	defNum := "1"
	for i, p := range knownProviders {
		if p.Name == cfg.General.DefaultProvider {
			defNum = fmt.Sprint(i + 1)
			break
		}
	}
	choice, err := prompt(defNum)
	if err != nil {
		return err
	}
	var idx int
	if n, _ := fmt.Sscanf(choice, "%d", &idx); n != 1 || idx < 1 || idx > len(knownProviders) {
		idx = 1
	}
	prov := knownProviders[idx-1]
	cfg.General.DefaultProvider = prov.Name
	// Ensure this provider exists and is enabled
	if _, ok := cfg.Providers[prov.Name]; !ok {
		cfg.Providers[prov.Name] = config.ProviderConfig{
			Enabled:      true,
			Mode:         "api",
			APIBase:      prov.APIBase,
			DefaultModel: prov.DefaultModel,
		}
	} else {
		p := cfg.Providers[prov.Name]
		p.Enabled = true
		p.APIBase = prov.APIBase
		if p.DefaultModel == "" {
			p.DefaultModel = prov.DefaultModel
		}
		cfg.Providers[prov.Name] = p
	}
	if prov.NeedsKey {
		fmt.Fprintf(os.Stdout, "API key: paste key or env var (e.g. ${%s})", prov.EnvVar)
		key, err := prompt("${" + prov.EnvVar + "}")
		if err != nil {
			return err
		}
		if key != "" {
			p := cfg.Providers[prov.Name]
			p.APIKey = key
			cfg.Providers[prov.Name] = p
		}
	}
	// Disable others as default
	for name := range cfg.Providers {
		if name != prov.Name {
			p := cfg.Providers[name]
			p.Enabled = false
			cfg.Providers[name] = p
		}
	}
	fmt.Fprintf(os.Stdout, "  Using provider: %s\n", prov.Name)

	// Step 3: Channel
	fmt.Println("\n--- Step 3: Channel ---")
	for i, c := range knownChannels {
		fmt.Fprintf(os.Stdout, "  %d) %s — %s\n", i+1, c.ID, c.Desc)
	}
	fmt.Fprint(os.Stdout, "Choose channel (1–3)")
	chChoice, err := prompt("1")
	if err != nil {
		return err
	}
	var chIdx int
	if n, _ := fmt.Sscanf(chChoice, "%d", &chIdx); n != 1 || chIdx < 1 || chIdx > len(knownChannels) {
		chIdx = 1
	}
	chID := knownChannels[chIdx-1].ID
	cfg.Channels.CLI.Enabled = chID == "cli"
	cfg.Channels.Web.Enabled = chID == "web"
	if chID == "telegram" {
		cfg.Channels.Telegram.Enabled = true
		fmt.Fprint(os.Stdout, "Telegram bot token (from @BotFather)")
		tok, err := prompt("")
		if err != nil {
			return err
		}
		if tok != "" {
			cfg.Channels.Telegram.Token = tok
		}
	} else {
		cfg.Channels.Telegram.Enabled = false
	}
	fmt.Fprintf(os.Stdout, "  Using channel: %s\n", chID)

	// Save
	cfgDir := filepath.Dir(cfgPath)
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "\nConfig saved to %s\n", cfgPath)
	fmt.Println("Next: run 'openbot chat' for CLI, or 'openbot gateway' for Web/API.")
	return nil
}

// ensureProvidersFromExample adds common providers from example so SetByPath has keys.
func ensureProvidersFromExample(cfg *config.Config) {
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]config.ProviderConfig)
	}
	for _, p := range knownProviders {
		if _, ok := cfg.Providers[p.Name]; !ok {
			cfg.Providers[p.Name] = config.ProviderConfig{
				Enabled:      p.Name == cfg.General.DefaultProvider || p.Name == "ollama",
				Mode:         "api",
				APIBase:      p.APIBase,
				APIKey:       "",
				DefaultModel: p.DefaultModel,
			}
		}
	}
}
