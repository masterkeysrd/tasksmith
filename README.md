<p align="center">
  <img src="./assets/tasksmith-logo.svg" alt="TaskSmith Anvil Logo" width="160" height="106" />
</p>

<h1 align="center">TaskSmith</h1>

<p align="center">
  <strong>An extensible, daemon-backed AI coding agent for the terminal with OS-level sandboxing, multi-provider routing, and declarative workspaces.</strong>
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> •
  <a href="#key-features">Key Features</a> •
  <a href="#interactive-cli">Interactive CLI</a> •
  <a href="./docs/README.md">Documentation</a> •
  <a href="./docs/architecture/daemon.md">Architecture</a>
</p>

---

## Overview

**TaskSmith** is a developer-centric artificial intelligence pair programmer engineered for the terminal. Built from the ground up on a **client-daemon architecture**, TaskSmith runs code generation, tool execution, and workspace indexing in an isolated background daemon while providing a responsive, interactive terminal user interface.

Unlike monolithic AI coding tools tied to a single vendor or unconfined host shell execution, TaskSmith pairs **strict operating-system sandboxing** and **AST shell inspection** with **pluggable multi-provider model routing** and **declarative, version-controlled workspaces**.

---

## Key Features

### 🖥️ Client-Daemon IPC & Real-Time Event Multiplexing
- **Persistent Background Daemon**: The daemon runs as an on-demand, headless service communicating over a local Unix domain socket (`~/.local/state/tasksmith/daemon.sock`).
- **Multiplexed SSE Bus**: Real-time token streaming, tool call start/finish events, interactive permission requests, and background task progress are streamed via Server-Sent Events (`/v1/events`).
- **Seamless Session Resumption**: Detach from a long-running turn or open multiple terminal windows against the same workspace with instantaneous state synchronization.
- **Durable Turn Journaling**: Backed by a transactional SQLite journal that ensures conversation turns and session history survive daemon restarts.

### 🌐 Triple-Role Model Gateway
TaskSmith completely decouples agents and skills from specific AI vendors by routing across three cognitive workload roles:
- **System 2 (`completion`)**: Multi-turn conversational reasoning, tool dispatch, and code generation.
- **System 1 (`decision`)**: Calibrated probabilistic classification, security gate checks, and workflow branch routing.
- **System 3 (`embedding`)**: Dense vector generation for codebase indexing, semantic search, and long-term memory.

Built-in driver support and presets include:
- **OpenAI**: Native support for Chat Completions (`/v1/chat/completions`) and the modern Responses API (`/v1/responses`) with **Zero Data Retention (`store: false`)** by default, client-side encrypted reasoning continuity, native server-side tools (`webSearch`, `codeInterpreter`, `fileSearch`), and compatible endpoints (vLLM, LiteLLM, OpenRouter, Together AI).
- **Anthropic**: Frontier reasoning (`claude-sonnet-5`, `claude-opus-5`), extended thinking (`type: effort`), and automated prompt prefix caching.
- **Ollama**: 100% offline local inference (`qwen3.8`, `nimble-v3`, `nomic-embed-text`) with zero API token costs and zero data egress.
- **Google GenAI**: Frontier Gemini models with 1M+ token context windows, native multimodal ingestion, and `text-embedding-004`.
- **TypeSafe**: Sub-50ms native System 1 deterministic decisions (`jev-latest`) at fractional cost.
- **Antigravity**: Enterprise model gateway with managed thinking effort tiers.

### 🛡️ Capability-Isolated Sandbox & Shell AST Inspection
- **Least-Privilege Capability Decomposition**: Tools receive only the specific capabilities they require—Filesystem (`FS`), Process (`Proc`), or Network (`Net`).
- **Path Confinement**: The filesystem capability is strictly jailed to the workspace root directory, resolving symlinks and blocking directory traversal attacks (`../`).
- **OS Kernel Sandboxing**: Process execution can be confined using native kernel sandboxes (Apple Seatbelt on macOS, Bubblewrap namespaces on Linux) to prevent unauthorized host modifications.
- **Shell AST Inspection (`shellguard`)**: Commands executed via `bash` are parsed into an Abstract Syntax Tree (AST) to unpack compound pipelines, subshells, command substitutions, and redirections, blocking destructive commands and smuggling techniques before execution.

