package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"openbot/internal/agent"
	"openbot/internal/bus"
	"openbot/internal/channel"
	"openbot/internal/config"
	"openbot/internal/domain"
	"openbot/internal/memory"
	"openbot/internal/provider"
	"openbot/internal/security"
	"openbot/internal/tool"

	"github.com/spf13/cobra"
)

var (
	version    = "0.2.0"
	logger     *slog.Logger
	configPath string // overridable via --config flag
)

func main() {
	logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	root := &cobra.Command{
		Use:   "openbot",
		Short: "OpenBot: open-source personal AI assistant",
		Long:  "OpenBot is a Go-based AI assistant with Telegram, CLI, and Web interfaces.",
	}

	root.PersistentFlags().StringVarP(&configPath, "config", "c", "", "path to config.json (default: ~/.openbot/config.json)")

	root.AddCommand(initCmd())
	root.AddCommand(chatCmd())
	root.AddCommand(gatewayCmd())
	root.AddCommand(loginCmd())
	root.AddCommand(statusCmd())
	root.AddCommand(configCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize config and workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgDir := config.DefaultConfigDir()
			cfgPath := config.DefaultConfigPath()
			if err := os.MkdirAll(cfgDir, 0o755); err != nil {
				return err
			}
			cfg := config.Defaults()
			if err := config.Save(cfgPath, cfg); err != nil {
				return err
			}
			workspace := cfg.General.Workspace
			if err := os.MkdirAll(workspace, 0o755); err != nil {
				return err
			}
			logger.Info("initialized", "config", cfgPath, "workspace", workspace)
			return nil
		},
	}
}

func chatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Start interactive chat (CLI)",
		RunE:  runChat,
	}
}

// resolveConfigPath returns the config path from --config flag or default.
func resolveConfigPath() string {
	if configPath != "" {
		return configPath
	}
	return config.DefaultConfigPath()
}

func runChat(cmd *cobra.Command, args []string) error {
	cfgPath := resolveConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		logger.Warn("config not found, using defaults", "path", cfgPath, "err", err)
		cfg = config.Defaults()
	}

	if err := os.MkdirAll(cfg.General.Workspace, 0o755); err != nil {
		return err
	}

	// Graceful shutdown on signals (M1 fix)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	messageBus := bus.New(100, logger)

	memStore, err := memory.NewSQLiteStore(cfg.Memory.DBPath, logger)
	if err != nil {
		return err
	}
	defer memStore.Close()
	defer messageBus.Close()

	confirmFn := func(ctx context.Context, question string) (bool, error) {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, question)
		fmt.Fprint(os.Stderr, "Type 'yes' to allow: ")
		var response string
		fmt.Scanln(&response)
		return response == "yes" || response == "y", nil
	}
	secEngine, err := security.NewEngine(cfg.Security, confirmFn, memStore, logger)
	if err != nil {
		return err
	}

	sessions := agent.NewSessionManager(memStore, logger)
	promptBuilder := agent.NewPromptBuilder(cfg.General.Workspace, memStore, logger)

	provFactory := provider.NewFactory(cfg, logger)
	prov, err := provFactory.DefaultProvider()
	if err != nil || prov == nil {
		logger.Warn("no default provider, trying ollama")
		prov = provider.NewOllama(provider.OllamaConfig{Logger: logger})
	}

	toolReg, cronSched := registerTools(cfg, messageBus)

	agentLoop := agent.NewLoop(agent.LoopConfig{
		Provider:      prov,
		Sessions:      sessions,
		Prompt:        promptBuilder,
		Tools:         toolReg,
		Security:      secEngine,
		Bus:           messageBus,
		Logger:        logger,
		MaxIterations: cfg.General.MaxIterations,
	})

	go agentLoop.Run(ctx)

	if cronSched != nil {
		go cronSched.Start(ctx)
	}

	cliCh := channel.NewCLI(channel.CLIConfig{Logger: logger})
	return cliCh.Start(ctx, messageBus)
}

