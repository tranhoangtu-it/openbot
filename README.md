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
</p>

---

## Overview

OpenBot is a self-hosted AI assistant written entirely in Go. It connects to multiple LLM providers (with **token-by-token streaming**), exposes five user-facing channels (CLI, Telegram, Web UI, WhatsApp, API Gateway), and lets the agent interact with your system through a pluggable tool and skills system — all while enforcing security policies on every action.

**Key design goals**: single binary deployment, zero JavaScript frameworks (vendored assets), persistent memory with RAG knowledge engine, and full control over your data.

---

## What's New in v0.2.0

- **Token-by-token streaming** — OpenAI and Claude providers now stream responses in real-time
- **Conversation sidebar** — Browse, search, and manage multiple conversations
- **Dark mode** — System-aware dark/light theme across all pages
- **Provider switching** — Select LLM provider per message
- **Parallel tool execution** — Multiple tool calls run concurrently (up to 5)
- **SQLite read/write split** — Separate reader pool for concurrent reads
- **Prompt caching** — 60s TTL cache for system prompts
- **Prometheus metrics** — Built-in `/metrics` endpoint
- **WhatsApp channel** — Cloud API integration with webhook signature verification
- **API Gateway** — OpenAI-compatible `/v1/chat/completions` endpoint
- **Skills system** — Reusable workflows (built-in + user YAML definitions)
- **Knowledge engine (RAG)** — Document upload, FTS5 chunked search, context injection
- **Multi-agent router** — Keyword-based routing to specialized agent profiles
- **Event system** — Internal pub/sub for cross-component communication
- **Vendored assets** — Tailwind, marked.js, highlight.js, htmx bundled in binary (no CDN)

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
| **Ollama** | API | Yes | Yes | Local/cloud, exponential backoff retry |
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

### Persistent Memory & Knowledge

- **SQLite-backed** with read/write connection splitting (4 reader pool)
- Per-conversation message history with provider/model/latency tracking
- **Knowledge engine (RAG)**: Upload documents → chunked FTS5 search → context injection
- Long-term memory entries with TTL
- Auto-generated conversation titles

### Skills System

- **Built-in skills**: system_health, code_review, research
- **User-defined skills**: YAML files in `~/.openbot/skills/`
- Keyword and regex pattern matching
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

# Vendor frontend assets (optional — already included in repo)
make vendor-assets

# Build
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

### New v0.2.0 Config Sections

```json
{
  "general": {
    "maxConcurrentMessages": 5
  },
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
  "channels": {
    "whatsapp": {
      "enabled": false,
      "appId": "",
      "appSecret": "",
      "accessToken": "",
      "verifyToken": "",
      "phoneNumberId": "",
      "webhookPath": "/webhook/whatsapp"
    }
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
| GET | `/status` | Health check |
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

---

## Project Structure

```
openbot/
├── cmd/openbot/main.go              # CLI entry point (Cobra)
├── internal/
│   ├── domain/                       # Interfaces & types
│   │   ├── provider.go               #   Provider, StreamingProvider, StreamEvent
│   │   ├── skill.go                  #   SkillDefinition, SkillRegistry, SkillExecutor
│   │   ├── knowledge.go              #   KnowledgeStore, Document, DocumentChunk
│   │   ├── memory.go                 #   MemoryStore (with provider/model/latency fields)
│   │   └── message.go                #   InboundMessage (with Provider field), OutboundMessage (with StreamEvent)
│   ├── agent/                        # Agent engine
│   │   ├── loop.go                   #   Core loop: streaming, parallel tools, provider switching
│   │   ├── parser.go                 #   Tool call extraction from LLM content
│   │   ├── prompt.go                 #   System prompt builder with caching (60s TTL)
│   │   ├── router.go                 #   Multi-agent router (keyword strategy)
│   │   ├── context_manager.go        #   Centralized context: memory + skills + knowledge
│   │   ├── session.go                #   Conversation & session manager
│   │   └── ratelimit.go              #   Token bucket rate limiter
│   ├── provider/                     # LLM providers
│   │   ├── openai.go                 #   OpenAI API + ChatStream (SSE)
│   │   ├── claude.go                 #   Claude API + ChatStream (SSE)
│   │   ├── ollama.go                 #   Ollama (streaming)
│   │   ├── factory.go                #   Provider factory (registry + cache)
│   │   ├── retry.go                  #   Exponential backoff retry
│   │   └── httpclient.go             #   Shared HTTP client pool
│   ├── channel/                      # User-facing channels
│   │   ├── web.go                    #   Web UI (SSE streaming, conversations API, stats)
│   │   ├── web_config.go             #   Web config API handlers
│   │   ├── whatsapp.go               #   WhatsApp Cloud API channel
│   │   ├── api_gateway.go            #   OpenAI-compatible API gateway
│   │   ├── telegram.go               #   Telegram bot
│   │   ├── cli.go                    #   CLI REPL
│   │   ├── web_templates/            #   HTML templates (dark mode, sidebar, streaming)
│   │   └── web_assets/               #   Vendored JS/CSS (Tailwind, marked, hljs, htmx)
│   ├── skill/                        # Skills system
│   │   ├── registry.go               #   Skill registry with keyword/regex matching
│   │   ├── executor.go               #   Multi-step skill execution
│   │   └── yaml_loader.go            #   Load user skills from YAML
│   ├── knowledge/                    # Knowledge engine (RAG)
│   │   └── engine.go                 #   Document chunking, FTS5 search, context building
│   ├── metrics/                      # Observability
│   │   └── collector.go              #   Prometheus-compatible metrics (counters, gauges, histograms)
│   ├── memory/
│   │   └── store.go                  #   SQLite (read/write split, v2 schema, knowledge tables)
│   ├── security/engine.go            #   Security policy engine + audit
│   ├── bus/
│   │   ├── bus.go                    #   Message bus (Go channels)
│   │   └── events.go                 #   Event system (pub/sub)
│   ├── tool/                         #   9 agent tools
│   ├── browser/bridge.go             #   Headless Chrome bridge
│   └── config/                       #   Config (v2 schema: agents, knowledge, metrics, api, whatsapp)
├── e2e/                              # Playwright E2E tests
├── docs/                             # Documentation (Vietnamese)
├── Makefile                          # Build targets including vendor-assets
├── Dockerfile                        # Multi-stage Docker build
└── docker-compose.yml
```

---

## Testing

### Go Unit Tests (89 tests)

```bash
make test    # go test ./... -v -race -count=1
```

| Package | Tests | Coverage |
|---------|-------|----------|
| `internal/agent` | 35 | loop, parser, ratelimit, session, security commands |
| `internal/config` | 25 | validate, load/save, accessor, flex types |
| `internal/security` | 16 | blacklist, whitelist, confirm, policy |
| `internal/tool` | 13 | registry, parameters, args |

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
| Binary | Statically linked (CGO_ENABLED=0) |
| User | Non-root `openbot` |
| Health check | `GET /status` every 30s |
| Ports | `8080` (Web UI), `9090` (API Gateway, optional) |

---

## License

MIT
