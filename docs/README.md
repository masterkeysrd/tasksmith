# TaskSmith Documentation

Start with a first session, choose a workflow, or look up a command or manifest field.

## Get Started

Install TaskSmith, run your first session, then set up a project.

- [Quickstart](./guides/quickstart.md)
- [Workspaces & Projects](./guides/workspaces.md)

## Core Workflows

Choose how to work: in the terminal, from an editor, or in a pipeline.

- [Interactive Terminal (TUI)](./guides/cli.md)
- [Sessions, Diffs & Usage](./guides/sessions.md)
- [Modes & Planning](./guides/modes-and-planning.md)
- [Connect an Editor (ACP)](./guides/acp.md)
- [Headless & CI Automation](./guides/headless-exec.md)

## Security & Governance

Decide which projects to trust, when tools need approval, and where commands can run.

- [Workspace Trust](./guides/trust.md)
- [Permissions & Rules](./guides/permissions.md)
- [Auto-Mode Permission Review](./guides/auto-permission-review.md)
- [Sandboxing](./guides/sandbox.md)

## Customize & Extend

Adapt TaskSmith to your team’s models, instructions, tools, and terminal.

- [Customization Overview](./guides/customizations.md)
- [Models & Providers](./guides/models-and-providers.md)
- [Agents & Subagents](./guides/agents-and-subagents.md)
- [Skills & Rules](./guides/skills-and-rules.md)
- [Slash Commands & Workflows](./guides/commands.md)
- [Tools & MCP Servers](./guides/tools-and-mcp.md)
- [Appearance & Status Line](./guides/appearance.md)

## Reference Specifications

Look up exact flags, shortcuts, tool parameters, and configuration fields.

### CLI & Daemon

- [CLI Overview](./reference/cli.md)
- [Flags & Subcommands](./reference/cli/flags-and-subcommands.md)
- [Slash Commands](./reference/cli/slash-commands.md)
- [Keyboard Shortcuts](./reference/cli/keybindings.md)
- [Config Files & State](./reference/cli/config-files.md)
- [Headless Execution (exec)](./reference/exec.md)
- [Daemon HTTP API](./reference/daemon-api.md)

### Built-in Tools

- [Tools Overview](./reference/tools.md)
- [Filesystem Tools](./reference/tools/filesystem.md)
- [Execution & Tasks](./reference/tools/execution-and-tasks.md)
- [Web & Network](./reference/tools/web.md)
- [Agents, Planning & Input](./reference/tools/agents-and-planning.md)
- [Workspace Resources](./reference/tools/workspace-resources.md)

### Resource Manifests (WARP)

- [Manifests Overview](./reference/manifests.md)
- [Workspace & Context](./reference/manifests/workspace.md)
- [Agent](./reference/manifests/agent.md)
- [Skill](./reference/manifests/skill.md)
- [Rule](./reference/manifests/rule.md)
- [Command](./reference/manifests/command.md)
- [Decision](./reference/manifests/decision.md)
- [Tool](./reference/manifests/tool.md)
- [MCP Server](./reference/manifests/mcp.md)
- [Plugin](./reference/manifests/plugin.md)

### Model Providers

- [Providers Overview & Schema](./reference/providers.md)
- [OpenAI](./reference/providers/openai.md)
- [Codex](./reference/providers/codex.md)
- [Anthropic](./reference/providers/anthropic.md)
- [Claude Code](./reference/providers/claudecode.md)
- [GitHub Copilot](./reference/providers/github-copilot.md)
- [Google GenAI](./reference/providers/google-genai.md)
- [Ollama](./reference/providers/ollama.md)
- [TypeSafe](./reference/providers/typesafe.md)
- [Antigravity](./reference/providers/antigravity.md)

## System Architecture

Follow a request through the daemon and its security boundaries.

| Subsystem | Focus Area |
| :--- | :--- |
| [Daemon Architecture](./architecture/daemon.md) | Background service lifecycle, Unix domain socket IPC, SSE event multiplexing, and SQLite turn journaling. |
| [Agent Execution Harness](./architecture/harness.md) | Directed execution graph, interactive mid-turn suspension, context compaction, subagent delegation, and ambient rule/skill injection. |
| [Provider Gateway](./architecture/gateway.md) | Content-addressable gateway pooling, role-based model routing (`completion`, `decision`, `embedding`), and lazy auth validation. |
| [Virtualized Sandbox](./architecture/sandbox.md) | OS capability decomposition (`FS`, `Proc`, `Net`), local process groups, and native kernel isolation (Seatbelt/Bubblewrap). |
| [Permission & Guard Engine](./architecture/permissions.md) | Multi-stage security pipeline, shell Abstract Syntax Tree (AST) inspection, and path confinement. |