// registerTools creates and registers all tools with the registry.
// Returns the registry and an optional CronScheduler (caller must start it).
func registerTools(cfg *config.Config, messageBus domain.MessageBus) (*tool.Registry, *tool.CronScheduler) {
	toolReg := tool.NewRegistry(logger)
	toolReg.Register(tool.NewShellTool(tool.ShellConfig{
		WorkingDir:          cfg.General.Workspace,
		TimeoutSeconds:      cfg.Tools.Shell.Timeout,
		MaxOutputBytes:      cfg.Tools.Shell.MaxOutputBytes,
		RestrictToWorkspace: cfg.Security.WorkspaceSandbox,
	}))
	toolReg.Register(tool.NewReadFileTool(cfg.General.Workspace))
	toolReg.Register(tool.NewWriteFileTool(cfg.General.Workspace))
	toolReg.Register(tool.NewListDirTool(cfg.General.Workspace))
	toolReg.Register(tool.NewWebSearchTool())
	toolReg.Register(tool.NewWebFetchTool())
	toolReg.Register(tool.NewSysInfoTool())

	toolReg.Register(tool.NewScreenTool(cfg.Tools.Screen.Enabled))

	var cronSched *tool.CronScheduler
	if cfg.Cron.Enabled {
		cronSched = tool.NewCronScheduler(messageBus, logger)
		toolReg.Register(tool.NewCronTool(cronSched))
	}

	return toolReg, cronSched
}

func loginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login [provider]",
		Short: "Open browser to log in to a web-based provider (chatgpt, gemini)",
		Long:  "Opens a visible Chrome window for you to log in. Cookies are saved for later headless use.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provName := args[0]
			cfgPath := resolveConfigPath()
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			provFactory := provider.NewFactory(cfg, logger)
			p, err := provFactory.Get(provName)
			if err != nil || p == nil {
				return fmt.Errorf("unknown or disabled provider: %s", provName)
			}

			// Check if provider supports Login
			type loginable interface {
				Login(context.Context) error
			}
			if l, ok := p.(loginable); ok {
				return l.Login(ctx)
			}
			return fmt.Errorf("provider %s does not support browser login", provName)
		},
	}
	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show system status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := resolveConfigPath()
			cfg, err := config.Load(cfgPath)
			if err != nil {
				logger.Info("config", "path", cfgPath, "loaded", false)
				cfg = config.Defaults()
			} else {
				logger.Info("config", "path", cfgPath, "loaded", true)
			}
			ctx := context.Background()
			factory := provider.NewFactory(cfg, logger)
			prov := factory.HealthyProvider(ctx)
			if prov != nil {
				logger.Info("provider", "name", prov.Name(), "healthy", true)
			} else {
				logger.Info("provider", "healthy", false)
			}
			return nil
		},
	}
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View and modify configuration",
		Long:  "Get, set, and list configuration values. Changes are saved to the config file.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "get [path]",
		Short: "Get a config value (e.g. general.defaultProvider)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := resolveConfigPath()
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			val, err := config.GetByPath(cfg, args[0])
			if err != nil {
				return err
			}
			data, _ := json.MarshalIndent(val, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "set [path] [value]",
		Short: "Set a config value (e.g. general.defaultProvider ollama)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := resolveConfigPath()
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if err := config.SetByPath(cfg, args[0], args[1]); err != nil {
				return fmt.Errorf("set value: %w", err)
			}
			if err := config.Save(cfgPath, cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			logger.Info("config updated", "path", args[0], "value", args[1], "file", cfgPath)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all config values",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := resolveConfigPath()
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			sanitized := config.Sanitize(cfg)
			data, _ := json.MarshalIndent(sanitized, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Show config file path",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(resolveConfigPath())
		},
	})

	return cmd
}

func gatewayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gateway",
		Short: "Start gateway (Telegram + Web + Agent loop)",
		Long:  "Starts all enabled channels (Telegram, Web) and the agent loop. Press Ctrl+C to stop.",
		RunE:  runGateway,
	}
}

