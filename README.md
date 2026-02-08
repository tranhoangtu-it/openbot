<p align="center">
  <img src="assets/logo.png" alt="OpenBot Logo" width="200"/>
</p>

<h1 align="center">OpenBot</h1>

<p align="center">
  <strong>Open-Source Personal AI Assistant</strong><br/>
  <em>The AI that runs on your machine — multi-LLM, multi-channel, tool-augmented, your data stays yours.</em>
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> &bull;
  <a href="docs/projects/README.md">Doc hub</a> &bull;
  <a href="#configuration">Configuration</a> &bull;
  <a href="#cli-commands">CLI</a> &bull;
  <a href="#documentation">Documentation</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white" alt="Go"/>
  <img src="https://img.shields.io/badge/License-MIT-green" alt="License"/>
  <img src="https://img.shields.io/badge/Platform-macOS%20%7C%20Linux%20%7C%20Docker-lightgrey" alt="Platform"/>
  <img src="https://img.shields.io/badge/Version-0.2.0-blue" alt="Version"/>
  <img src="https://img.shields.io/badge/Tests-269%20Go%20%2B%2019%20E2E-brightgreen" alt="Tests"/>
</p>

---

## Overview

OpenBot is a self-hosted AI assistant written entirely in Go. It connects to multiple LLM providers (with **token-by-token streaming**), exposes five user-facing channels (CLI, Telegram, Web UI, WhatsApp, API Gateway), and lets the agent interact with your system through a pluggable tool and skills system — all while enforcing security policies on every action.

**Key design goals**: single binary deployment, zero JavaScript frameworks (vendored assets), persistent memory with RAG knowledge engine, and full control over your data.

---

## What's New in v0.2.0

### Features
- **Token-by-token streaming** — OpenAI and Claude providers stream responses in real-time via SSE
- **Conversation sidebar** — Browse, search, and manage multiple conversations in the Web UI
- **Dark mode** — System-aware dark/light theme across all pages
- **Provider switching** — Select LLM provider per message
- **Parallel tool execution** — Multiple tool calls run concurrently (up to 5)
- **WhatsApp channel** — Cloud API integration with webhook signature verification
- **Slack & Discord** — Socket Mode (Slack), bot token + optional guild (Discord); see [channels-slack-discord](docs/projects/presales/channels-slack-discord.md)
- **API Gateway** — OpenAI-compatible `/v1/chat/completions` endpoint
- **MCP (Model Context Protocol)** — Connect MCP servers; tools appear as `mcp_<server>_<name>` in the agent
- **Per-session token cap (R5)** — `maxTokensPerSession` and `tokenBudgetAlert` to control cost and usage
- **Onboarding wizard** — `openbot wizard` for interactive setup (workspace → provider → channel)
- **Skills system** — Reusable workflows (built-in + user YAML definitions)
- **Knowledge engine (RAG)** — Document upload, FTS5 chunked search, context injection
- **Multi-agent router** — Keyword-based routing to specialized agent profiles
- **Event system** — Internal pub/sub for cross-component communication
- **Prometheus metrics** — Built-in `/metrics` endpoint
- **Vendored assets** — Tailwind, marked.js, highlight.js, htmx bundled in binary (no CDN)
- **CLI ops** — `openbot doctor` (diagnostics), `openbot backup` / `restore`, `openbot install-daemon` / `uninstall-daemon`

### Performance & Hardening
- **Singleton HTTP client** — All LLM providers share one connection pool via `sync.Once` (100 idle conns, HTTP/2)
- **O(n) streaming** — `strings.Builder` replaces `O(n^2)` string concatenation during LLM streaming
- **Pre-compiled patterns** — Skill registry caches compiled regexes and lowercase keywords at registration
- **Pre-computed routing** — Agent router pre-lowers keywords once, not per-message
- **Prompt cache cleanup** — Background goroutine evicts expired `sync.Map` entries every 2 minutes
- **Retry with jitter** — Exponential backoff with randomized jitter prevents thundering herd
- **SQLite read/write split** — Separate reader pool (4 connections) for concurrent reads
- **Prompt caching** — 60s TTL cache for system prompts

