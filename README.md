<p align="center">
  <img src="assets/logo.png" alt="OpenBot Logo" width="200"/>
</p>

<h1 align="center">OpenBot</h1>

<p align="center">
  <strong>Open-Source Personal AI Assistant</strong><br/>
  Go-native &bull; Multi-LLM &bull; Multi-Channel &bull; Tool-Augmented &bull; Secure
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white" alt="Go"/>
  <img src="https://img.shields.io/badge/License-MIT-green" alt="License"/>
  <img src="https://img.shields.io/badge/Platform-macOS%20%7C%20Linux%20%7C%20Docker-lightgrey" alt="Platform"/>
  <img src="https://img.shields.io/badge/Version-0.2.0-blue" alt="Version"/>
  <img src="https://img.shields.io/badge/Tests-94%20passed-brightgreen" alt="Tests"/>
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
- **API Gateway** — OpenAI-compatible `/v1/chat/completions` endpoint
- **Skills system** — Reusable workflows (built-in + user YAML definitions)
- **Knowledge engine (RAG)** — Document upload, FTS5 chunked search, context injection
- **Multi-agent router** — Keyword-based routing to specialized agent profiles
- **Event system** — Internal pub/sub for cross-component communication
- **Prometheus metrics** — Built-in `/metrics` endpoint
- **Vendored assets** — Tailwind, marked.js, highlight.js, htmx bundled in binary (no CDN)

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

| Channel | Description |
|---------|-------------|
| **CLI** | Interactive REPL with animated spinner, inline confirmation prompts |
| **Telegram Bot** | Full-featured bot with inline keyboard confirmations, Markdown rendering |
| **Web UI** | Dashboard with stats, streaming chat with sidebar, dark mode, settings editor |
| **WhatsApp** | Cloud API integration with webhook verification |
| **API Gateway** | OpenAI-compatible `/v1/chat/completions` endpoint |

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
    "maxConcurrentMessages": 5         // parallel message processing
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
    }
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
  }
}
```

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
| POST | `/chat/send` | Send message (supports `stream=true` for async mode) |
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

## Project Structure

```
openbot/
├── cmd/openbot/main.go              # CLI entry point (Cobra commands)
├── internal/
│   ├── domain/                       # Interfaces & types
│   │   ├── provider.go               #   Provider, StreamingProvider, StreamEvent
│   │   ├── skill.go                  #   SkillDefinition, SkillRegistry, SkillExecutor
│   │   ├── knowledge.go              #   KnowledgeStore, Document, DocumentChunk
│   │   ├── memory.go                 #   MemoryStore (with provider/model/latency fields)
│   │   └── message.go                #   InboundMessage, OutboundMessage (with StreamEvent)
│   ├── agent/                        # Agent engine
│   │   ├── loop.go                   #   Core loop: streaming (strings.Builder), parallel tools
│   │   ├── parser.go                 #   Tool call extraction from LLM content
│   │   ├── prompt.go                 #   System prompt builder with caching (60s TTL + cleanup)
│   │   ├── router.go                 #   Multi-agent router (pre-computed keywords)
│   │   ├── context_manager.go        #   Centralized context: memory + skills + knowledge
│   │   ├── session.go                #   Conversation & session manager
│   │   └── ratelimit.go              #   Token bucket rate limiter
│   ├── provider/                     # LLM providers
│   │   ├── openai.go                 #   OpenAI API + ChatStream (shared helpers)
│   │   ├── claude.go                 #   Claude API + ChatStream (shared helpers)
│   │   ├── ollama.go                 #   Ollama (streaming)
│   │   ├── factory.go                #   Provider factory (double-check locking cache)
│   │   ├── retry.go                  #   Exponential backoff with jitter
│   │   └── httpclient.go             #   Singleton HTTP client (sync.Once, HTTP/2)
│   ├── channel/                      # User-facing channels
│   │   ├── web.go                    #   Web UI (SSE streaming, conversations API, hardened server)
│   │   ├── web_config.go             #   Web config API handlers
│   │   ├── whatsapp.go               #   WhatsApp Cloud API channel
│   │   ├── api_gateway.go            #   OpenAI-compatible API gateway (sync.Mutex, body limits)
│   │   ├── telegram.go               #   Telegram bot
│   │   ├── cli.go                    #   CLI REPL
│   │   ├── web_templates/            #   HTML templates (dark mode, sidebar, streaming)
│   │   └── web_assets/               #   Vendored JS/CSS (Tailwind, marked, hljs, htmx)
│   ├── skill/                        # Skills system
│   │   ├── registry.go               #   Skill registry (pre-compiled regex, cached keywords)
│   │   ├── executor.go               #   Multi-step skill execution
│   │   └── yaml_loader.go            #   Load user skills from YAML
│   ├── knowledge/                    # Knowledge engine (RAG)
│   │   └── engine.go                 #   Document chunking, FTS5 search, context building
│   ├── metrics/                      # Observability
│   │   └── collector.go              #   Prometheus-compatible metrics
│   ├── memory/
│   │   └── store.go                  #   SQLite (read/write split, v2 schema)
│   ├── security/engine.go            #   Security policy engine + audit
│   ├── bus/
│   │   ├── bus.go                    #   Message bus (Go channels)
│   │   └── events.go                 #   Event system (pub/sub)
│   ├── tool/                         #   9 agent tools
│   ├── browser/bridge.go             #   Headless Chrome bridge
│   └── config/                       #   Config (v2 schema)
├── docs/                             # Documentation (Vietnamese)
│   ├── attachment/                   #   File Attachment feature analysis (UR/SR)
│   └── improved/                     #   Improvement proposals & roadmap
├── Makefile                          # Build, test, docker, cross-compile
├── Dockerfile                        # Multi-stage Docker build (golang:1.25-alpine)
├── docker-compose.yml                # Ready-to-run composition
├── config.example.json               # Full config template
└── go.mod
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

