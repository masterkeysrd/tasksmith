# CLI Command & Keybinding Reference

The TaskSmith command-line interface provides access to the interactive terminal REPL, background Brain daemon management, session orchestration, and built-in slash commands.

---

## 1. CLI Commands & Global Flags

### Root Invocation: `tasksmith`

Invoking `tasksmith` without a subcommand automatically starts the interactive terminal REPL attached to the Brain daemon:

```bash
tasksmith [flags]
```

### Subcommands

| Command | Usage | Description |
| :--- | :--- | :--- |
| **`repl`** | `tasksmith repl [flags]` | Starts the interactive terminal REPL (identical to invoking `tasksmith` directly). |
| **`daemon serve`** | `tasksmith daemon serve [flags]` | Runs the Brain daemon server in the foreground. |
| **`daemon start`** | `tasksmith daemon start` | Spawns and detaches the Brain daemon process in the background. |
| **`daemon stop`** | `tasksmith daemon stop` | Gracefully terminates a running Brain daemon via SIGTERM / shutdown API. |
| **`daemon status`** | `tasksmith daemon status` | Checks daemon process health, IPC socket connectivity, and active workspace runtimes. |

### Global Flags

The following flags apply to `tasksmith`, `tasksmith repl`, and `tasksmith daemon`:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--workspace <path>` | `-w` | `.` (current directory) | Path to the target workspace root directory. Discovers `WORKSPACE.md` and `.agents/`. |
| `--socket <path>` | | `/tmp/tsd-<uid>/b.sock` | Explicit path to the Brain daemon Unix domain socket. |
| `--session <id>` | | *(none)* | UUIDv7 of an existing session to resume immediately. |
| `--agent <name>` | | `main` | Default agent persona to activate for newly initialized sessions. |
| `--model <id>` | | Provider default | Upstream model identifier (e.g., `openai/gpt-5.5` or `ollama/qwen3.8`). |
| `--title <title>` | | *(none)* | Optional human-readable title assigned to a newly created session. |
| `--help` | `-h` | | Print help and usage information for any command. |

### Daemon Flags

| Flag | Subcommand | Default | Description |
| :--- | :--- | :--- | :--- |
| `--lock <path>` | `daemon serve` | `/tmp/tsd-<uid>/daemon.lock` | Override path for the daemon process lockfile. |

---

## 2. Interactive Slash Commands

Inside the TaskSmith interactive REPL, slash commands provide instant control over session state, permission modes, thinking, and resource inspection. The session status shows the model resolved by the daemon and the thinking that model will apply when nothing overrides it. When no thinking default is known, the UI shows `provider default` and leaves the choice to the provider:

| Slash Command | Aliases | Arguments | Description |
| :--- | :--- | :--- | :--- |
| **`/help`** | `/h`, `/?` | *(none)* | Opens the interactive slash command browser and help viewer. |
| **`/mode`** | | `[mode]` (`default`, `accept_edits`, `plan`, `ask`, `auto`) | Inspects current permission mode or switches to the specified mode. |
| **`/model`** | | `[model_id]` | Switches to `model_id`, or opens the interactive model picker if no argument is provided. See [Model Picker](#model-picker-model). |
| **`/agent`** | | `[agent_name]` | Displays active agent, switches to `agent_name`, or opens the interactive agent picker if no argument is provided. |
| **`/effort`** | | `[level\|default]` | Set an allowed model-specific effort or open the picker, which shows available descriptions. |
| **`/budget`** | | `[tokens\|default]` | Show or set a supported thinking token budget. |
| **`/thinking`** | | `[on\|off\|adaptive\|default]` | Show or set a supported thinking toggle or adaptive strategy. |
| **`/theme`** | | `[theme_name]` | Changes terminal UI color theme, or opens the interactive theme picker if no argument is provided. |
| **`/clear`** | | *(none)* | Creates and transitions to a fresh draft session. |
| **`/exit`** | `/quit`, `/q` | *(none)* | Gracefully terminates and exits the interactive REPL. |
| **`/new`** | | `[model]` | Creates and transitions to a fresh draft session, optionally overriding the model. |
| **`/sessions`** | `/session`, `/ls` | `[session_id]` | Switches to `session_id`, or opens the interactive session browser to switch, rename, or delete sessions. |
| **`/changes`** | | *(none)* | Opens the interactive changes browser to inspect tracked file modifications, diffs, and hashes. |
| **`/revert`** | | `[path] [entry_id]` | Reverts uncommitted file mutations for a specific file, change entry, or the entire session. |
| **`/permissions`** | `/perm`, `/perms` | *(none)* | Opens the interactive permissions browser to inspect and edit project and global allow/deny rules. |
| **`/tasks`** | | *(none)* | Opens the background task manager to inspect running commands and stream task logs. |
| **`/subagents`** | `/subagent` | *(none)* | Opens the interactive subagent browser to inspect running and completed subagents, view live outputs, or terminate tasks. |
| **`/compact`** | | `[focus]` | Compacts conversation history to free context window space, with optional focus instructions. Requires pending permissions and questions to be resolved first. |
| **`/context`** | | *(none)* | Displays visual token usage grid and detailed context window breakdown by category. |
| **`/usage`** | | `[session\|workspace\|global]` | Opens the usage browser slot on the Session, Workspace or Global tab (default: Session). See [Usage Browser](#usage-browser-usage). |
| **`/stats`** | | `[session\|workspace\|global]` | Opens the inference speed browser slot on the Session, Workspace or Global tab (default: Session). See [Stats Browser](#stats-browser-stats). |
| **`/skills`** | | *(none)* | Opens the interactive skills browser to inspect runbooks, token estimates, and reload workspace skills. |
| **`/rules`** | | *(none)* | Opens the interactive rules browser to inspect rule status, scope, triggers, and token estimates, and to reload workspace rules. |
| **`/tools`** | | *(none)* | Opens the interactive tools browser to inspect tool status, scope, safety annotations, and JSON schemas, and to reload workspace tools. See [Tools Browser](#tools-browser-tools). |
| **`/mcp`** | `/mcps` | `[auth <name>\|restart <name>]` | With no subcommand, opens the interactive MCP browser to inspect servers, run prompts, attach resources, authorize, and restart. `auth` and `restart` act on one named server after a confirmation. See [MCP Browser](#mcp-browser-mcp). |
| **`/setup`** | | *(none)* | Opens the interactive workspace onboarding and configuration wizard to reconfigure providers, scopes, permissions, tools, or MCP servers. |
| **`/statusline`** | `/status` | *(none)* | Opens the interactive status line editor to toggle, reorder, and compact the status line segments with a live preview; saves to `statusline.json`. See [Status Line Fields](#status-line-fields). |
| **`/init`** | | *(none)* | Invokes the built-in `system:init` command to inspect codebase conventions and interactively generate or update `AGENTS.md`. |
| **`/workspace`** | `/ws` | `[reload]` | Reloads the active workspace resources from disk. With no subcommand, defaults to `reload`. |
| **`/daemon`** | | `[status\|restart\|start\|stop]` | Inspects or manages the background Brain Daemon. With no subcommand, opens the interactive daemon browser. |

### Model Picker (`/model`)

The picker lists the models the daemon reports as able to serve chat completion. Models that declare `completion: false` — embeddings, moderation, and similar — are left out, and the daemon rejects them for a session with `model "<id>" does not support chat completion`, so a session can never end up on a model it cannot converse with. A model that reports no completion capability at all is treated as chat-capable.

Each row shows:

| Column | Content |
| :--- | :--- |
| Label | Model label, falling back to its name and then its id. |
| Description | Provider, then the model's workspace aliases rendered as `@name`. |
| Current | The row of the model the session is actually running is suffixed `current`. |

- **Active model before a choice**: a fresh draft session has no model of its own, yet it already resolves to the workspace or agent default. That resolved model is marked `current`, and a session configured with an alias is marked on the model the alias resolves to.
- **Search**: the filter box matches the label, description, and id, so typing an alias name narrows the list to the model it names. `/model` argument autocomplete matches aliases the same way and inserts the canonical model id.
- **Nothing chat-capable**: the picker closes and reports `No chat-capable models available.`

### Thinking Controls (`/effort`, `/budget`, `/thinking`)

Each command is available only when the resolved model declares the strategy it needs, so the same command can be usable on one model and refuse on another.

| Command | Requires |
| :--- | :--- |
| `/effort [level\|default]` | An `effort` strategy. |
| `/budget [tokens\|default]` | A `budget` strategy. |
| `/thinking [on\|off\|adaptive\|default]` | A `toggle` strategy for `on`/`off`, or an `adaptive` strategy for `adaptive`. The command needs at least one of the two. |

- **State without an argument** reports the session override if there is one, then the model's own default as `default (<strategy>)`, then `provider default` when nothing declares a default. `/budget` adds the range it accepts, e.g. `Thinking budget: default (500 tokens) — range 100–1000`.
- **Suggestions** list only the arguments the active model accepts, so an adaptive-only model never offers `on`/`off`.
- `default` clears the session override and returns to the agent or model default. Setting any control replaces the previous thinking override. See [Providers](providers.md#thinking-capability-fields) for how strategies and defaults are declared.

### Workspace Commands, Skills & Plugin Extensions

In addition to built-in commands, TaskSmith dynamically registers all workspace, user, and plugin resources:

| Invocation Syntax | Source | Description |
| :--- | :--- | :--- |
| **`/<command> [args...]`** | `.agents/commands/` | Executes a workspace command prompt template with arguments. |
| **`/<skill> [args...]`** | `.agents/skills/` | Renders a skill runbook into the session prompt with arguments. |
| **`/<plugin>/<command> [args...]`** | Plugin resources | Invokes a namespaced command contributed by an installed plugin or extension. |

- **Slash Autocomplete**: Typing `/` shows commands and available skills, interleaved in registration order. Type a name to filter the suggestions.
- **Argument Autocomplete & Hints**: Typing `/<name> ` presents autocomplete suggestions and highlights argument requirements (`<required>`, `[optional=default]`).
- **Pre-Flight Validation**: Missing required parameters are caught immediately client-side before sending any network request.
- **Automatic Default Population**: Arguments omitted by the user are automatically filled by the renderer from `spec.arguments[].default`.

### Skill & Rule Loading in the Transcript

Agent calls to `load_skill` show the skill name and loading, loaded, or failed status. Failures include the first error line below the status. Skill arguments and rendered instructions are not displayed in the widget.

Agent calls to `load_rule` render the same style of widget (`○ Loading Rule <name>` then `● Loaded Rule <name>`), with a truncation warning subline when the rule body exceeds size limits. When the harness injects a rule automatically, or a rule is attached with `@rule:`, a single muted transcript line is emitted instead:

```text
● Loaded Rule go-error-handling (matched internal/daemon/session/session.go)
● Loaded Rule bash-safety (tool: bash)
● Loaded Rule security-audit (@rule:security-audit)
```

The injected `<system_reminders>` body itself is never printed in the transcript.

### Inline Attachment References

Token prefixes typed anywhere in a prompt render as inline pill badges:

| Syntax | Rendering | Functional today |
| :--- | :--- | :--- |
| `@<token>` / `@file:<path>` | Pill with file/folder icon | **Yes** — triggers `@` autocomplete, inserts shortest unique workspace suffix (or explicit path), expands file or directory into `<system_reminders>`, supports optional `#L<start>[-L<end>]` line ranges. |
| `@rule:<name>` | Pill with shield icon | **Yes** — loads the rule, with `@rule:` autocomplete. |
| `@sym:<name>` | Pill with symbol-kind icon | Display only. |
| `@skill:<name>` | Pill with plugin icon | **Yes** — loads the skill into `<system_reminders>` and activates its deferred tools, with `@skill:` autocomplete (skills with arguments can also be invoked as `/skill <args>`). |

