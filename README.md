<p align="center">
  <img src="./assets/tasksmith-logo.svg" alt="TaskSmith Anvil Logo" width="160" height="106" />
</p>

<h1 align="center">TaskSmith</h1>

<p align="center">
  <strong>Provider-agnostic coding agents. Deterministic environments.</strong><br />
  Run version-controlled agents, skills, rules, and kernel-sandboxed workflows across any model provider—from your terminal, editor, or CI.
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> •
  <a href="#key-features">Key Features</a> •
  <a href="#interactive-cli">Interactive CLI</a> •
  <a href="./docs/README.md">Documentation</a> •
  <a href="./docs/architecture/daemon.md">Architecture</a> •
  <a href="./ROADMAP.md">Roadmap</a>
</p>

---

## Overview

**TaskSmith** is a developer-centric artificial intelligence pair programmer engineered for the terminal. Built from the ground up on a **client-daemon architecture**, TaskSmith runs code generation, tool execution, and workspace indexing in an isolated background daemon while providing a responsive, interactive terminal user interface.

Unlike monolithic AI coding tools tied to a single vendor or unconfined host shell execution, TaskSmith pairs **strict operating-system sandboxing** and **AST shell inspection** with **pluggable multi-provider model routing** and **declarative, version-controlled workspaces**.

---

## Key Features

### 🖥️ Client-Daemon IPC & Real-Time Event Multiplexing
- **Persistent Background Daemon**: The daemon runs as an on-demand, headless service communicating over a local Unix domain socket (`$XDG_RUNTIME_DIR/tasksmith/brain.sock`, falling back to `~/.local/state/tasksmith/run/brain.sock` when `XDG_RUNTIME_DIR` is unset).
- **Multiplexed SSE Bus**: Real-time token streaming, tool call start/finish events, interactive permission requests, and background task progress are streamed via Server-Sent Events (`/v1/events`).
- **Seamless Session Resumption**: Detach from a long-running turn or open multiple terminal windows against the same workspace with instantaneous state synchronization.
- **Durable Turn Journaling**: Backed by a transactional SQLite journal that ensures conversation turns and session history survive daemon restarts.

### 🌐 Triple-Role Model Gateway
TaskSmith completely decouples agents and skills from specific AI vendors by routing across three cognitive workload roles:
- **System 2 (`completion`)**: Multi-turn conversational reasoning, tool dispatch, and code generation — the role the agent harness drives today.
- **System 1 (`decision`)**: Calibrated probabilistic classification, security gate checks, and workflow branch routing. The gateway exposes native decision routing plus a structured-output fallback adapter, and the `decision` role is declarable on providers and models; harness-side consumers (e.g., rule `triggers.decision`) are planned and currently surface a diagnostic when declared.
- **System 3 (`embedding`)**: Dense vector generation for codebase indexing, semantic search, and long-term memory. Embedding models are first-class catalog entries routable through the gateway; the indexing loops that will consume them are planned.

Built-in driver support and presets include:
- **OpenAI**: Native support for Chat Completions (`/v1/chat/completions`) and the modern Responses API (`/v1/responses`) with **Zero Data Retention (`store: false`)** by default, client-side encrypted reasoning continuity, native server-side tools (`webSearch`, `codeInterpreter`, `fileSearch`), and compatible endpoints (vLLM, LiteLLM, OpenRouter, Together AI).
- **Anthropic**: Frontier reasoning (`claude-sonnet-5-5`, `claude-opus-5-5`), extended thinking (`type: effort`), and automated prompt prefix caching.
- **Ollama**: 100% offline local inference (`qwen3.8`, `mimo-v2.6`, `nemotron-3.5-lightning`) with zero API token costs and zero data egress.
- **Google GenAI**: Frontier Gemini models (`gemini-4-hydrogen`) with 1M+ token context windows and native multimodal ingestion.
- **TypeSafe**: Sub-50ms native System 1 deterministic decisions (`jev-latest`) at fractional cost.
- **Antigravity**: Enterprise model gateway with managed thinking effort tiers.

### 🛡️ Capability-Isolated Sandbox & Shell AST Inspection
- **Least-Privilege Capability Decomposition**: Tools receive only the specific capabilities they require—Filesystem (`FS`), Process (`Proc`), or Network (`Net`).
- **Path Confinement**: The filesystem capability is strictly jailed to the workspace root directory, resolving symlinks and blocking directory traversal attacks (`../`).
- **OS Kernel Sandboxing**: Session child processes run inside native kernel sandboxes by default (Apple Seatbelt on macOS, Bubblewrap namespaces on Linux), confining writes to the workspace and masking host credential directories such as `~/.ssh` from agent commands.
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

Follow the [Quickstart](./docs/guides/quickstart.md) for a guided first session.

### Prerequisites
- **Operating System**: macOS or Linux
- **Go**: Version 1.26 or later *(only required when installing via `go install` or building from source)*
- **Optional**: [ripgrep](https://github.com/BurntSushi/ripgrep) for ultra-fast code search, [Ollama](https://ollama.com) for local offline inference

### 1. Installation

Install via **Homebrew** (macOS & Linux):

```bash
brew tap masterkeysrd/tasksmith https://github.com/masterkeysrd/tasksmith
brew install masterkeysrd/tasksmith/tasksmith
```

Install via **Shell script** (macOS & Linux — installs to `~/.local/bin` and verifies SHA-256 checksums):

```bash
curl -fsSL https://github.com/masterkeysrd/tasksmith/releases/latest/download/install.sh | sh
```

Install via **`go install`**:

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
 ╭─ TaskSmith v0.0.3 ─────────────────────────────────────────── default ──╮
 │ Workspace: acme-platform [main]              Model: openai/gpt-6.1-sol  │
 ╰─────────────────────────────────────────────────────────────────────────╯
```

### Essential Slash Commands

| Command | Action | Description |
| :--- | :--- | :--- |
| `/model` | Select Model | Switch the active LLM or view registered provider catalogs. |
| `/agent` | Switch Agent | Switch between specialized personas (`main`, `reviewer`, `architect`). |
| `/effort` | Set Thinking Effort | Select a model-supported thinking intensity. |
| `/budget` | Set Thinking Budget | Set a supported thinking-token budget. |
| `/thinking` | Toggle Thinking | Turn thinking on or off when supported. |
| `/mode` | Permission Mode | Switch operational mode (`default`, `accept_edits`, `plan`, `ask`, `auto`) or press `Shift+Tab`. |
| `/changes` | Review Diff | Inspect unified syntax-highlighted diffs of files modified in the session. |
| `/revert` | Rollback Turn | Atomically restore modified files to their pre-turn state on disk. |
| `/tasks` | Background Tasks | View and manage running background shell processes and compilers. |
| `!<cmd>` | Instant Bash | Prefix any line with `!` to run a sandboxed shell command directly. |

See the [interactive terminal guide](./docs/guides/cli.md) and [keyboard shortcut reference](./docs/reference/cli/keybindings.md).

---

## Documentation Map

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
