package config

func Defaults() *Config {
	return &Config{
		General: GeneralConfig{
			Workspace:             "~/.openbot/workspace",
			LogLevel:              "info",
			MaxIterations:         20,
			DefaultProvider:       "ollama",
			MaxConcurrentMessages: 5,
		},
		Providers: map[string]ProviderConfig{
			"ollama": {
				Enabled:      true,
				Mode:         "api",
				APIBase:      "http://localhost:11434",
				DefaultModel: "llama3.1:8b",
			},
		},
		Channels: ChannelsConfig{
			Telegram: TelegramConfig{
				Enabled:   false,
				ParseMode: "Markdown",
			},
			Web: WebConfig{
				Enabled: false,
				Host:    "127.0.0.1",
				Port:    8080,
			},
			CLI: CLIConfig{
				Enabled: true,
			},
			WhatsApp: WhatsAppConfig{
				Enabled:     false,
				WebhookPath: "/webhook/whatsapp",
			},
		},
		Memory: MemoryConfig{
			Enabled:                   true,
			DBPath:                    "~/.openbot/memory.db",
			MaxHistoryPerConversation: 100,
			RetentionDays:             365,
		},
		Security: SecurityConfig{
			DefaultPolicy:         "ask",
			WorkspaceSandbox:      false,
			Blacklist:             defaultBlacklist(),
			Whitelist:             defaultWhitelist(),
			ConfirmPatterns:       defaultConfirmPatterns(),
			ConfirmTimeoutSeconds: 60,
			AuditLog:              true,
		},
		Tools: ToolsConfig{
			Shell: ShellToolConfig{
				Timeout:        30,
				MaxOutputBytes: 65536,
			},
			Screen: ScreenToolConfig{
				Enabled: false,
			},
			Web: WebToolConfig{
				SearchProvider: "duckduckgo",
			},
		},
		Cron: CronConfig{
			Enabled: true,
		},
		Agents: AgentsConfig{
			Enabled:        false,
			Mode:           "single",
			RouterStrategy: "keyword",
		},
		Knowledge: KnowledgeConfig{
			Enabled:      false,
			MaxDocuments: 100,
			ChunkSize:    512,
			ChunkOverlap: 50,
			SearchTopK:   5,
		},
		Metrics: MetricsConfig{
			Enabled:       false,
			Endpoint:      "/metrics",
			RetentionDays: 30,
		},
		API: APIConfig{
			Enabled: false,
			Port:    9090,
		},
		MCP: MCPConfig{
			Enabled: false,
			Servers: nil,
		},
	}
}

func defaultBlacklist() []string {
	return []string{
		"rm -rf /",
		"rm -rf /*",
		"mkfs",
		"dd if=",
		":(){:|:&};:",
		"chmod -R 777 /",
		">(w)/dev/sda",
		"mv /* /dev/null",
	}
}

func defaultWhitelist() []string {
	return []string{
		"ls", "cat", "echo", "pwd", "date", "whoami",
		"git status", "git log", "git diff", "git branch",
		"go version", "go env", "python --version",
		"uname", "uptime", "df -h", "free -h",
	}
}

func defaultConfirmPatterns() []string {
	return []string{
		"rm ", "sudo ", "kill ", "killall ",
		"shutdown", "reboot", "halt",
		"chmod ", "chown ",
		"mv /", "cp /",
		"apt ", "apt-get ", "brew ",
		"pip install", "npm install -g",
		"systemctl ", "launchctl ",
	}
}
