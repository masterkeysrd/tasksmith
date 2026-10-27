# TaskSmith Documentation

TaskSmith is an autonomous, agentic software engineering platform powered by a local daemon, decoupled AI provider gateways, and virtualized OS sandboxes.

---

## 1. User Guides (`docs/guides/`)

Step-by-step guides for setting up workspaces, authoring agents, managing security, and integrating tools:

| Guide | Description |
| :--- | :--- |
| [Workspaces & Projects](./guides/workspaces.md) | Multi-tier resource hierarchy (`WORKSPACE.md`, `AGENTS.md`), project discovery, and isolated `.env` secret layering. |
| [CLI & Interactive Workflow](./guides/cli.md) | Terminal user interface, slash commands (`/effort`, `/mode`, `/revert`), interactive reviews, and keyboard shortcuts. |
| [Security & Permissions](./guides/permissions.md) | Operational modes (`default`, `plan`, `auto`), rule matching (`deny`, `allow`, `ask`), and interactive approval scopes. |
| [Customizations & Extensions](./guides/customizations.md) | Building custom agents (`*.agent.md`), skills (`*.skill.md`), commands (`*.command.md`), and decisions (`*.decision.md`). |
| [Tools & MCP Servers](./guides/tools-and-mcp.md) | Using built-in tools (`view`, `edit`, `write`, `bash`), custom tools, and integrating Model Context Protocol (MCP) servers. |

---

## 2. System Architecture (`docs/architecture/`)

Deep dives into the internal subsystems and design invariants of the TaskSmith engine:

| Subsystem | Focus Area |
| :--- | :--- |
| [Daemon Architecture](./architecture/daemon.md) | Background service lifecycle, Unix domain socket IPC, SSE event multiplexing, and SQLite turn journaling. |
| [Agent Execution Harness](./architecture/harness.md) | Directed execution graph, interactive mid-turn suspension, context compaction, and subagent delegation. |
| [Provider Gateway](./architecture/gateway.md) | Content-addressable gateway pooling, role-based model routing (`completion`, `decision`, `embedding`), and lazy auth validation. |
| [Virtualized Sandbox](./architecture/sandbox.md) | OS capability decomposition (`FS`, `Proc`, `Net`), local process groups, and native kernel isolation (Seatbelt/Bubblewrap). |
| [Permission & Guard Engine](./architecture/permissions.md) | Multi-stage security pipeline, shell Abstract Syntax Tree (AST) inspection, and path confinement. |

---

## 3. Reference Specifications (`docs/reference/`)

Declarative manifest schemas, CLI command specifications, and provider configuration references:

| Reference | Description |
| :--- | :--- |
| [Providers Overview & Manifest Schema](./reference/providers.md) | General `kind: ModelProvider` Warp manifest schema, authentication schemes, and model capability properties. |
| [Resource Manifest Reference](./reference/manifests.md) | Formal YAML frontmatter specifications for agents, skills, commands, toolkits, and MCP manifests. |
| [Built-in Tool Reference](./reference/tools.md) | Comprehensive parameter reference and output schemas for `view`, `write`, `edit`, `bash`, `glob`, `grep`, `web_fetch`, and `ask_question`. |
| [CLI Command Reference](./reference/cli.md) | Command-line flags, startup arguments, daemon management subcommands, and environment variables. |

### Upstream Provider Guides (`docs/reference/providers/`)

Detailed guides for configuring and connecting upstream model providers:

- [OpenAI](./reference/providers/openai.md) — Frontier reasoning (`gpt-5.5`, `o3`), Chat Completions vs Responses API (`apiMode`), Zero Data Retention (`store: false`), and compatible endpoints.
- [Anthropic](./reference/providers/anthropic.md) — Claude 4.5 & 5 models (`claude-sonnet-5`, `claude-opus-5`), extended thinking, and prompt caching.
- [Ollama](./reference/providers/ollama.md) — 100% offline local inference (`qwen3.8`, `nimble-v3`, `nomic-embed-text`), zero data egress, and remote clusters.
- [Google GenAI](./reference/providers/google-genai.md) — Gemini models (`gemini-3.1-pro-preview`, `gemini-3.8-flash`), 1M+ token context windows, multimodal analysis, and embeddings.
- [TypeSafe](./reference/providers/typesafe.md) — Native System 1 probabilistic decision engine (`jev-latest`) for sub-50ms structured classifications.
- [Antigravity](./reference/providers/antigravity.md) — Enterprise model gateway with managed thinking effort tiers.