### ⏪ Transactional Timeline & Instant Revert
- **Automated Snapshots**: Before any mutating tool (`edit`, `write`, `bash`) modifies a file, TaskSmith records an immutable snapshot of the original content.
- **Unified Diff Inspection**: Inspect syntax-highlighted diffs across individual tool calls or entire multi-file turns at any time (`/changes`).
- **One-Keystroke Rollbacks**: Revert specific turns or roll back entire sessions with `/revert`, restoring files on disk to their exact pre-turn state.

### 📁 Declarative 4-Tier Workspaces
- **Version-Controlled Workflows**: Define agents, skills, prompt commands, decision classifiers, provider configs, custom tools, and MCP servers directly in your repository (`WORKSPACE.md`, `AGENTS.md`, `.agents/`).
- **Hierarchical Precedence**: Resources resolve across four clean tiers:
  1. `system`: Embedded core agents and tools.
  2. `user`: Global preferences in `~/.config/tasksmith/`.
  3. `workspace`: Repository-wide conventions in `<root>/WORKSPACE.md` and `<root>/.agents/`.
  4. `local`: Service-specific overrides in monorepo subprojects.
- **Isolated Secret Layering**: Secrets are layered in-memory across `.env` files with fallback to host process environment variables (`os.Getenv`), featuring **lazy credential validation** that never blocks startup for unused providers.

---

## Quick Start