Typing `@` in the composer opens a fuzzy file and directory completion popup backed by the daemon's sandbox file index. Matching candidates display Nerd Font file/directory icons, parent directories, and `· recent` badges for session-accessed paths. Prohibited files (such as secret `.env` files or paths denied by permissions/policy) display a `[Prohibited]` badge; sending a prompt containing a prohibited or path-escaping mention is rejected with HTTP 403 Forbidden. Selecting a file or directory inserts its shortest unique slash-delimited workspace suffix (e.g. `@composer.go `, `@cli/app.go `, or `@docs/ `), quoting if the path contains whitespace. Explicit `@file:<path>` syntax is also supported. You can optionally specify a 1-based line range (e.g. `@composer.go#L10-L20` or `#L42`). During turn execution, the harness reads the file or directory via `std.View` / `std.List`, automatically tracks CAS reads, and injects the formatted contents into `<system_reminders>`. Raw prompt prose without attachments (e.g. pasted logs referencing `.env`) is never falsely rejected.

Typing `@rule:` or `@skill:` opens a completion popup listing available workspace rules or skills with their scope and description; selecting one inserts `@rule:<name> ` or `@skill:<name> `. Referenced rules and skills are injected immediately into `<system_reminders>` (and attached skills activate any policy-allowed deferred tools they declare); unknown names are ignored, and neither is injected twice within the same compaction epoch.