func runGateway(cmd *cobra.Command, args []string) error {
	cfgPath := resolveConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Ensure workspace exists
	if err := os.MkdirAll(cfg.General.Workspace, 0o755); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Message bus (closed during graceful shutdown below)
	messageBus := bus.New(100, logger)

	// Memory store
	memStore, err := memory.NewSQLiteStore(cfg.Memory.DBPath, logger)
	if err != nil {
		return fmt.Errorf("memory store: %w", err)
	}
	defer memStore.Close()

	provFactory := provider.NewFactory(cfg, logger)
	prov, err := provFactory.DefaultProvider()
	if err != nil || prov == nil {
		logger.Warn("no default provider, falling back to ollama")
		prov = provider.NewOllama(provider.OllamaConfig{Logger: logger})
	}
	if err := prov.Healthy(ctx); err != nil {
		logger.Warn("default provider unhealthy at startup", "provider", prov.Name(), "err", err)
	} else {
		logger.Info("provider healthy", "provider", prov.Name())
	}

	var telegramCh *channel.Telegram
	confirmFn := func(ctx2 context.Context, question string) (bool, error) {
		// Route confirmation through Telegram if available and user has allowFrom set
		if telegramCh != nil && len(cfg.Channels.Telegram.AllowFrom) > 0 {
			chatIDStr := cfg.Channels.Telegram.AllowFrom[0]
			chatID, _ := strconv.ParseInt(chatIDStr, 10, 64)
			if chatID != 0 {
				return telegramCh.RequestConfirmation(ctx2, chatID, question)
			}
		}
		// No Telegram available — deny
		return false, nil
	}
	secEngine, err := security.NewEngine(cfg.Security, confirmFn, memStore, logger)
	if err != nil {
		return fmt.Errorf("security engine: %w", err)
	}

	sessions := agent.NewSessionManager(memStore, logger)
	promptBuilder := agent.NewPromptBuilder(cfg.General.Workspace, memStore, logger)

	toolReg, cronSched := registerTools(cfg, messageBus)

	agentLoop := agent.NewLoop(agent.LoopConfig{
		Provider:      prov,
		Sessions:      sessions,
		Prompt:        promptBuilder,
		Tools:         toolReg,
		Security:      secEngine,
		Bus:           messageBus,
		Logger:        logger,
		MaxIterations: cfg.General.MaxIterations,
	})

	go agentLoop.Run(ctx)

	if cronSched != nil {
		go cronSched.Start(ctx)
	}

	if cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.Token != "" {
		telegramCh = channel.NewTelegram(channel.TelegramConfig{
			Token:     cfg.Channels.Telegram.Token,
			AllowFrom: cfg.Channels.Telegram.AllowFrom,
			ParseMode: cfg.Channels.Telegram.ParseMode,
			Logger:    logger,
		})
		go func() {
			if err := telegramCh.Start(ctx, messageBus); err != nil {
				logger.Error("telegram channel error", "err", err)
			}
		}()
		logger.Info("telegram channel enabled")
	} else {
		logger.Info("telegram channel disabled")
	}

	var webCh *channel.Web
	if cfg.Channels.Web.Enabled {
		webCh = channel.NewWeb(channel.WebConfig{
			Host:       cfg.Channels.Web.Host,
			Port:       cfg.Channels.Web.Port,
			Logger:     logger,
			Config:     cfg,
			ConfigPath: cfgPath,
			Version:    version,
			Store:      memStore,
		})
		go func() {
			if err := webCh.Start(ctx, messageBus); err != nil {
				logger.Error("web channel error", "err", err)
			}
		}()
	}

	logger.Info("gateway started. Press Ctrl+C to stop.")

	// Block until shutdown signal
	<-ctx.Done()
	logger.Info("shutting down gateway...")

	// Graceful shutdown with timeout
	const shutdownTimeout = 10 * time.Second
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	var shutdownErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		if telegramCh != nil {
			telegramCh.Stop()
		}
		if webCh != nil {
			webCh.Stop()
		}
		messageBus.Close()
	}()

	select {
	case <-done:
		logger.Info("shutdown complete")
	case <-shutdownCtx.Done():
		logger.Warn("shutdown timed out, forcing exit")
		shutdownErr = fmt.Errorf("shutdown timed out")
	}

	return shutdownErr
}
