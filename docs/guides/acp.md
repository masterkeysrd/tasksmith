# Run TaskSmith in an editor

Install `tasksmith` on the editor’s `PATH` and configure a [model provider](../reference/providers.md) in TaskSmith. The editor launches `tasksmith acp` as a subprocess; it does not launch `daemon serve` or connect to an HTTP endpoint. Standard output is reserved for protocol messages. If the editor cannot find the executable, use the path printed by `command -v tasksmith` in its launch settings.

## Zed

Merge this entry into your Zed settings, then select **TaskSmith** when starting a new external-agent thread:

```json
{
  "agent_servers": {
    "TaskSmith": { "type": "custom", "command": "tasksmith", "args": ["acp"] }
  }
}
```

## JetBrains AI Assistant

In AI Chat choose **Add Custom Agent** and add the following agent to `acp.json`. Disable **Pass custom MCP servers** and **Pass IntelliJ MCP server** for this agent: TaskSmith uses the workspace’s [daemon-managed MCP servers](./tools-and-mcp.md), not servers supplied by the IDE. Select TaskSmith in the chat mode selector.

```json
{
  "agent_servers": {
    "TaskSmith": { "command": "tasksmith", "args": ["acp"] }
  }
}
```

## Neovim

For **codecompanion.nvim**, add a custom adapter under `adapters.acp` in your plugin setup. Select `tasksmith` as the chat adapter:

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

For **avante.nvim**, select a named ACP provider instead of an HTTP provider:

```lua
require("avante").setup({
  provider = "tasksmith",
  acp_providers = { tasksmith = { command = "tasksmith", args = { "acp" } } },
})
```

## Check the connection

Send a prompt, then open the TUI in the same project and look in `/sessions`: both clients use the daemon journal. An editor that supports loading sessions can replay the conversation after restarting. Workspace slash commands run on the daemon; `/compact [focus]` requests compaction. Use the editor’s mode or model selector when available.

Permission prompts can allow once, grant a displayed pattern for the session or project, or reject. Dismissing a permission prompt does not grant access. The “always reject” option saves the displayed pattern as a project deny rule. Structured questions and plan reviews open an editor form when the editor advertises `elicitation.form`. Otherwise TaskSmith cancels the turn and suggests retrying in the TUI.

Load a session to replay its history, or resume it without replay. Closing a session cancels its active turn but keeps the journal; deleting it removes the daemon session and history. Session lists are paginated. Editors can send audio, images, or embedded context and select the mode, model, thought level, and agent. During a turn, TaskSmith reports context usage, priced usage in USD, and changes to the session title or activity time.

Editors that advertise the corresponding capabilities also receive compaction summaries, diagnostic and quota notices, named plans, and child-session activity. Other editors still receive ordinary tool calls and todo plans. If a child needs permission or a form, editors without child-session views receive the request on the parent session; the answer still goes to the child. When the editor cannot display a required form, TaskSmith cancels the child turn rather than leaving it waiting. TaskSmith does not use the editor’s filesystem or terminal APIs: tools run under daemon sandbox and shell policy. Configure MCP in the workspace and provider credentials through `/login` or the provider environment, not ACP authentication. See the [CLI reference](../reference/cli.md) for socket selection.