## Testing

### Go Unit Tests (94 tests)

```bash
make test    # go test ./... -v -race -count=1
```

| Package | Tests | Coverage |
|---------|-------|----------|
| `internal/agent` | 40 | loop, parser (embedded JSON extraction), ratelimit, session, security commands |
| `internal/config` | 25 | validate, load/save, accessor, flex types |
| `internal/security` | 16 | blacklist, whitelist, confirm, policy |
| `internal/tool` | 13 | registry, parameters, args |

All tests pass with the Go race detector enabled (`-race`), confirming zero data races across all concurrent subsystems.

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

## Documentation

Full documentation (Vietnamese) is available in the `docs/` directory:

### Core Documentation

| File | Topic |
|------|-------|
| `01-tong-quan.md` | Overview |
| `02-kien-truc.md` | Architecture |
| `03-cau-hinh.md` | Configuration |
| `04-api.md` | API Reference |
| `05-cong-cu-agent.md` | Agent Tools |
| `06-bao-mat.md` | Security |
| `07-trien-khai.md` | Deployment |
| `08-kiem-thu.md` | Testing |

### Feature Analysis — File Attachment (`docs/attachment/`)

Requirement analysis for the upcoming file attachment feature — enabling users to upload documents, code, and data files for Agent analysis via RAG.

| File | Topic |
|------|-------|
| `README.md` | Feature overview & architecture diagram |
| `01-requirement-analysis.md` | Full UR/SR analysis, gap analysis, consistency check |

### Improvement Proposals (`docs/improved/`)

| File | Topic |
|------|-------|
| `01-phan-tich-hien-trang.md` | Current state analysis |
| `02-de-xuat-tinh-nang.md` | Feature proposals |
| `03-thiet-ke-ui.md` | UI design |
| `04-nang-cap-performance.md` | Performance upgrades |
| `05-nang-cap-kien-truc.md` | Architecture upgrades |
| `06-roadmap.md` | Roadmap |
| `07-kien-truc-muc-tieu.md` | Target architecture |
| `08-whatsapp-integration.md` | WhatsApp integration |
| `09-he-thong-skills.md` | Skills system |

---

## License

MIT
