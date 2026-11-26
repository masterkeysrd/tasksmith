# Run TaskSmith in your editor

Connect Zed, JetBrains AI Assistant, or Neovim to TaskSmith over Agent Client Protocol (ACP) v1.
Your editor hosts the conversation; the same local daemon that backs the terminal runs the tools and saves the session.

## Before you start

Install TaskSmith and configure a [model provider](../reference/providers.md).
Check the executable your editor needs to launch:

```bash
command -v tasksmith
```

The launch command is `tasksmith acp`, **not** `tasksmith daemon serve` or an HTTP endpoint.
The editor starts it as a subprocess and uses standard input and output for JSON-RPC messages.
Don't add a wrapper that writes a greeting or diagnostic to stdout.
If your editor doesn't inherit your shell's `PATH`, use the path printed above as its command.

## Connect Zed

Merge this entry into your Zed settings:

```json
{
  "agent_servers": {
    "TaskSmith": { "type": "custom", "command": "tasksmith", "args": ["acp"] }
  }
}
```

Start a new external-agent thread and select **TaskSmith**.

## Connect JetBrains AI Assistant

In AI Chat choose **Add Custom Agent**. Add this agent to `acp.json`:

```json
{
  "agent_servers": {
    "TaskSmith": { "command": "tasksmith", "args": ["acp"] }
  }
}
```

Disable **Pass custom MCP servers** and **Pass IntelliJ MCP server** for this agent.
TaskSmith uses [daemon-managed MCP servers](./tools-and-mcp.md), not servers supplied by the IDE.
Select **TaskSmith** in the chat mode selector.

## Connect Neovim

For **codecompanion.nvim**, put this adapter under `adapters.acp` in the plugin setup,
then select `tasksmith` as your chat adapter:

```lua
adapters = {
  acp = {
    tasksmith = function()
      return {
        name = "tasksmith", formatted_name = "TaskSmith", type = "acp",
        roles = { llm = "assistant", user = "user" },
        commands = { default = { "tasksmith", "acp" } },
        defaults = { mcpServers = {}, timeout = 20000 },
        parameters = { protocolVersion = 1, clientCapabilities = {} },
        handlers = {
          lifecycle = { setup = function() return true end, auth = function() return true end },
          request = { build_messages = require("codecompanion.adapters.acp.helpers").build_messages },
        },
      }
    end,
  },
}
```

For **avante.nvim**, select a named ACP provider rather than an HTTP provider:

```lua
require("avante").setup({
  provider = "tasksmith",
  acp_providers = { tasksmith = { command = "tasksmith", args = { "acp" } } },
})
```

## What works over ACP v1

| To… | Use your editor to… |
| --- | --- |
| Continue a conversation | Browse paginated sessions. **Load** replays history; **resume** attaches without replay. |
| Leave or remove a session | **Close** cancels its active turn but keeps the journal. **Delete** removes the session and history. |
| Guide a turn | Send text, images, audio, or embedded context. Use the available selectors for mode, model, thought level, and agent. |
| Manage context | Run workspace slash commands on the daemon, or send `/compact` with optional focus instructions. |
| Monitor a run | Watch context usage, priced usage in USD, session title, and activity time. |

Permission prompts let you allow once, grant a displayed pattern for the session or project,
or reject. Dismissing the prompt does **not** grant access. **Always reject** saves the
shown pattern as a project deny rule.

Structured questions and plan reviews need the editor's `elicitation.form` capability.
Without it, TaskSmith cancels the turn and suggests retrying in the TUI rather than waiting indefinitely.

Editors advertising additional capabilities receive compaction summaries, diagnostics,
quota notices, named plans, and child-session activity. Other editors still receive ordinary
tool calls and todo plans. Without a child-session view, a child's permission request or form
appears on the parent; the reply still goes to the child. If a required form cannot be shown,
TaskSmith cancels the child turn.

Tools run under daemon sandbox and shell policy, not the editor's filesystem or terminal APIs.
Configure credentials with `/login` or the provider environment, not ACP authentication.

## Verify the connection

1. Open a project in your editor and send `Summarize the README without changing files.`
2. In a terminal in that same project, run `tasksmith` and open `/sessions`.
3. Find the editor conversation. If your editor supports loading sessions, restart it,
   load that session, and check that the earlier exchange reappears.

If no session appears, check the editor's working directory and executable first.
For a non-default daemon socket, consult the [socket and workspace flags](../reference/cli/flags-and-subcommands.md).

## Next steps

- [Resume or branch an editor conversation in the TUI](./sessions.md).
- [Choose a permission mode](./modes-and-planning.md) before requesting changes.
- [Look up ACP launch flags](../reference/cli/flags-and-subcommands.md).
