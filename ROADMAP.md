# Roadmap

High-level overview of TaskSmith's direction and release milestones. Plans and priorities are tentative and subject to change as architecture and user workflows evolve.

---

## 0.0.3 — "The Daemon Rewrite"

Ground-up architectural rewrite around a headless local daemon, OS-level sandboxing, and a scrollback-native reactive CLI.

- [x] **Client-daemon architecture**: on-demand background daemon over Unix domain sockets with multiplexed SSE streaming, cursor-based event replay, and automatic version-skew handling
- [x] **Inline reactive CLI**: scrollback-native REPL built on `kite` (replacing the legacy full-screen TUI), with configurable status line segments, live turn progress, `/stats` inference metrics, and session exit summaries
- [x] **OS kernel sandboxing & `/sandbox` settings**: per-session native isolation (macOS Seatbelt, Linux Bubblewrap), host credential masking, workspace path self-protection, unsandboxed command exclusions, and interactive `/sandbox` configuration (`sandbox.json`)
- [x] **Shell AST inspection & layered permissions**: `shellguard` command analysis, interactive permission rules (`permissions.json`), untrusted repository gating (`/setup`), and provenance-isolated subprocess environments
- [x] **Transactional file journal & timeline**: SQLite-backed turn durability and pre-mutation file snapshots with unified diff inspection (`/changes`) and instant rollback (`/revert`)
- [x] **Multi-provider model gateway**: OpenAI (Responses & Chat), Anthropic, Google GenAI, Ollama, TypeSafe, and Antigravity with automatic prompt caching, `@alias` catalog resolution, and browser OAuth sign-in
- [x] **Subagent delegation & deferred tool discovery**: isolated subagent execution (`invoke_agent`, git worktrees), on-demand `tool_search`, skill-activated deferred tools, and explicit custom tool contracts
- [x] **Declarative 4-tier WARP workspaces & MCP**: hierarchical resource resolution (`WORKSPACE.md`, `AGENTS.md`, `.agents/`), ambient and on-demand rules, skills (`/skills`), and Model Context Protocol (MCP) server integration
- [x] **Release packaging & versioning**: cross-platform GoReleaser builds (`darwin`/`linux`, `amd64`/`arm64`), version stamping (`--version`, `tasksmith version`), and Apache 2.0 licensing
- [x] **Subprocess network allowlisting**: per-domain outbound proxy enforcement and interactive network approvals for sandboxed child processes
- [x] **Subscription quota tracking & enriched `/model` picker**: live SSE quota updates, adaptive status line countdowns, `/usage` quotas tab, and grouped `/model` pricing and context metadata
- [x] **Subagent model tiers & discovery**: governed `@fast`, `@balanced`, `@smart`, `@flagship`, and `@inherit` routing enforced against `spec.models` globs, plus `manage_agents` model search
- [x] **Resolved subagent models**: see the actual model in the activity bar, `invoke_agent` results, `/subagents` rows, and the live inspector.
- [x] **CI/CD & `main` cutover**: GitHub Actions verification, CodeQL, docs deployment, and GoReleaser workflows alongside promotion of the `v2` architecture to `main`
- [x] **Unified session modes & planning**: decoupled permission gates and behavioral postures (`default`, `accept_edits`, `plan`, `ask`, `auto`), safe `<session-dir>/plans/` storage, `enter_plan_mode` / `exit_plan_mode`, and `/plan`
- [x] **Design interviews (`/grill-me`)**: verify repository facts, settle design choices in dependency order, and confirm the decision summary before planning or implementation.
- [x] **Mode-aware pending approvals**: widening a mode resolves eligible pending permissions without granting rules or approving unsafe requests; eligible write prompts can switch directly to `accept_edits`.
- [x] **Headless, bidirectional stdio, & SDK execution**: one-shot and bidirectional streaming `tasksmith exec` / `-p` modes (`text`, `json`, and 1:1 `api/` `jsonl` over `stdin`/`stdout`) plus Go `client` SDK lifecycle helpers for CI, pipelines, and programmatic integrations
- [ ] **Agent Client Protocol (`tasksmith acp`)**: JSON-RPC 2.0 stdio adapter over the shared local daemon for Zed, JetBrains, and Neovim with live TUI session sharing, tool diffs, and interactive permission prompts
- [x] **Quick session resume & forking**: `-c` / `--continue` flag for resuming the latest workspace session and `/fork` conversation branching
- [x] **Multimodal input**: content-addressed `<session-dir>/media/` storage with late gateway hydration, image/PDF/audio ingestion in `view` (with PDF `pages`), `@` media mentions, clipboard paste, and model-switch capability confirmation
- [x] **Built-in `web_search` & CLI `auth`**: host-mediated web search tool and `tasksmith auth login|status|logout` subcommands
- [x] **WARP-managed `/providers` & `spec.enabled`**: interactive `/providers` browser with model manager, catalog, and add flow, inline `F2` alias editor shared with `/model`, and manifest-driven `spec.enabled` for providers with workspace shadow overrides
- [x] **Deterministic tool & MCP availability**: `spec.enabled` on Tool/MCP manifests plus `WORKSPACE.md` `spec.policies.tools.exclude` / `spec.policies.mcps.exclude` as the only availability switches (legacy `permissions.json` disabled lists removed), with `[Disabled]` listings and in-place toggling in `/tools` and `/mcp`
- [x] **SmartReplace in `edit`**: exact-first matching with conservative fallbacks for line endings, whitespace, indentation, and minor typos; preserves diff previews and file-change checks.
- [x] **ChatGPT Codex & live model catalogs** — Sign in with a ChatGPT plan and browse the models available to that account in `/providers`. The gateway uses live catalogs where providers expose them. OpenAI, Google GenAI, and Antigravity presets include updated models and tier aliases. [Codex setup](./docs/reference/providers/codex.md)
- [x] **Claude Code & GitHub Copilot providers**: subscription-backed `claudecode` and `github-copilot` completion via Loom, with browser sign-in, built-in presets, four model-tier aliases, and provider-reported quota. Copilot requires a CLI that speaks protocol 4, such as `1.0.114`. [Claude Code setup](./docs/reference/providers/claudecode.md) · [Copilot setup](./docs/reference/providers/github-copilot.md)