### MCP References (`/mcp:`, `@mcp:`)

Discovered MCP prompts and resources are invokable from the composer:

| Syntax | Autocomplete | Effect |
| :--- | :--- | :--- |
| `/mcp:<server>:<prompt>` | `/` slash menu | Renders the prompt on that server and submits the rendered messages as your turn. |
| `@mcp:<server>:<uri>` | `@` mention menu | Binds the resource as a structured attachment; the daemon reads it on turn execution and injects `<context_resource>` into `<system_reminders>` while keeping the `@mcp:` pill in your message. |

- **Position**: A reference is recognized anywhere in the message at a token boundary, so `explain @mcp:notes:notes://spec-1 briefly` works and the surrounding text is kept.
- **Multiple references**: Multiple allowed `@mcp:` resources can be attached in a single message (including alongside `@` file and `@rule:` mentions). Invoking more than one `/mcp:` prompt (or mixing interactive MCP prompts/confirmations) in one message is rejected with an error naming the count.
- **Unknown server**: Left as ordinary text and sent to the agent, so prose that happens to start with `/mcp:` is never hijacked. An unknown prompt or URI on a *known* server is an error pointing at `/mcp`.
- **Prohibited items**: Completion lists them labeled `[Prohibited]`. Invoking one opens the confirmation described in [MCP Item Confirmation](#mcp-item-confirmation); see [Tools & MCP Servers](../guides/tools-and-mcp.md) for what a policy prohibits.
- **Arguments**: A prompt that declares arguments collects them first in an `Arguments for /mcp:<server>:<prompt>` input prefilled with a JSON object template. A prompt with exactly one declared argument also accepts a bare value.

### MCP Browser (`/mcp`)

The browser opens as `Manage MCP servers` with a `<N> server` / `<N> servers` count line, then one muted heading per scope that has servers — `Project MCPs`, `Workspace MCPs`, `User MCPs`, `Builtin MCPs` — each naming the directory its manifests live in (for example `Workspace MCPs (.agents/mcps)` or `User MCPs (~/.config/tasksmith/agents/mcps)`). Servers are listed by scope tier, closest to the repository first, then by name. The list is deliberately not filterable, so the single-letter actions below always reach the highlighted server instead of typing into a search box.

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `Ctrl+P` / `Ctrl+N` | MCP Browser | Moves the highlighted server. |
| `Enter` / `i` | MCP Browser | Opens the overview for the highlighted server. |
| `a` | MCP Browser | Opens the [authorization screen](#mcp-authentication) for the highlighted server. |
| `r` | MCP Browser | Restarts (reconnects) the highlighted server. |
| `R` / `Ctrl+R` | MCP Browser | Reloads workspace resources from disk and refreshes the server list. |
| `Esc` | MCP Browser | Closes the browser and leaves `Exited /mcp command` in the transcript. |

Each row shows a status glyph in the cursor gutter, the server name, and a right-hand status summary:

| Status | Glyph | Right-hand summary |
| :--- | :--- | :--- |
| `connected` | `✔` green | Capability counts, e.g. `25 tools · 2 prompts · 3 resources` — `connected` when nothing was discovered, and `25 tools (24 allowed)` when a policy blocks some items. |
| `auth_required` | `⚠` yellow | `needs authentication` |
| `error` | `✘` red | `failed to connect`, or `failed to connect (<reason>)` with transport plumbing trimmed so the reason reads cleanly. |
| `connecting` | `⟳` cyan | `connecting…` |
| `disconnected` | `○` muted | `disconnected` |

With nothing configured, the view offers `R` / `Ctrl+R` to reload, which is what you need right after adding a manifest.

`/mcp auth <name>` and `/mcp restart <name>` run the same actions as `a` and `r` with no confirmation step: naming the server in the composer is already the decision. `/mcp auth <name>` opens the browser directly on that server's [authorization screen](#mcp-authentication); `/mcp restart <name>` opens nothing and reports in the conversation timeline, like `/daemon restart` and `/workspace reload`. A server that authenticates with static credentials instead of an authorization redirect gets a message naming the credential to provide — the environment variable, header, or credential file — because there is no browser flow to open; set it and run `/mcp restart <name>`.

### MCP Server Overview

Pressing `Enter` or `i` on a server replaces the list with that server's overview: a title line, aligned `Key: Value` metadata, and a numbered action menu.

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `Ctrl+P` / `Ctrl+N` | MCP Overview | Moves the menu cursor. |
| `Enter` | MCP Overview | Runs the highlighted menu entry. |
| `1` – `9` | MCP Overview | Runs the menu entry printed with that number. |
| `PgUp` / `PgDn` or `Ctrl+U` / `Ctrl+D` or `Ctrl+B` / `Ctrl+F` | MCP Overview | Scrolls the card by 8 lines. |
| `g` / `G` | MCP Overview | Scrolls the card to the top or bottom. |
| `p` | MCP Overview | Opens the prompt picker (same entry as `Run prompt`). |
| `u` | MCP Overview | Opens the resource picker (same entry as `Attach resource`). |
| `a` | MCP Overview | Opens the [authorization screen](#mcp-authentication) for this server, whenever the menu offers `Authenticate`. |
| `r` | MCP Overview | Restarts (reconnects) this server. |
| `Esc` | MCP Overview | Returns to the server list. |

Only the shortcuts that exist for the current server are advertised in the hint row, and letter shortcuts drop off that row when the terminal is too narrow to show all of them; they keep working either way, and the menu lists them.

The card shows a field only when it says something — a `stdio` server has no `URL`, a server with no authentication has no `Auth` row, and a server that connected cleanly has no `Error` row. A value too long for the terminal wraps under its own label instead of being clipped.

| Field | Value |
| :--- | :--- |
| `Status` | `✔ connected`, `⚠ needs authentication`, `✘ failed to connect`, `⟳ connecting…`, or `○ disconnected`, in the matching color. |
| `Auth` | Shown when the server declares an auth scheme or an authorization is outstanding: `✘ not authenticated (<authType>)`, `✔ authenticated (<authType>)`, or the bare `<authType>` for a static scheme. |
| `Command` / `URL` | The launch command, or the endpoint for a remote server. |
| `Transport` | `stdio`, `sse`, `http`, or `streamable-http`. |
| `Config location` | Home-shortened manifest path plus the scope badge, e.g. `~/.config/tasksmith/agents/mcps/github.mcp.yaml [User]`. |
| `Capabilities` | The kinds actually discovered (`tools · prompts · resources`), or `none`. |
| `Tools` / `Prompts` / `Resources` | Counts, calling out the split when a policy blocks some items, e.g. `25 tools (24 allowed, 1 prohibited)`. |
| `Authorize at` | The outstanding authorization URL, only while one is waiting. |
| `Error` | The failure reason with transport plumbing trimmed, so the row reads `connection refused` rather than the whole call chain. |

The menu is context-aware, so its first entry answers the most urgent question about the server:

| Server state | Menu entries |
| :--- | :--- |
| `auth_required` | `Authenticate`, `Reconnect`, `Back to servers` |
| `connected` or items discovered | `View tools (<N>)`, `Run prompt (<N>)`, `Attach resource (<N>)` — each shown only when that kind has items — then `Authenticate` for an OAuth server, then `Reconnect` |
| Failed with nothing discovered | `Reconnect` |

### MCP Tool Drill-Down

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `Ctrl+P` / `Ctrl+N` | Tool List | Moves the selection. |
| any text | Tool List | Filters tools by name. |
| `Enter` | Tool List | Opens the highlighted tool's detail. |
| `Esc` | Tool List | Returns to the overview. |
| `↑` / `↓` or `Ctrl+P` / `Ctrl+N` | Tool Detail | Scrolls. |
| `PgUp` / `PgDn` or `Ctrl+U` / `Ctrl+D` or `Ctrl+B` / `Ctrl+F` | Tool Detail | Scrolls by 8 lines. |
| `g` / `G` | Tool Detail | Scrolls to the top or bottom. |
| `e` | Tool Detail | Permits this tool in its manifest, shown only for a prohibited tool whose manifest can be rewritten. |
| `Esc` | Tool Detail | Returns to the tool list. |

`View tools` opens a filterable list titled `<server> — tools (<N>)`, one row per tool with a `✔` or `✘` policy glyph, the tool's own name, its badges, and a compact description. `Enter` on a tool opens its detail: `Tool name`, `Full name` (the qualified name the agent calls it by), and `Policy` — the verdict with its badges inline, e.g. `✔ Allowed · [ReadOnly]` or `✘ Prohibited · [Dangerous]` — followed by a `Description:` section and a `Parameters:` section read from the tool's input schema, required parameters first, then optional ones alphabetically.

A prohibited tool offers `e` (`Enable in manifest`) exactly when pressing it would change something — the tool is blocked and its manifest can be rewritten — and the outcome is reported in this view: on success the policy row flips to `✔ Allowed` and the shortcut disappears, on failure the reason is printed in full. `Esc` steps back one screen at a time — detail to list, list to overview, overview to the server list.

The scroll region is sized from the terminal, so a long failure reason stays readable instead of pushing the menu away, and every pane tracks the listing as it refreshes underneath, so a policy change made elsewhere is visible without leaving the view.

### MCP Authentication

`Authenticate` on the overview, `a` in the server list, and `/mcp auth <name>` all open the same screen rather than asking for confirmation first. It shows what is happening and keeps both ways of finishing a login on screen:

```text
Authenticating with sentry…

✻ A browser window will open for authentication

If your browser doesn't open automatically, copy this URL manually (c to copy)
https://auth.example.com/authorize?client_id=…&redirect_uri=http%3A%2F%2Flocalhost%3A56014%2Fcallback…

If the redirect page shows a connection error, paste the URL from your browser's address bar:
URL >

Return here after authenticating in your browser. Press Esc to go back.
```

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `c` | MCP Authorization | Copies the authorization URL, shown as `Copied authorization URL to clipboard.` Works while the paste field is empty; `Ctrl+Y` copies at any time. |
| `Ctrl+Y` | MCP Authorization | Copies the authorization URL regardless of what the paste field holds. |
| `Enter` | MCP Authorization | Submits whatever the paste field holds to the waiting authorization. |
| `Esc` | MCP Authorization | Stops reporting on the flow and returns to the server list, or closes the browser when `/mcp auth <name>` opened it. |

The paste field is the escape hatch for a browser that cannot reach the daemon's loopback callback — a browser on another machine, a blocked `localhost`, or a redirect page that simply failed to load. It accepts the full address-bar URL, a bare query string, or an authorization code alone, and the waiting flow receives the code exactly as if the browser had reached the daemon.

The browser itself is answered with a self-contained TaskSmith page, not plain text: the authorization page shows a green `Authentication Complete` card naming the connected server, and a rejected login shows a red `Authentication Failed` card with the OAuth `error` and `error_description`. Both carry the TaskSmith mark inline and as the tab icon, need no network access, and offer a `Close Window` button.

When the authorization settles, the screen closes and the outcome is left in the conversation timeline where it can still be read and copied:

```text
> /mcp
  ⎿  Authentication successful. Connected to sentry.
```

A failure — a rejected consent screen, an unsupported Dynamic Client Registration, a token exchange that failed, or a paste the daemon refused — is recorded as an error naming the reason instead. Leaving the screen with `Esc` does not cancel the login: the daemon keeps its callback listener open on a timer of its own, so finishing in the browser afterwards still connects the server.

### MCP Item Confirmation

Allowed prompts and resources run immediately. A prohibited one opens `Prohibited MCP item` with the reason naming the item, the policy section (`spec.policies.prompts` or `spec.policies.resources`), and the manifest path. Choices:

| Choice | Effect |
| :--- | :--- |
| **`Cancel`** *(default)* | Aborts and changes nothing. `Enter` lands here, so an accidental keypress is safe. |
| **`Use once`** | Runs this single request with `allowProhibited`, never editing the manifest. |
| **`Enable in manifest`** | Permits the item in the manifest, refreshes the server, then runs it as an allowed item. |

`Enable in manifest` is offered only when the manifest path is known and the server comes from workspace or project scope; a user- or builtin-scope server shows `Use once` alone, since rewriting it would edit files outside the project. Prohibited **tools** have no per-call override in either direction — the agent cannot be talked into one, and the CLI offers no `Use once` for them; only the manifest changes what tools a server exposes, which is why widening a tool happens from its [detail view](#mcp-tool-drill-down) rather than from this prompt. See [Tools & MCP Servers](../guides/tools-and-mcp.md#scoping-mcp-resources).

---

## 3. Keyboard Shortcuts

TaskSmith uses structured modal keybindings across its terminal interface:

### Application Navigation & Control

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Ctrl+C` | Global | First press: Clears composer input. Second press (within 2s): Exits the application. |
| `Esc` | Global | Cancels a running LLM turn, dismisses active autocomplete menus, or closes open modals/pickers. |

### Composer & Prompt Input

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Enter` | Composer | Submits prompt to the agent, executes a `/command`, or runs a direct `!bash` command. |
| `Shift+Enter` / `Alt+Enter` | Composer | Inserts a literal newline without submitting the prompt. |
| `Shift+Tab` | Composer | Cycles session permission mode: `default` $\to$ `accept_edits` $\to$ `plan` $\to$ `ask` $\to$ `auto`. |
| `!` *(on empty input)* | Composer | Switches composer to direct Bash command mode (`!command`). |
| `Backspace` *(on empty Bash prompt)* | Composer | Exits direct Bash command mode and returns to standard prompt. |
| `Ctrl+Y` | Composer | Approves the pending tool authorization request for the first waiting subagent once without leaving the composer. |
| `Ctrl+G` | Composer | Opens the live subagent chat view for the waiting subagent in the slot. |

### Autocomplete & History

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Tab` | Autocomplete | Inserts or completes the currently highlighted autocomplete suggestion. |
| `↑` / `↓` | Autocomplete | Navigates candidate suggestions up and down when the autocomplete menu is visible. |
| `Ctrl+P` / `Ctrl+N` | Autocomplete | Navigates candidate suggestions up and down, mirroring `↑` / `↓`. |
| `↑` / `↓` / `Ctrl+P` / `Ctrl+N` | Composer | Cycles backward and forward through prompt history. Autocomplete is suppressed while browsing, so recalling a `/command` or `@` mention keeps navigating instead of opening the menu. |
| `Esc` | Composer (history browsing) | Abandons browsing and restores the draft you were typing before you pressed `↑`. |
| Any edit key | Composer (history browsing) | Exits browsing mode and keeps the edited entry as your draft; autocomplete resumes. |

While browsing history, the composer's bottom border is replaced by a centered rule reading `───── history (<n>/<total>) ─────`, and the status line hint reads `prompt history · enter to send · esc to cancel`.

### Modals & Pickers (General)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `k` / `Ctrl+P` | Modals & Pickers | Move selection cursor up. |
| `↓` / `j` / `Ctrl+N` | Modals & Pickers | Move selection cursor down. |
| `Enter` | Modals & Pickers | Confirm and activate highlighted item. |
| `Esc` | Modals & Pickers | Dismiss and close modal without applying changes. |
| *Character keys* | Modals & Pickers | Dynamically filter list items with live search. |

### Session Browser (`/sessions`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Enter` | Session Browser | Switches active workspace session to the selected session. |
| `F2` | Session Browser | Prompts inline to rename the selected session title. |
| `F4` | Session Browser | Prompts confirmation (`y`/`Enter`) to permanently delete the selected session. |

### Changes Browser (`/changes`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Enter` | Changes Browser | Opens the full-screen unified diff viewer for the highlighted file change. |
| `r` | Changes Browser | Prompts confirmation to revert mutations on the highlighted file. |
| `Esc` | Diff Viewer / Browser | Closes the diff viewer or returns to composer. |

### Tasks Browser (`/tasks`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Enter` | Tasks Browser | Opens the live streaming output viewer for the highlighted background task. |
| `x` | Tasks Browser | Sends SIGTERM/kill signal to terminate the highlighted running task. |
| `a` | Tasks Browser | Toggles between displaying all session tasks and active running tasks only. |
| `Esc` | Output Viewer / Browser | Closes the streaming log viewer or dismisses the task manager. |

### Subagents Browser (`/subagents`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Enter` | Subagents Browser | Opens the live streaming chat view and history for the highlighted subagent. |
| `x` | Subagents Browser | Sends cancel signal to terminate the highlighted running subagent. |
| `a` | Subagents Browser | Toggles between displaying all subagents and active running subagents only. |
| `Esc` | Subagents Browser | Closes the subagents browser and returns to composer. |

### Subagent Live Viewer

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `k` / `j` or `Ctrl+P` / `Ctrl+N` | Subagent Viewer | Scrolls the live chat view up or down by 1 line. |
| `PgUp` / `PgDn` or `Ctrl+U` / `Ctrl+D` or `Ctrl+B` / `Ctrl+F` or `b` / `f` | Subagent Viewer | Scrolls the live chat view up or down by 8 lines. |
| `Home` / `End` or `g` / `G` | Subagent Viewer | Scrolls directly to the top or bottom of the live chat view. |
| `x` | Subagent Viewer | Sends cancel signal to terminate the subagent if currently active. |
| `Esc` | Subagent Viewer | Returns to the subagents browser or dismisses the viewer. |

### Daemon Browser (`/daemon`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `r` | Daemon Browser | Restarts the daemon (or starts if offline) and refreshes health state. |
| `s` | Daemon Browser | Starts the daemon if currently offline. |
| `x` | Daemon Browser | Gracefully stops the running daemon. |
| `g` | Daemon Browser | Re-queries daemon health and refreshes status in-place. |
| `Esc` / `q` | Daemon Browser | Closes the daemon browser and returns to composer. |

### Rules Browser (`/rules`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `Ctrl+P` / `Ctrl+N` | Rules Browser | Moves the highlighted rule. |
| `Enter` / `i` | Rules Browser | Opens the rule markdown inspector for the highlighted rule. |
| `r` | Rules Browser | Reloads workspace resources from disk and refreshes the rule list. |
| `Esc` | Rules Browser | Closes the rules browser and returns to composer. |

Columns are Name, Status (`[Always]`, `[Loaded]`, or `[Standby]`), Scope (`[Workspace]`, `[Project]`, `[User]`, or `[Builtin]`), estimated tokens, and description. `[Loaded]` reflects rules injected in the current session, including `load_rule` calls.

### Rule Inspector

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `k` / `j` or `Ctrl+P` / `Ctrl+N` | Rule Inspector | Scrolls the rule body by 1 line. |
| `PgUp` / `PgDn` or `Ctrl+U` / `Ctrl+D` or `Ctrl+B` / `Ctrl+F` or `b` / `f` | Rule Inspector | Scrolls the rule body by 8 lines. |
| `Home` / `End` or `g` / `G` | Rule Inspector | Scrolls to the top or bottom of the rule body. |
| `Enter` | Rule Inspector | Closes the browser and submits `@rule:<name> ` to attach the rule to the next prompt. |
| `Esc` | Rule Inspector | Returns to the rules list. |

### Tools Browser (`/tools`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `k` / `j` or `Ctrl+P` / `Ctrl+N` | Tools Browser | Moves the highlighted tool. |
| `Enter` / `i` | Tools Browser | Opens the tool inspector for the highlighted tool. |
| `r` | Tools Browser | Reloads workspace resources from disk and refreshes the tool list. |
| `Esc` | Tools Browser | Closes the tools browser and returns to composer. |

Columns are Name, Status (`[Eager]`, `[Deferred]`, `[Denied]`, `[Inactive]`, or `[—]`), Scope (`[Workspace]`, `[Project]`, `[User]`, or `[Builtin]`), safety annotations (`[ReadOnly]`, `[Dangerous]`, `[OpenWorld]`), and description. Statuses are resolved for the active agent — falling back to the workspace default agent — so the same tool can be `[Eager]` for one agent and `[Deferred]` for another. `[—]` is shown when the daemon could not classify tools for an agent and the unclassified workspace listing is displayed instead. See [Tool Partitioning](tools.md#5-tool-partitioning-eager-vs-deferred) for what each status means.

`/tools` takes no arguments; passing any is rejected before the browser opens.

### Tool Inspector

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `k` / `j` or `Ctrl+P` / `Ctrl+N` | Tool Inspector | Scrolls the tool detail by 1 line. |
| `PgUp` / `PgDn` or `Ctrl+U` / `Ctrl+D` or `Ctrl+B` / `Ctrl+F` or `b` / `f` | Tool Inspector | Scrolls the tool detail by 8 lines. |
| `Home` / `End` or `g` / `G` | Tool Inspector | Scrolls to the top or bottom of the tool detail. |
| `Esc` | Tool Inspector | Returns to the tools list. |

The inspector shows the tool name with its scope, status, and estimated token cost, followed by its description, `Command` (rendered argv, or `—` when the tool declares no command), `Annotations` (including `[Idempotent]` and the approval `userHint`), and pretty-printed `inputSchema` / `outputSchema` JSON blocks (`— (no schema declared)` when absent).

### Onboarding & Setup Wizard (`/setup`)

When TaskSmith is launched for the first time in an unconfigured directory, or when explicitly invoking `/setup`, the interactive wizard guides configuration across seven focused screens:

1. **Trust Prompt**: Review detected executable configurations (`.agents/tools/`, `.agents/mcps/`, `.env`) and grant folder or parent trust, or proceed in restricted mode.
2. **Provider Source**: Choose whether to reuse machine-wide global providers or configure project-specific credentials.
3. **Provider & Scope**: Select upstream AI provider preset and target configuration scope (`User`, `Workspace`, or `Project`).
4. **Credentials**: Enter and verify provider API keys, authenticate via browser OAuth (e.g. Google Cloud Code for Antigravity), or configure custom endpoints with real-time verification.
5. **Default Mode**: Choose the initial permission mode (`default`, `accept_edits`, `plan`, `ask`, `auto`).
6. **Tools & MCPs**: Selectively enable or disable built-in tools and global MCP servers.
7. **Review & Write**: Review configured settings, write out manifests, and optionally offer `/init`.

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `k` / `j` | Setup Wizard | Navigates between selectable options, list items, or menu entries. |
| `Enter` | Setup Wizard | Selects current option, confirms input, verifies credentials, or advances to the next step. |
| `Tab` | Setup Wizard | Toggles between options (such as trust scope `Folder` vs `Parent`, or provider scope `User` vs `Workspace`). |
| `Space` | Setup Wizard | Toggles selection checkbox for individual tools or MCP servers on the tools screen. |
| `Esc` | Setup Wizard | Navigates back to the previous screen or exits the setup wizard. |

---

## 4. State, Log, & Socket Locations

TaskSmith adheres to the XDG Base Directory Specification for state persistence, logs, and configuration:

| Category | Path | Purpose | Persistence |
| :--- | :--- | :--- | :--- |
| **Global Config** | `~/.config/tasksmith/` | Root directory for machine-wide user configuration (`$XDG_CONFIG_HOME`). | Permanent |
| **Global Secrets** | `~/.config/tasksmith/.env` | Global fallback environment variables and upstream API credentials. | Permanent |
| **Global Providers** | `~/.config/tasksmith/providers/*.yaml` | User-tier model provider manifests available across all workspaces. | Permanent |
| **Global Agents** | `~/.config/tasksmith/agents/*.agent.md` | User-tier custom agent personas and instructions. | Permanent |
| **Global Rules** | `~/.config/tasksmith/permissions.json` | Persistent machine-wide permission allow and deny rules. | Permanent |
| **Provider Credentials** | `~/.local/share/tasksmith/credentials/*.json` | OAuth 2.0 PKCE tokens (e.g. `antigravity.json`) with restricted permissions (`0600`). | Permanent |
| **Trust Store** | `~/.local/share/tasksmith/trust.json` | Machine-wide trusted directories registry, trust scopes (`folder` / `parent`), and executable fingerprints (`$XDG_DATA_HOME`). | Permanent |
| **Workspace Metadata** | `~/.local/share/tasksmith/workspaces/<hash>/workspace.json` | Workspace schema version and binary version for automated upgrade migrations. | Permanent |
| **Workspace DB** | `~/.local/share/tasksmith/workspaces/<hash>/tasksmith.db` | SQLite database storing turn journals, messages, and session state (`$XDG_DATA_HOME`). | Permanent |
| **Project Rules** | `~/.local/share/tasksmith/workspaces/<hash>/permissions.json` | Project-scoped persistent permission allow and deny rules. | Permanent |
| **Fallback Socket** | `~/.local/share/tasksmith/daemon.sock` | Fallback Unix domain socket path when `/tmp` runtime dir is unavailable. | Ephemeral |
| **Runtime Socket** | `/tmp/tsd-<uid>/b.sock` | Active Unix domain IPC socket used by CLI and daemon (`$XDG_RUNTIME_DIR`). | Ephemeral |
| **Process Lock** | `/tmp/tsd-<uid>/daemon.lock` | File lock ensuring a single background daemon runs per user. | Ephemeral |
| **Daemon Logs** | `~/.local/state/tasksmith/logs/daemon.jsonl` | Structured JSON Lines log file with daily rolling rotation (`$XDG_STATE_HOME`). | Permanent |
| **Workspace Root** | `<workspace-root>/WORKSPACE.md` | Workspace declaration, project boundaries, defaults, and security policies. | Permanent |
| **Workspace Env** | `<workspace-root>/.env` | Workspace-scoped secrets and environment variables. | Permanent |
| **Workspace Agents** | `<workspace-root>/.agents/` | Workspace-tier agents, skills, commands, decisions, and tool manifests. | Permanent |

## Status Line Fields

The status line composition is user-configurable via `/statusline` (or by editing
`~/.config/tasksmith/statusline.json` directly, which takes effect on the next REPL start). The
default composition, from left to right, is: the shortcut hint, the permission mode, context
window usage, session usage, and the model with its thinking effort. The first enabled segment
renders on the left of the line, the remaining ones on the right separated by ` · `.

| Segment ID | Renders | Compact form |
| :--- | :--- | :--- |
| `hint` | Shortcut hint or the Ctrl+C cancel prompt | — |
| `mode` | Permission mode badge | — |
| `context` | Context window usage, e.g. `109.5k/400.0k (27%)` | `27%` |
| `usage` | Session usage as `<cost> · ↑<input> ↓<output>` (for example `$0.42 · ↑128.4k ↓8.3k`) once the session has recorded an LLM call; updates live from `usage_updated` events. `↑` is the total prompt size (including cached tokens) and `↓` is output tokens. A leading `≥` on the cost means some calls had no pricing data. | — |
| `model` | Model and thinking effort, e.g. `gpt-5 · high` | model name only |

Unknown segment IDs in the config file are dropped and reported; duplicates collapse; an empty
result falls back to the default composition.

### Status Line Editor (`/statusline`)

The editor lists enabled segments (numbered, in layout order) followed by disabled ones, and
previews every edit live in the real status line.

| Key | Action |
| :--- | :--- |
| `↑` / `↓` (or `k` / `j`) | Move the cursor between rows. |
| `Space` / `Enter` / `t` | Toggle the segment under the cursor (at least one segment must stay enabled). |
| `←` / `→` (or `h` / `l`) | Reorder the enabled segment (its layout position). |
| `c` | Toggle compact mode. |
| `s` | Save the composition to `~/.config/tasksmith/statusline.json` and exit. |
| `Esc` | Discard the draft, restore the previous composition, and exit. |

### Responsive Narrowing

When the terminal is too narrow for the configured composition, segments shrink or hide in a
fixed ladder regardless of their layout order: `model` compacts then hides, `usage` compacts then
hides, `hint` is ellipsized to the available room then hides, and `context` compacts then hides.
The `mode` badge is safety-relevant and is never hidden by narrowing — only removing it from the
composition hides it.

## Turn Spinner and Summary

| Element | Format | Source |
| :--- | :--- | :--- |
| Spinner statistics | `(<elapsed> · ↑<input> ↓<output>)` | Elapsed time since the turn started streaming. `↑` and `↓` are the tokens sent and received in the turn, summed across its calls (not the context size): the session's token growth since the turn started plus the running usage of the in-flight call (`usage` on the stream chunk, cumulative per call). They start at zero. |
| Turn summary line | `<Verb> for <elapsed> · ↑<input> ↓<output> · <cost> · done <time>` | `turn_completed` timeline entry written by the daemon when the turn ends. Canceled and failed turns use `Stopped after` and `Failed after`. Completed turns under 5 seconds are skipped. |

## Usage Browser (`/usage`)

| Tab | Content |
| :--- | :--- |
| **Session** (`1`) | Total cost; API and wall-clock duration; code changes (lines added and removed); usage by model (uncached input, cache read, cache write, output, cost); prompt cache (requests, share of input from cache, misses, expected rebuilds after compaction, last use); cache read and write cost; tool calls with per-tool calls, errors, denied, canceled, total and slowest time, and lines changed; usage insights. |
| **Workspace** (`2`) | The selected range for the current workspace: cost and trend versus the previous period, sessions and average cost per session, API duration, code changes and cost per 100 changed lines, tokens, cache, usage grouped by the selected dimension, top sessions by cost, tools, insights. |
| **Global** (`3`) | The same as Workspace across all workspaces, plus usage by workspace and top sessions across workspaces. |

| Key | Action |
| :--- | :--- |
| `←` / `→` / `Tab` / `Shift+Tab` / `1` `2` `3` | Switch tab. |
| `↑` / `↓` / `Ctrl+P` / `Ctrl+N` / `PgUp` / `PgDn` / `Ctrl+B` / `Ctrl+F` | Scroll long views. |
| `m` `p` `a` `u` `i` `d` | Group Workspace and Global tabs by model, provider, agent, purpose, pricing tier, or day (the busiest day is marked with `*`). |
| `t` | Cycle the range: last 24 hours, 7 days, 30 days, all time (default 30 days). |
| `r` | Refresh. |
| `Esc` / `q` | Close. |

Input tokens are shown as uncached, cache read and cache write, which do not overlap. A cost prefixed with `≥` is a lower bound because some calls had no pricing. Insights (large-context share, cache write share, cache misses, compaction share, idle sessions, unpriced models, provider failure rates, tool error, denial and slow-tool rates, spend trend) appear only when a fixed threshold is met. A cache miss is a drop in cache read versus the previous chat call; calls after a compaction are counted as expected rebuilds. The cause of a miss is not guessed: the view shows the time since the previous call.

## Stats Browser (`/stats`)

`/stats` answers how fast inference is, where `/usage` answers how much it cost. It reads the same metrics records and covers chat calls only: compaction re-reads a whole context and title generation is a tiny call, so including either would distort prompt speed.

| Section | Content |
| :--- | :--- |
| **Throughput** | Prompt processing in tokens per second over uncached input, token generation in tokens per second over output, time to first token and end-to-end latency as an average plus the slowest call. |
| **Per model** | One row per model with requests, prompt tokens (uncached input), output tokens, cached tokens, total tokens, and generation speed in tokens per second, ordered by total tokens. |
| **Footnote** | Cost for the range (or `not priced` when no call had pricing), which speeds were inferred and from what, or how many calls carried timing, and a note that only chat calls are counted. |

| Key | Action |
| :--- | :--- |
| `←` / `→` / `Tab` / `Shift+Tab` / `1` `2` `3` | Switch tab. |
| `↑` / `↓` / `Ctrl+P` / `Ctrl+N` / `PgUp` / `PgDn` / `Ctrl+B` / `Ctrl+F` | Scroll long views. |
| `t` | Cycle the range: today, last 7 days, last 30 days, all time (default today). |
| `r` | Refresh. |
| `Esc` / `q` | Close. |

The Session tab re-reads its own metrics every 15 seconds while it is open; Workspace and Global are scanned on demand with `r`, because those ranges grow with history and gain nothing from a background query.

**Reported vs inferred speeds.** Only providers that return a per-call timing split (Ollama reports prompt-eval and eval durations) let `/stats` divide tokens by the exact phase that moved them. Providers speaking the OpenAI protocol return no such split, so both speeds are derived from the timings that do exist and are prefixed with `~`: prompt processing uses time to first token, since no token can arrive before the prompt has been read, and token generation uses total duration minus time to first token. Because time to first token also carries queueing and network overhead, the derived prompt speed is a floor rather than an estimate of the true rate. Deriving generation requires both timings to cover the same calls; when they do not, the figure stays `—` rather than inventing a number. A speed whose timing covers fewer calls than the range contains is also marked `~`, because it divides every call's tokens by part of the time. The footnote names whichever speeds were inferred.

Averages are taken over the calls that recorded the relevant timing, so a speed is never diluted by calls that measured nothing. A figure shown as `—` was not measured at all rather than being zero. Today begins at local midnight. A cost prefixed with `≥` is a lower bound because some calls had no pricing; a model with no pricing at all reads `not priced` rather than `≥ $0.00`.

## Usage HTTP Endpoints

All usage endpoints are read-only.

| Method | Path | Description |
| :--- | :--- | :--- |
| `GET` | `/v1/sessions/{id}/usage` | `SessionUsage` for one session (including child sessions): totals, breakdowns by model, pricing tier, purpose and tool (calls, errors, denied, canceled, total and slowest duration, lines changed), code changes, prompt cache statistics, and first and last activity time. |
| `GET` | `/v1/usage` | Usage summary (totals, groups, sessions count, top sessions, tools, code changes, previous-period totals, and per-workspace groups for global scope). Query params: `scope=workspace\|global` (default `workspace`, resolved from `X-TaskSmith-CWD`), `since` and `until` (RFC 3339 or `YYYY-MM-DD`; `since` defaults to 30 days ago), `group_by=model\|provider\|agent\|purpose\|tier\|day` (default `model`). |
| `POST` | `/v1/metrics/query` | Runs a generic metrics query (`kind`, `aggregates`, `group_by`, `bucket`, `filter`, `since`, `until`) and returns grouped rows. Aggregate functions are `sum`, `min`, `max` and `count`; `count` counts only the records that measured that metric, so `sum/count` is a mean over measured records. Rows report one value per metric name, so a query listing one metric twice is rejected with `400`; request one function per metric. |

## MCP HTTP Endpoints

MCP endpoints act on the workspace resolved from the `X-TaskSmith-CWD` header. Servers are addressed by manifest name, or by qualified name (`<slug>/MCP/<name>`) when a global and a project server share a name. Every endpoint returns `404` `not_found` for an unknown server, and `503` `mcp_unavailable` for operations needing a live connection while the workspace has no MCP runtime attached.

| Method | Path | Description |
| :--- | :--- | :--- |
| `GET` | `/v1/workspace/mcps` | Lists configured MCP servers as `MCPServer`: manifest `name`, `displayName`, `description`, `namespace`, `scope`, portable manifest `path`, `transport` (as declared: `stdio`, `sse`, `http`, or `streamable-http`; defaults to `stdio`), resolved `command` or `endpoint`, `authType`, live `status` (`disconnected`, `connecting`, `connected`, `auth_required`, or `error`), `authUrl` and `error` when set, plus discovered `tools`, `prompts`, and `resources`. Items excluded by manifest policies are still listed with `allowed: false`. Servers with no live connection report `status: disconnected`. |
| `POST` | `/v1/workspace/mcps/{name}/restart` | Reconnects one server and returns its refreshed `MCPServer`. |
| `POST` | `/v1/workspace/mcps/{name}/auth` | Starts or resumes authorization for one server and returns `MCPAuthResponse` (`authUrl`, `status`). |
| `POST` | `/v1/workspace/mcps/{name}/auth/callback` | Completes the authorization in progress with a redirect URL the user copied from their browser. Body: `callbackUrl` — the full address-bar URL, a bare query string, or an authorization code alone. Answers `204` with no body, since the flow continues on its own; `400` `bad_request` when the paste is empty or no authorization is open for the server. |
| `POST` | `/v1/workspace/mcps/{name}/prompts/{prompt}` | Renders one discovered prompt with optional `args` (string key to string value) and returns `MCPPromptResponse` (`description`, `messages` with `role` and `text`). |
| `POST` | `/v1/workspace/mcps/{name}/resources` | Reads one resource by `uri` and returns `MCPResourceResponse` (`uri`, `mimeType`, `text`). |
| `POST` | `/v1/workspace/mcps/{name}/policies/allow` | Permits one item in the server manifest. Body: `kind` (`tool`, `prompt`, or `resource`) and `value` (raw tool name, prompt name, or resource URI). Returns the updated `MCPServer`. |

Prompt rendering and resource reads reject anything the server manifest's `policies` prohibit with `403` `mcp_policy_prohibited`; the rejection message names the prohibited item, the policy section it is excluded from (`policies.tools`, `policies.prompts`, or `policies.resources`), and the manifest path. Pass `allowProhibited: true` in the prompt or resource body to permit a single prohibited use without editing the manifest. See [Tools & MCP Servers](../guides/tools-and-mcp.md) for manifest authoring and [Manifests](./manifests.md) for the `MCP` schema.

## Workspace Onboarding & Trust HTTP Endpoints

All onboarding and trust endpoints operate on the workspace directory resolved from the `X-TaskSmith-CWD` header.

| Method | Path | Description |
| :--- | :--- | :--- |
| `GET` | `/v1/workspace/status` | Returns `WorkspaceStatus` containing trust state (`isTrusted`, `fingerprintChanged`), executable scan summary, configuration flags, available global providers/MCPs, missing credentials, and whether the workspace requires first-run onboarding (`needsOnboarding`). |
| `GET` | `/v1/workspace/trust` | Returns `TrustStatus` for the current workspace, including detected MCP configs, custom tools, and local `.env` existence. |
| `POST` | `/v1/workspace/trust` | Records trust for a workspace directory. Body: `path` (absolute path) and `scope` (`folder` or `parent`). |
| `DELETE` | `/v1/workspace/trust` | Revokes trust for the current workspace directory, returning it to restricted mode. |
| `POST` | `/v1/providers/verify` | Validates API credentials or endpoint reachability against upstream model provider. Body: `name`, `apiKey`, optional `endpoint`, and `scope` (`user` or `workspace`). Returns success status, latency, or validation error. |
| `POST` | `/v1/workspace/init` | Initializes workspace configuration and applies user choices. Body: `InitWorkspaceRequest` (`scope`, `provider`, `apiKey`, `endpoint`, `defaultMode`, `disabledTools`, `disabledMcps`). Writes configuration files and initial `workspace.json` metadata. |
