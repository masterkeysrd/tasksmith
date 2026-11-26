# CLI Reference

TaskSmith connects terminal, script, and editor clients to the Brain daemon. Choose a reference by where you enter the command: shell flags configure a client invocation, while slash commands and keyboard shortcuts act inside a running session.

## Reference pages

| To look up | Reference |
| :--- | :--- |
| Shell commands, arguments, defaults, and environment variables | [Flags and subcommands](./cli/flags-and-subcommands.md) |
| REPL commands, aliases, arguments, and interactive views | [Slash commands](./cli/slash-commands.md) |
| Keys for the composer, pickers, approvals, and editors | [Keyboard shortcuts](./cli/keybindings.md) |
| Appearance schemas, workspace data, logs, and socket paths | [Configuration and state files](./cli/config-files.md) |
| Headless input, output, NDJSON control frames, and exit codes | [Headless execution](./exec.md) |
| HTTP methods, paths, response fields, and errors behind CLI views | [Daemon HTTP API](./daemon-api.md) |

## Shell entry points

| Invocation | Use |
| :--- | :--- |
| `tasksmith` or `tasksmith repl` | Open the interactive terminal client. |
| `tasksmith -c` | Continue the most recently updated top-level workspace session. |
| `tasksmith exec [prompt...]` or `tasksmith -p [prompt...]` | Run without the terminal UI. |
| `tasksmith acp` | Serve editor sessions over JSON-RPC on stdio. |
| `tasksmith daemon` | Start, serve, stop, or inspect the background daemon. |
| `tasksmith auth` | Sign in, check provider authentication, or sign out. |
| `tasksmith version` | Print the client version and, if reachable, the daemon version. |

## Inside the REPL

| Input | Use |
| :--- | :--- |
| `/help` | Browse commands; type `/` to see completions. |
| `/sessions` | Switch, rename, fork, or delete a session. |
| `/mode` or `Shift+Tab` | Inspect or change the session mode. |
| `/usage` and `/stats` | Inspect cost and quotas (`/usage`) or inference speed (`/stats`). |
| `@` | Complete a workspace file or directory reference in the composer. |
| `!` on empty input | Enter direct shell-command mode. |

## View and schema lookup

### Providers Browser (`/providers`)

[Provider browser](./cli/slash-commands.md#providers-browser-providers)

### Provider Model Catalog

[Provider model catalog](./cli/slash-commands.md#provider-model-catalog)

### Model Alias Editor (`F2`)

[F2 alias editor](./cli/slash-commands.md#model-alias-editor-f2)

### Provider Sign-In (`/login`)

[Provider sign-in](./cli/slash-commands.md#provider-sign-in-login)

### MCP Browser (`/mcp`)

[Server browser](./cli/slash-commands.md#mcp-browser-mcp)

### MCP Server Overview

[Overview fields and actions](./cli/slash-commands.md#mcp-server-overview)

### MCP Tool Drill-Down

[Tool list and detail](./cli/slash-commands.md#mcp-tool-drill-down)

### MCP Authentication

[Authentication](./cli/slash-commands.md#mcp-authentication)

### MCP Item Confirmation

[Item confirmation](./cli/slash-commands.md#mcp-item-confirmation)

### MCP HTTP Endpoints

[HTTP endpoints](./daemon-api.md#mcp-http-endpoints)

### Sandbox Editor (`/sandbox`)

[Sandbox editor](./cli/slash-commands.md#sandbox-editor-sandbox)

### Tools Browser (`/tools`)

[Tool browser](./cli/slash-commands.md#tools-browser-tools)

### Changes Browser (`/changes`)

[Changes browser shortcuts](./cli/keybindings.md#changes-browser-changes)

### Status Line Fields

[Configuration fields and segments](./cli/config-files.md#status-line-fields)

### Warnings View (`/warnings`)

[Warnings view](./cli/slash-commands.md#warnings-view-warnings)

## See Also

- [CLI guide](../guides/cli.md) — start an interactive session and use the composer.
- [Sessions guide](../guides/sessions.md) — resume or branch a conversation.
- [Appearance guide](../guides/appearance.md) — customize the theme and status line.
- [ACP guide](../guides/acp.md) — connect an editor to TaskSmith.