### Prerequisites
- **Go**: Version 1.24 or later
- **Operating System**: macOS or Linux
- **Optional**: [ripgrep](https://github.com/BurntSushi/ripgrep) for ultra-fast code search, [Ollama](https://ollama.com) for local offline inference

### 1. Installation

Install via `go install`:

```bash
go install github.com/masterkeysrd/tasksmith/cmd/tasksmith@latest
```

Or build from source:

```bash
git clone https://github.com/masterkeysrd/tasksmith.git
cd tasksmith
go build -o tasksmith ./cmd/tasksmith
```

### 2. Configure Your Model Provider

Set an API key in your environment:

```bash
# OpenAI
export OPENAI_API_KEY=sk-proj-...

# Or Anthropic
export ANTHROPIC_API_KEY=sk-ant-api03-...

# Or Google GenAI
export GOOGLE_API_KEY=AIzaSy...
```

*(Alternatively, use [Ollama](./docs/reference/providers/ollama.md) for 100% local, offline inference without an API key)*

### 3. Launch TaskSmith

Open any Git repository or directory and run:

```bash
tasksmith
```

If the project has not been initialized yet, TaskSmith's interactive Setup Wizard will automatically detect your project runtime, guide you through provider selection, and scaffold a starter `WORKSPACE.md`.

---

## Interactive CLI

TaskSmith provides a rich terminal interface designed for continuous pair programming:

```text
 ╭─ TaskSmith v2.4.0 ────────────────────────────────────────── default ──╮
 │ Workspace: acme-platform [main]               Model: openai/gpt-5.5    │
 ╰────────────────────────────────────────────────────────────────────────╯
```

### Essential Slash Commands

| Command | Action | Description |
| :--- | :--- | :--- |
| `/model` | Select Model | Switch the active LLM or view registered provider catalogs. |
| `/agent` | Switch Agent | Switch between specialized personas (`main`, `reviewer`, `architect`). |
| `/effort` | Set Thinking Effort | Select a model-supported thinking intensity. |
| `/budget` | Set Thinking Budget | Set a supported thinking-token budget. |
| `/thinking` | Toggle Thinking | Turn thinking on or off when supported. |
| `/mode` | Permission Mode | Switch operational mode (`default`, `plan`, `auto`) or press `Shift+Tab`. |
| `/changes` | Review Diff | Inspect unified syntax-highlighted diffs of files modified in the session. |
| `/revert` | Rollback Turn | Atomically restore modified files to their pre-turn state on disk. |
| `/tasks` | Background Tasks | View and manage running background shell processes and compilers. |
| `!<cmd>` | Instant Bash | Prefix any line with `!` to run a sandboxed shell command directly. |

For the full keyboard shortcut reference and navigation guide, see the [CLI User Guide](./docs/guides/cli.md).

---

## Documentation Map

Explore the complete TaskSmith documentation:

```text
docs/
├── README.md                      # Documentation portal & index
├── guides/                        # Step-by-step user guides
│   ├── workspaces.md              # Workspace hierarchy, discovery, and .env secrets
│   ├── cli.md                     # Terminal interface, keybindings, and workflows
│   ├── permissions.md             # Security modes, rule matching, and approval scopes
│   ├── customizations.md          # Authoring custom agents, skills, and commands
│   └── tools-and-mcp.md           # Built-in tools and Model Context Protocol (MCP)
├── architecture/                  # Deep-dive subsystem architecture
│   ├── daemon.md                  # Daemon service, IPC, event bus, and SQLite journal
│   ├── harness.md                 # Execution graph, suspension, and context compaction
│   ├── gateway.md                 # Gateway pooling, triple roles, and lazy auth
│   ├── sandbox.md                 # FS, Proc, Net capability isolation & OS kernels
│   └── permissions.md             # Security pipeline, shellguard AST, and path guard
└── reference/                     # Formal specifications & schemas
    ├── providers.md               # ModelProvider manifest schema & role defaults
    ├── manifests.md               # Warp resource specifications (Agent, Skill, Tool)
    ├── tools.md                   # Built-in tool parameter schemas
    ├── cli.md                     # Complete CLI flags and subcommand reference
    └── providers/                 # Upstream provider configuration guides
        ├── openai.md              # OpenAI (Responses API, ZDR, tools, compatible endpoints)
        ├── anthropic.md           # Anthropic (Claude 4.5/5, extended thinking, caching)
        ├── ollama.md              # Ollama (local offline inference, triple roles)
        ├── google-genai.md        # Google GenAI (Gemini models, 1M+ context, embeddings)
        ├── typesafe.md            # TypeSafe (System 1 calibrated decision engine)
        └── antigravity.md         # Antigravity (enterprise gateway & effort tiers)
```

- **[Documentation Portal](./docs/README.md)**
- **[System Architecture](./docs/architecture/daemon.md)**
- **[Provider Reference](./docs/reference/providers.md)**

---

## Repository Structure

```text
tasksmith/
├── cmd/tasksmith/         # CLI entry point and daemon startup commands
├── api/                   # Public client/daemon protocol types and event schemas
├── client/                # Go client library for communicating with the daemon
├── internal/
│   ├── cli/               # Terminal user interface components, store, and REPL
│   ├── daemon/            # Core daemon subsystems
│   │   ├── broker/        # Multiplexed SSE event pub/sub broker
│   │   ├── gateway/       # Content-addressable provider gateway pool
│   │   ├── harness/       # Graph-driven agent execution harness
│   │   ├── journal/       # Durable SQLite session and turn journal
│   │   ├── permissions/   # Layered permission manager & rule evaluation
│   │   ├── sandbox/       # Virtualized FS, Proc, Net capability sandbox
│   │   ├── session/       # Session lifecycle state machine and turn runner
│   │   ├── timeline/      # Transactional file snapshots and diff engine
│   │   ├── tools/         # Built-in tools and external MCP client container
│   │   └── workspace/     # 4-tier Warp manifest loader and secret layering
│   └── core/              # Shared security, logging, and OS utilities
│       ├── guard/         # Path boundary confinement and traversal checks
│       ├── shellguard/    # Shell command AST parser and security classifier
│       └── xdg/           # Portable standard platform paths
└── docs/                  # Public documentation, architecture deep dives, and guides
```

---

## Contributing & License

Contributions to TaskSmith are welcome! Please ensure all code changes include corresponding documentation updates and tests.

TaskSmith is licensed under the Apache 2.0 License.