### Security & Stability
- **HTTP server hardening** — `ReadHeaderTimeout`, `ReadTimeout`, `IdleTimeout`, `MaxHeaderBytes` on all servers
- **Request body limits** — API Gateway enforces 1MB max body via `io.LimitReader`
- **TOCTOU race fix** — Provider factory uses double-check locking for thread-safe singleton creation
- **Proper mutex** — API Gateway uses `sync.Mutex` instead of channel-based mutex for better performance

### Code Quality
- **DRY providers** — Shared helper functions for OpenAI/Claude message and tool conversion (~100 lines deduplication)
- **Standard library** — Custom `contains()` replaced with `strings.Contains`
- **Error observability** — Previously-ignored database errors now logged with warnings

---

## Features

### Channels (Interfaces)

| Channel | Setup | Description |
|---------|-------|-------------|
| **CLI** | Easy | `./build/openbot chat` — no extra config. [Quick Start](#quick-start) |
| **Telegram Bot** | Easy | Set `channels.telegram.token` and `allowFrom`. [Configuration](#configuration) |
| **Web UI** | Easy | Set `channels.web.enabled: true`; optional auth in `channels.web.auth`. [Configuration](#configuration) |
| **WhatsApp** | Medium | Cloud API: `appId`, `appSecret`, `accessToken`, webhook. [Configuration](#configuration) |
| **Slack** | Medium | Socket Mode: `channels.slack.botToken`, `appToken`. [Slack & Discord setup](docs/projects/presales/channels-slack-discord.md) |
| **Discord** | Easy | `channels.discord.token`, optional `guildId`. [Slack & Discord setup](docs/projects/presales/channels-slack-discord.md) |
| **API Gateway** | Easy | Set `api.enabled: true`, `api.port`, `api.apiKey`. [Configuration](#configuration) |

### LLM Providers

| Provider | Mode | Streaming | Tool Calling | Notes |
|----------|------|:---------:|:---:|-------|
| **Ollama** | API | Yes | Yes | Local/cloud, exponential backoff retry with jitter |
| **OpenAI** | API | **Yes** | Yes | GPT-4o, GPT-4.1, token-by-token streaming |
| **Claude** | API | **Yes** | Yes | Claude Sonnet/Opus/Haiku, SSE streaming |
| **ChatGPT Web** | Browser | No | No | Via headless Chrome |
| **Gemini Web** | Browser | No | No | Via headless Chrome |

### Tools (Agent Capabilities)

| Tool | Description |
|------|-------------|
| `shell` | Execute shell commands (with security checks) |
| `read_file` | Read file contents (workspace-sandboxed) |
| `write_file` | Write/create files (workspace-sandboxed) |
| `list_dir` | List directory contents |
| `web_search` | Search the web via DuckDuckGo |
| `web_fetch` | Fetch and extract content from any URL (SSRF-protected) |
| `system_info` | Detailed system info — CPU, RAM, GPU, Disk, OS, network |
| `screen` | Screen control — mouse, keyboard, screenshots (robotgo) |
| `cron` | Create, list, remove scheduled tasks at runtime |
| **MCP tools** | Tools from [MCP](https://modelcontextprotocol.io) servers (config `mcp.enabled`, `mcp.servers`); names prefixed `mcp_<server>_<name>` |

### Security Engine

- **Blacklist**: Dangerous commands are always blocked
- **Whitelist**: Safe commands are always allowed
- **Confirm patterns**: Risky commands require user confirmation
- **Multi-tool coverage**: Security checks on shell, file write, and web fetch
- **Audit logging**: Every tool execution is logged
- **Workspace sandbox**: File tools enforce path boundaries
- **Web UI auth**: Optional HTTP Basic Auth
- **Request limits**: Body size limits on API Gateway (1MB)
- **Server hardening**: Timeouts on all HTTP servers to prevent slowloris attacks

### Persistent Memory & Knowledge

- **SQLite-backed** with read/write connection splitting (4 reader pool)
- Per-conversation message history with provider/model/latency tracking
- **Knowledge engine (RAG)**: Upload documents → chunked FTS5 search → context injection
- Long-term memory entries with TTL
- Auto-generated conversation titles

### Skills System

- **Built-in skills**: system_health, code_review, research
- **User-defined skills**: YAML files in `~/.openbot/skills/`
- Keyword and regex pattern matching (pre-compiled at registration)
- Multi-step workflows: tool → LLM → transform

### Observability

- **Prometheus-compatible `/metrics`** endpoint (no heavy dependencies)
- Counters: messages, LLM requests, tool executions, security blocks
- Histograms: LLM latency, tool latency
- Gauges: active sessions, SSE connections

---

## Quick Start

**TL;DR — 3 steps:**

```bash
# 1. Build (Go 1.25+)
make build

# 2. Initialize config and workspace
./build/openbot init

# 3. Chat (CLI) or start full gateway (Web + channels)
./build/openbot chat
# or: ./build/openbot gateway
```

Alternatively, run **`./build/openbot wizard`** for interactive setup (workspace → provider → channel); config is written automatically.

### CLI commands {#cli-commands}

| Command | Description |
|---------|-------------|
| `openbot init` | Create default config and workspace |
| `openbot chat` | Interactive CLI chat (single channel) |
| `openbot gateway` | Start all channels + agent (Web, Telegram, Slack, Discord, API, etc.) |
| `openbot wizard` | Interactive onboarding (workspace → provider → channel) |
| `openbot config get/set/list/path` | View or change config |
| `openbot status` | Show provider health |
| `openbot login [provider]` | Open browser to log in (e.g. ChatGPT Web, Gemini) |
| `openbot backup` | Backup memory DB and config |
| `openbot restore` | Restore from backup |
| `openbot doctor` | Run diagnostics (config, workspace, provider, memory) |
| `openbot install-daemon` | Install as a system service (launchd/systemd) |
| `openbot uninstall-daemon` | Remove daemon installation |

<details>
<summary>Full steps (clone, build, init, run)</summary>

```bash
# Clone the repository
git clone https://github.com/your-org/openbot.git
cd openbot

# Build (uses Go 1.25+)
make build

# Initialize config and workspace
./build/openbot init

# Start interactive CLI chat (requires Ollama running locally)
./build/openbot chat

# Or start the full gateway (all channels + agent)
./build/openbot gateway
```
</details>

### Prerequisites

- **Go 1.25+**
- **[Ollama](https://ollama.com)** for local LLM (recommended to start)
- **Chrome/Chromium** (optional, only for ChatGPT Web / Gemini Web providers)
- **Docker** (optional, for containerized deployment)

---

## Configuration

Default path: `~/.openbot/config.json`

Use `config.example.json` as a template with all available options.

```bash
# Initialize default config
./build/openbot init

# View current config
./build/openbot config list

# Change a setting
./build/openbot config set channels.web.enabled true
```

### Full Config Structure

```jsonc
{
  "general": {
    "workspace": "~/.openbot/workspace",
    "logLevel": "info",
    "maxIterations": 20,
    "defaultProvider": "ollama",
    "maxConcurrentMessages": 5,        // parallel message processing
    "maxTokensPerSession": 0,          // 0=off; per-conversation token cap (R5)
    "tokenBudgetAlert": 0              // 0=off; log warning when session reaches this
  },
  "providers": {
    "ollama": {
      "enabled": true,
      "mode": "api",
      "apiBase": "http://localhost:11434",
      "defaultModel": "llama3.1:8b"
    },
    "openai": {
      "enabled": false,
      "mode": "api",
      "apiKey": "",
      "defaultModel": "gpt-4o"
    },
    "claude": {
      "enabled": false,
      "mode": "api",
      "apiKey": "",
      "defaultModel": "claude-sonnet-4-20250514"
    }
  },
  "channels": {
    "cli": { "enabled": true },
    "telegram": {
      "enabled": false,
      "token": "",
      "allowFrom": [],
      "parseMode": "Markdown"
    },
    "web": {
      "enabled": false,
      "host": "127.0.0.1",
      "port": 8080,
      "auth": { "enabled": false, "username": "", "passwordHash": "" }
    },
    "whatsapp": {
      "enabled": false,
      "appId": "", "appSecret": "", "accessToken": "",
      "verifyToken": "", "phoneNumberId": "",
      "webhookPath": "/webhook/whatsapp"
    },
    "discord": { "enabled": false, "token": "", "guildId": "" },
    "slack": { "enabled": false, "botToken": "", "appToken": "" }
  },
  "memory": {
    "enabled": true,
    "dbPath": "~/.openbot/memory.db",
    "maxHistoryPerConversation": 100,
    "retentionDays": 365
  },
  "security": {
    "defaultPolicy": "ask",            // "allow" | "deny" | "ask"
    "workspaceSandbox": false,
    "blacklist": ["rm -rf /", "mkfs", "dd if="],
    "whitelist": ["ls", "cat", "echo", "pwd", "date", "git status"],
    "confirmPatterns": ["rm ", "sudo ", "kill ", "chmod "],
    "confirmTimeoutSeconds": 60,
    "auditLog": true
  },
  "tools": {
    "shell": { "timeout": 30, "maxOutputBytes": 65536 },
    "screen": { "enabled": false },
    "web": { "searchProvider": "duckduckgo", "searchApiKey": "" }
  },
  "cron": { "enabled": true, "tasks": [] },
  "agents": {
    "enabled": false,
    "mode": "single",
    "routerStrategy": "keyword",
    "agents": {}
  },
  "knowledge": {
    "enabled": false,
    "maxDocuments": 100,
    "chunkSize": 512,
    "chunkOverlap": 50,
    "searchTopK": 5
  },
  "metrics": {
    "enabled": false,
    "endpoint": "/metrics",
    "retentionDays": 30
  },
  "api": {
    "enabled": false,
    "port": 9090,
    "apiKey": ""
  },
  "mcp": {
    "enabled": false,
    "servers": [
      { "name": "my-mcp", "transport": "stdio", "command": "npx", "args": ["-y", "@modelcontextprotocol/server-everything"] }
    ]
  }
}
```

**MCP (Model Context Protocol)** — Set `mcp.enabled: true` and add entries to `mcp.servers` (each: `name`, `transport` — `stdio` \| `http` \| `sse`, and for stdio: `command`/`args`/`env`; for http/sse: `url`). Tools from connected servers are registered with prefix `mcp_<server>_<toolname>`. See [architecture/06-mcp-integration-note.md](docs/projects/architecture/06-mcp-integration-note.md).

---

## Web UI

When `channels.web.enabled` is `true`, the Web UI is available at `http://127.0.0.1:8080`.

| Page | Path | Description |
|------|------|-------------|
| Dashboard | `/` | Stats cards (messages, conversations, sessions), recent conversations, system status |
| Chat | `/chat` | Streaming chat with conversation sidebar, dark mode, tool execution badges |
| Settings | `/settings` | Live configuration editor |

### API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/status` | Health check (`{"status":"ok","version":"0.2.0"}`) |
| GET | `/api/config` | Get current config |
| PUT | `/api/config` | Update config value |
| POST | `/api/config/save` | Save config to disk |
| POST | `/chat/send` | Send message (multipart: `message`, optional `files`; `stream=true` for async) |
| POST | `/chat/clear` | Clear current session |
| GET | `/chat/stream` | SSE stream with structured events |
| GET | `/api/conversations` | List all conversations |
| GET | `/api/conversations/{id}/messages` | Get messages for a conversation |
| DELETE | `/api/conversations/{id}` | Delete a conversation |
| POST | `/api/conversations` | Start new conversation |
| GET | `/api/stats` | Dashboard stats (messages, conversations, sessions) |
| GET | `/api/system` | System status |
| GET | `/metrics` | Prometheus metrics |

### SSE Event Types

| Event | Description |
|-------|-------------|
| `connected` | SSE connection established |
| `thinking` | Agent is processing |
| `token` | Streaming token from LLM |
| `tool_start` | Tool execution started |
| `tool_end` | Tool execution completed |
| `done` | Response complete |
| `error` | Error occurred |

### API Gateway (OpenAI-compatible)

When `api.enabled` is `true`, an OpenAI-compatible API is exposed on port `9090`:

```bash
curl http://localhost:9090/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "model": "ollama",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

---

## Makefile Targets

```bash
make build            # Build the binary → ./build/openbot
make run ARGS='...'   # Run with arguments
make dev              # Run interactive chat (shortcut)
make test             # Run all tests with race detector
make e2e              # Run Playwright E2E tests
make clean            # Clean build artifacts
make install          # Install to $GOPATH/bin
make tidy             # Tidy Go modules
make lint             # Run golangci-lint
make build-linux      # Cross-compile for Linux amd64
make build-darwin     # Cross-compile for macOS ARM64
make docker-build     # Build Docker image
make docker-run       # Run in Docker
make docker-compose   # Docker Compose (detached)
make vendor-assets    # Download vendored frontend assets
make init             # Initialize config
```

---

## Docker

```bash
make docker-build     # Build image
make docker-run       # Run with local config
make docker-compose   # Docker Compose (detached)
```

| Property | Value |
|----------|-------|
| Build base | `golang:1.25-alpine` (multi-stage) |
| Runtime base | `alpine:3.20` |
| Binary | Statically linked (`CGO_ENABLED=0`) |
| User | Non-root `openbot` |
| Health check | `GET /status` every 30s |
| Ports | `8080` (Web UI), `9090` (API Gateway) |

---

## Architecture Highlights

### Concurrency Model
- **Agent loop** processes messages concurrently (configurable `maxConcurrentMessages`)
- **Parallel tool execution** within a single agent turn (reusable semaphore, up to 5 concurrent tools)
- **Provider factory** uses double-check locking to safely cache singleton provider instances
- All HTTP servers configured with proper timeouts to prevent resource exhaustion

### Performance Optimizations
- **Singleton HTTP client** (`sync.Once`) with shared transport: 100 idle connections, 20 per-host, HTTP/2
- **O(n) streaming** via `strings.Builder` (replaces O(n^2) concatenation in hot path)
- **Pre-compiled regex** and **pre-lowered keywords** in skill registry and agent router
- **Prompt cache** with 60s TTL and periodic cleanup to prevent unbounded memory growth
- **Retry with jitter** — exponential backoff + randomized jitter to avoid thundering herd

### Security Layers
1. **Blacklist/Whitelist/Confirm** — Policy engine on every tool execution
2. **Workspace sandbox** — File tools restricted to configured workspace
3. **HTTP hardening** — `ReadHeaderTimeout`, `ReadTimeout`, `IdleTimeout`, `MaxHeaderBytes`
4. **Body size limits** — 1MB max on API Gateway requests
5. **Audit logging** — Every tool execution recorded
6. **Web auth** — Optional HTTP Basic Auth for Web UI

---

## Community & testimonials

We only list **real** testimonials (tweets, blog posts, case studies) with explicit consent. If you've used OpenBot and are happy to be quoted or linked, see [docs/social-proof.md](docs/social-proof.md) for how to submit. No fabricated quotes.

---

## Contributing & Security

- **Contributing**: See [CONTRIBUTING.md](CONTRIBUTING.md) for how to contribute code, report bugs, and propose features.
- **Security**: See [SECURITY.md](SECURITY.md) for supported versions, how to report vulnerabilities, security headers, and dependency audit.

---
## License

MIT