---

## 0.0.4 — "Workspace Intelligence"

- **Lifecycle hooks**: deterministic `PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `Stop`, and `SubagentStop` hooks gated by repository trust
- **LSP diagnostics & code intelligence**: automatic post-edit compiler/typecheck feedback, symbol navigation tools, and live `@sym:` mention resolution
- **Unified rewind**: `/rewind` timeline picker pairing conversation graph checkpoints with file journal snapshots
- **Workspace auto-notes**: private per-workspace notes (`MEMORY.md`, `/memory`, `/learn`) separate from git-tracked `AGENTS.md`
- **Plugin lifecycle & lockfile hardening**: `tasksmith plugin` CLI subcommands, interactive `/plugins` browser, and strict `warp.lock` digest verification
- **Configurable commit & PR attribution**: workspace and global preferences for git trailers
- **Dynamic capability discovery**: host and sandbox tool probes (`git`, `rg`, `gh`) exposed to WARP prompt and tool templates
- **Programmatic tool orchestration ("Code Mode")**: embedded, zero-ambient-authority JavaScript tool-chaining runtime (`goja`) in the daemon so any provider/model can batch, filter, and loop across built-in and MCP tools in a single turn while enforcing per-call permissions, file journaling, and multi-client (`TUI`, `exec`, `acp`) event streaming
- **Memory footprint & idle eviction**: cold-session `ListSessions` projection, idle session unloading, sandbox/worktree-isolated lazy file indexing, and CLI scrollback memory trimming

---

## Future (unknown release)

- **Prepare for 1.0**: stabilize the daemon HTTP/SSE wire protocol (`api/`) and WARP manifest schemas
- **Agent memory**: episodic and semantic long-term recall across sessions so agents retain past decisions, architectural context, and learned conventions
- **Agent swarms**: coordinated multi-agent execution enabling peer and specialist agents to collaborate in parallel across isolated worktrees with shared messaging
- **Workflow orchestrator**: declarative multi-step pipelines combining prompts, tools, `decision` gates, and subagents/swarms into repeatable automated runs
- **Remote broker & daemon mesh**: control-plane relay that daemons running anywhere (Docker, EC2, ECS, remote hosts) connect to, letting clients discover, attach to, and switch across multiple remote daemons
- **Centralized agentic gateway**: broker-hosted multi-key rotation, fallback routing, and OpenAI-, Anthropic-, and Ollama-compatible API endpoints so remote daemons stay free of raw provider credentials
- **Webhook-driven dispatch**: inbound webhook receivers on the broker that trigger workflows and spin up background work across connected daemons
- **Active System 1 (`decision`) & System 3 (`embedding`) loops**: low-latency probabilistic routing/classification and local semantic codebase indexing over the Triple-Role Gateway
- **Multi-client ecosystem**: native Desktop, full-screen TUI, and Web clients backed by the shared local or remote daemon

---

## Completed

### 0.0.2 — "Workflow Polish"

- Interactive `ask_question` clarification widget and workflow agents
- Session picker modal with fuzzy search and preview, plus `<C-r>` prompt history search
- Permissions manager modal, multi-action `bash` authorization prompts, and unset environment variable checks
- Agent turn cancellation and retry for failed or interrupted runs
- Multi-line composer input, subcommand/argument autocompletion, and toggleable sidebar

### 0.0.1 — "Initial Prototype"

- Single-process terminal coding agent with full-screen chat TUI, streaming metrics sidebar, and colorscheme engine
- Core built-in toolset (`view`, `write`, `edit`, `multi_edit`, `rm`, `ls`, `glob`, `grep`, `bash`, `web_fetch`, `web_search`, `todos`)
- Initial WARP workspace discovery, trust/import onboarding, `@` file attachments, and MCP client support
- Context compaction, `:thinking` effort controls, subagent invocation, and initial LSP diagnostics
