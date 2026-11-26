# Interactive Slash Commands

Use these commands in the REPL composer. Keys for each interactive view are listed in [Keyboard Shortcuts](./keybindings.md).

## Command directory

The session status shows the model resolved by the daemon and the thinking that model will apply when nothing overrides it. When no thinking default is known, the banner and status line say `default effort`; the thinking commands call this `provider default`.

| Slash Command | Aliases | Arguments | Description |
| :--- | :--- | :--- | :--- |
| **`/help`** | `/h`, `/?` | *(none)* | Opens the interactive slash command browser and help viewer. |
| **`/mode`** | | `[mode]` (`default`, `accept_edits`, `plan`, `ask`, `auto`) | Inspects current permission mode or switches to the specified mode. |
| **`/plan`** | | `[prompt]` | Switches to `plan` mode, and optionally submits `prompt` immediately as a planning turn. |
| **`/model`** | | `[--force] [model_id]` | Switches to `model_id`, or opens the interactive model picker if no argument is provided. Prompts for confirmation (or requires `--force`) when switching drops modalities active in the session. See [Model Picker](#model-picker-model). |
| **`/agent`** | | `[agent_name]` | Displays active agent, switches to `agent_name`, or opens the interactive agent picker if no argument is provided. |
| **`/effort`** | | `[level\|default]` | Set an allowed model-specific effort or open the picker, which shows available descriptions. |
| **`/budget`** | | `[tokens\|default]` | Show or set a supported thinking token budget. |
| **`/thinking`** | | `[on\|off\|adaptive\|default]` | Show or set a supported thinking toggle or adaptive strategy. |
| **`/theme`** | | `[theme_name]` | Changes terminal UI color theme, or opens the interactive theme picker if no argument is provided. |
| **`/clear`** | | *(none)* | Creates and transitions to a fresh draft session. |
| **`/exit`** | `/quit`, `/q` | *(none)* | Gracefully terminates and exits the interactive REPL. |
| **`/new`** | | `[model]` | Creates and transitions to a fresh draft session, optionally overriding the model. |
| **`/fork`** | | `[title]` | Forks the current session into a new top-level session that starts from the same conversation, then switches to it. An optional title overrides the derived `"… (fork)"` name. |
| **`/sessions`** | `/session`, `/ls` | `[session_id]` | Switches to `session_id`, or opens the interactive session browser to switch, rename, fork, or delete sessions. |
| **`/changes`** | | *(none)* | Opens the interactive changes browser to inspect tracked file modifications, diffs, and hashes. |
| **`/revert`** | | `[path] [entry_id]` | Reverts uncommitted file mutations for a specific file, change entry, or the entire session. |
| **`/permissions`** | `/perm`, `/perms` | *(none)* | Opens the interactive permissions browser to inspect and edit project and global allow/deny rules. |
| **`/sandbox`** | | *(none)* | Opens the interactive sandbox configuration editor to inspect host kernel sandboxing and edit project/global isolation settings. See [Sandbox Editor](#sandbox-editor-sandbox). |
| **`/tasks`** | | *(none)* | Opens the background task manager to inspect running commands and stream task logs. |
| **`/subagents`** | `/subagent` | *(none)* | Opens the interactive subagent browser to inspect running and completed subagents, view live outputs, or terminate tasks. |
| **`/compact`** | | `[focus]` | Compacts conversation history to free context window space, with optional focus instructions. Requires pending permissions and questions to be resolved first. |
| **`/context`** | | *(none)* | Displays visual token usage grid and detailed context window breakdown by category. |
| **`/usage`** | | `[session\|workspace\|global\|quotas]` | Opens the usage browser slot on the Session, Workspace, Global or Quotas tab (default: Session). See [Usage Browser](#usage-browser-usage). |
| **`/stats`** | | `[session\|workspace\|global]` | Opens the inference speed browser slot on the Session, Workspace or Global tab (default: Session). See [Stats Browser](#stats-browser-stats). |
| **`/skills`** | | *(none)* | Opens the interactive skills browser to inspect runbooks, token estimates, and reload workspace skills. |
| **`/rules`** | | *(none)* | Opens the interactive rules browser to inspect rule status, scope, triggers, and token estimates, and to reload workspace rules. |
| **`/tools`** | | *(none)* | Opens the interactive tools browser to inspect tool status, scope, safety annotations, and JSON schemas, and to reload workspace tools. See [Tools Browser](#tools-browser-tools). |
| **`/mcp`** | `/mcps` | `[auth <name>\|restart <name>]` | With no subcommand, opens the interactive MCP browser to inspect servers, run prompts, attach resources, authorize, and restart. `auth` and `restart` act directly on one named server without a confirmation. See [MCP Browser](#mcp-browser-mcp). |
| **`/providers`** | `/provider` | *(none)* | Opens the interactive provider browser to enable/disable, manage models, browse catalogs, and add configured model providers from their WARP manifests, with an `F2` alias editor shared with `/model`. See [Providers Browser](#providers-browser-providers). |
| **`/setup`** | | *(none)* | Opens the interactive workspace onboarding and configuration wizard to reconfigure providers, scopes, permissions, tools, or MCP servers. |
| **`/login`** | `/auth`, `/signin` | `[provider]` | Opens the provider sign-in screen to authorize an OAuth provider (e.g. Antigravity) through the daemon. With no provider, resolves automatically when exactly one OAuth provider exists. See [Provider Sign-In (`/login`)](#provider-sign-in-login). |
| **`/statusline`** | `/status` | *(none)* | Opens the interactive editor to toggle, reorder, and move segments between rows, with a compact option and live preview; saves to `statusline.json`. See [Status Line Editor](#status-line-editor-statusline). |
| **`/warnings`** | | *(none)* | Opens the interactive list of active workspace and MCP warnings. See [Warnings View](#warnings-view-warnings). |
| **`/init`** | | *(none)* | Invokes the built-in `system:init` command to inspect codebase conventions and interactively generate or update `AGENTS.md`. |
| **`/review`** | | `[pr, branch, commit, or diff scope]` | Starts a clean, read-only `code-reviewer` session. See [Code reviews](../../guides/cli.md). |
| **`/grill-me`** | | `[idea, plan, or problem]` | Runs a design interview in the current session without writing files or switching modes. Use it before `/plan` to settle design decisions. See [Design Interviews](../../guides/cli.md). |
| **`/workspace`** | `/ws` | `[reload] [--force\|-y]` | Reloads workspace resources (`refresh` is an alias for `reload`). With conversation history, warns that changes can invalidate the provider prompt cache and cause cached input tokens to be billed at the uncached rate next turn. Use ↑/↓ to select **Reload workspace** or **Cancel** (default), Enter to confirm, or Esc to cancel. Zero-turn sessions reload immediately; `--force` or `-y` skips confirmation. |
| **`/daemon`** | | `[status\|restart\|start\|stop]` | Inspects or manages the background Brain Daemon. With no subcommand, opens the interactive daemon browser. |

## Headless Slash Commands (`exec`)

`tasksmith exec` (and `-p`) resolves a small UI-free command set locally and forwards everything else to the daemon, exactly as the REPL does. The full table and forwarding rules live in the [Headless Execution Reference — Slash Commands Without the TUI](../exec.md#6-slash-commands-without-the-tui).

## Model Picker (`/model`)

The picker lists the models the daemon reports as able to serve chat completion. Models that declare `completion: false` — embeddings, moderation, and similar — are left out, and the daemon rejects them for a session with `model "<id>" does not support chat completion`, so a session can never end up on a model it cannot converse with. A model that reports no completion capability at all is treated as chat-capable.

Each row shows:

| Column | Content |
| :--- | :--- |
| Label | Model label, falling back to its name and then its id. Rows are grouped under a muted provider heading. |
| Capacity | Context window (e.g. `400.0k ctx`) and the model's workspace aliases rendered as `@name`. |
| Billing | Per-token prices in USD per million tokens (`$3/$15 /M` for input/output), or `quota` for subscription-billed providers. Blank when billing is unknown. |

A muted detail line under the list shows the highlighted model's description, and the row of the
model the session is actually running is suffixed `current`.

- **Active model before a choice**: a fresh draft session has no model of its own, yet it already resolves to the workspace or agent default. That resolved model is marked `current`, and a session configured with an alias is marked on the model the alias resolves to.
- **Search**: the picker filter matches the label, aliases, model ID, provider name, and billing cell. `/model` argument autocomplete matches model IDs, labels, and unique `@alias` values, and inserts the full `provider/model` ID. If multiple selectable models share an alias, autocomplete skips that alias but still lists the models by ID.
- **Alias status line**: a muted line under the title shows where each standard tier currently resolves (`@default → anthropic/claude-x · @fast → unassigned`), so unassigned tiers are visible before editing anything.
- **Alias editor (`F2`)**: opens the shared [Model Alias Editor](#model-alias-editor-f2) on the highlighted model. `F2` matches the `/sessions` rename chord, so printable characters keep feeding the live search filter without collision, and `Enter` stays a strictly ephemeral session model switch. The editor opens only for models owned by a configured provider manifest.
- **Capability mismatch gate**: if switching to a model drops an input modality (`image`, `pdf`, `audio`) currently present in post-compaction session context, the REPL prompts in `<Slot />` to **Switch anyway** (retrying with `force: true` and omitting incompatible historical media on future turns) or **Cancel**. Passing `/model --force <id>` skips the prompt.
- **Nothing chat-capable**: the picker closes and reports `No chat-capable models available.`

## Thinking Controls (`/effort`, `/budget`, `/thinking`)

Each command is available only when the resolved model declares the strategy it needs, so the same command can be usable on one model and refuse on another.

| Command | Requires |
| :--- | :--- |
| `/effort [level\|default]` | An `effort` strategy. |
| `/budget [tokens\|default]` | A `budget` strategy. |
| `/thinking [on\|off\|adaptive\|default]` | A `toggle` strategy for `on`/`off`, or an `adaptive` strategy for `adaptive`. The command needs at least one of the two. |

- **State without an argument** reports the session override if there is one, then the model's own default as `default (<strategy>)`, then `provider default` when nothing declares a default. `/budget` adds the range it accepts, e.g. `Thinking budget: default (500 tokens) — range 100–1000`.
- **Suggestions** list only the arguments the active model accepts, so an adaptive-only model never offers `on`/`off`.
- `default` clears the session override and returns to the agent or model default. Setting any control replaces the previous thinking override. See [Providers](../providers.md#thinking-capability-fields) for how strategies and defaults are declared.

## Workspace Commands, Skills & Plugin Extensions

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

## Skill & Rule Loading in the Transcript

Agent calls to `load_skill` show the skill name and loading, loaded, or failed status. Failures include the first error line below the status. Skill arguments and rendered instructions are not displayed in the widget.

Agent calls to `load_rule` render the same style of widget (`○ Loading Rule <name>` then `● Loaded Rule <name>`), with a truncation warning subline when the rule body exceeds size limits. When the harness injects a rule automatically, or a rule is attached with `@rule:`, a single muted transcript line is emitted instead:

```text
● Loaded Rule go-error-handling (matched internal/daemon/session/session.go)
● Loaded Rule bash-safety (tool: bash)
● Loaded Rule security-audit (@rule:security-audit)
```

The injected `<system_reminders>` body itself is never printed in the transcript.

## Inline Attachment References

Token prefixes typed anywhere in a prompt render as inline pill badges:

| Syntax | Rendering | Functional today |
| :--- | :--- | :--- |
| `@<token>` / `@file:<path>` | Pill with file/folder icon | **Yes** — triggers `@` autocomplete, inserts shortest unique workspace suffix (or explicit path), expands text files or directories into `<system_reminders>` (with optional `#L<start>[-L<end>]` line ranges), and attaches supported images/PDFs/audio into `<session-dir>/media/<sha256>`. |
| `[Image #N]` (via `Ctrl+V` / `Alt+V`) | Pill with image icon | **Yes** — pastes an image from the OS clipboard into the composer, staging it as an inline `[Image #N]` pill and uploading it to `<session-dir>/media/<sha256>` on send. |
| `@rule:<name>` | Pill with shield icon | **Yes** — loads the rule, with `@rule:` autocomplete. |
| `@sym:<name>` | Pill with symbol-kind icon | Display only. |
| `@skill:<name>` | Pill with plugin icon | **Yes** — loads the skill into `<system_reminders>` and activates its deferred tools, with `@skill:` autocomplete (skills with arguments can also be invoked as `/skill <args>`). |

Typing `@` in the composer opens a fuzzy file and directory completion popup backed by the daemon's sandbox file index. Matching candidates display Nerd Font file/directory icons, parent directories, and `· recent` badges for session-accessed paths. Prohibited files (such as secret `.env` files or paths denied by permissions/policy) display a `[Prohibited]` badge; sending a prompt containing a prohibited or path-escaping mention is rejected with HTTP 403 Forbidden. Selecting a file or directory inserts its shortest unique slash-delimited workspace suffix (e.g. `@composer.go `, `@cli/app.go `, or `@docs/ `), quoting if the path contains whitespace. Explicit `@file:<path>` syntax is also supported. You can optionally specify a 1-based line range (e.g. `@composer.go#L10-L20` or `#L42`). During turn execution, the harness reads the file or directory via `std.View` / `std.List`, automatically tracks CAS reads, and injects the formatted contents into `<system_reminders>`. Raw prompt prose without attachments (e.g. pasted logs referencing `.env`) is never falsely rejected.

Typing `@rule:` or `@skill:` opens a completion popup listing available workspace rules or skills with their scope and description; selecting one inserts `@rule:<name> ` or `@skill:<name> `. Referenced rules and skills are injected immediately into `<system_reminders>` (and attached skills activate any policy-allowed deferred tools they declare); unknown names are ignored, and neither is injected twice within the same compaction epoch.

## MCP References (`/mcp:`, `@mcp:`)

Discovered MCP prompts and resources are invokable from the composer:

| Syntax | Autocomplete | Effect |
| :--- | :--- | :--- |
| `/mcp:<server>:<prompt>` | `/` slash menu | Renders the prompt on that server and submits the rendered messages as your turn. |
| `@mcp:<server>:<uri>` | `@` mention menu | Binds the resource as a structured attachment; the daemon reads it on turn execution and injects `<context_resource>` into `<system_reminders>` while keeping the `@mcp:` pill in your message. |

- **Position**: A reference is recognized anywhere in the message at a token boundary, so `explain @mcp:notes:notes://spec-1 briefly` works and the surrounding text is kept.
- **Multiple references**: Multiple allowed `@mcp:` resources can be attached in a single message (including alongside `@` file and `@rule:` mentions). Invoking more than one `/mcp:` prompt (or mixing interactive MCP prompts/confirmations) in one message is rejected with an error naming the count.
- **Unknown server**: Left as ordinary text and sent to the agent, so prose that happens to start with `/mcp:` is never hijacked. An unknown prompt or URI on a *known* server is an error pointing at `/mcp`.
- **Prohibited items**: Completion lists them labeled `[Prohibited]`. Invoking one opens the confirmation described in [MCP Item Confirmation](#mcp-item-confirmation); see [Tools & MCP Servers](../../guides/tools-and-mcp.md) for what a policy prohibits.
- **Arguments**: A prompt that declares arguments collects them first in an `Arguments for /mcp:<server>:<prompt>` input prefilled with a JSON object template. A prompt with exactly one declared argument also accepts a bare value.

## MCP Browser (`/mcp`)

The browser opens as `Manage MCP servers` with a `<N> server` / `<N> servers` count line, then one muted heading per scope that has servers — `Project MCPs`, `Workspace MCPs`, `User MCPs`, `Builtin MCPs` — each naming the directory its manifests live in (for example `Workspace MCPs (.agents/mcps)` or `User MCPs (~/.config/tasksmith/agents/mcps)`). Servers are listed by scope tier, closest to the repository first, then by name. The list is deliberately not filterable, so the single-letter actions below always reach the highlighted server instead of typing into a search box.

[Keyboard shortcuts](./keybindings.md#mcp-browser-mcp).

Each row shows a status glyph in the cursor gutter, the server name, and a right-hand status summary:

| Status | Glyph | Right-hand summary |
| :--- | :--- | :--- |
| `connected` | `✔` green | Capability counts, e.g. `25 tools · 2 prompts · 3 resources` — `connected` when nothing was discovered, and `25 tools (24 allowed)` when a policy blocks some items. |
| `auth_required` | `⚠` yellow | `needs authentication` |
| `error` | `✘` red | `failed to connect`, or `failed to connect (<reason>)` with transport plumbing trimmed so the reason reads cleanly. |
| `connecting` | `⟳` cyan | `connecting…` |
| `disconnected` | `○` muted | `disconnected` |

Disabled servers stay in the list so `e` can enable them again. Disabling a server updates `spec.enabled` in a writable workspace manifest or adds it to `WORKSPACE.md` under `spec.policies.mcps.exclude`. Disabled servers cannot be restarted or authorized.

With nothing configured, the view offers `R` / `Ctrl+R` to reload, which is what you need right after adding a manifest.

`/mcp auth <name>` and `/mcp restart <name>` run the same actions as `a` and `r` with no confirmation step: naming the server in the composer is already the decision. `/mcp auth <name>` opens the browser directly on that server's [authorization screen](#mcp-authentication); `/mcp restart <name>` opens nothing and reports in the conversation timeline, like `/daemon restart` and `/workspace reload --force`. A server that authenticates with static credentials instead of an authorization redirect gets a message naming the credential to provide — the environment variable, header, or credential file — because there is no browser flow to open; set it and run `/mcp restart <name>`.

## MCP Server Overview

Pressing `Enter` or `i` on a server replaces the list with that server's overview: a title line, aligned `Key: Value` metadata, and a numbered action menu.

[Keyboard shortcuts](./keybindings.md#mcp-server-overview).

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

## MCP Tool Drill-Down

[Keyboard shortcuts](./keybindings.md#mcp-tool-drill-down).

`View tools` opens a filterable list titled `<server> — tools (<N>)`, one row per tool with a `✔` or `✘` policy glyph, the tool's own name, its badges, and a compact description. `Enter` on a tool opens its detail: `Tool name`, `Full name` (the qualified name the agent calls it by), and `Policy` — the verdict with its badges inline, e.g. `✔ Allowed · [ReadOnly]` or `✘ Prohibited · [Dangerous]` — followed by a `Description:` section and a `Parameters:` section read from the tool's input schema, required parameters first, then optional ones alphabetically.

A prohibited tool offers `e` (`Enable in manifest`) exactly when pressing it would change something — the tool is blocked and its manifest can be rewritten — and the outcome is reported in this view: on success the policy row flips to `✔ Allowed` and the shortcut disappears, on failure the reason is printed in full. `Esc` steps back one screen at a time — detail to list, list to overview, overview to the server list.

The scroll region is sized from the terminal, so a long failure reason stays readable instead of pushing the menu away, and every pane tracks the listing as it refreshes underneath, so a policy change made elsewhere is visible without leaving the view.

## MCP Authentication

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

[Keyboard shortcuts](./keybindings.md#mcp-authentication).

The paste field is the escape hatch for a browser that cannot reach the daemon's loopback callback — a browser on another machine, a blocked `localhost`, or a redirect page that simply failed to load. It accepts the full address-bar URL, a bare query string, or an authorization code alone, and the waiting flow receives the code exactly as if the browser had reached the daemon.

The browser itself is answered with a self-contained TaskSmith page, not plain text: the authorization page shows a green `Authentication Complete` card naming the connected server, and a rejected login shows a red `Authentication Failed` card with the OAuth `error` and `error_description`. Both carry the TaskSmith mark inline and as the tab icon, need no network access, and offer a `Close Window` button.

When the authorization settles, the screen closes and the outcome is left in the conversation timeline where it can still be read and copied:

```text
> /mcp
  ⎿  Authentication successful. Connected to sentry.
```

A failure — a rejected consent screen, an unsupported Dynamic Client Registration, a token exchange that failed, or a paste the daemon refused — is recorded as an error naming the reason instead. Leaving the screen with `Esc` does not cancel the login: the daemon keeps its callback listener open on a timer of its own, so finishing in the browser afterwards still connects the server.

## MCP Item Confirmation

Allowed prompts and resources run immediately. A prohibited one opens `Prohibited MCP item` with the reason naming the item, the policy section (`spec.policies.prompts` or `spec.policies.resources`), and the manifest path. Choices:

| Choice | Effect |
| :--- | :--- |
| **`Cancel`** *(default)* | Aborts and changes nothing. `Enter` lands here, so an accidental keypress is safe. |
| **`Use once`** | Runs this single request with `allowProhibited`, never editing the manifest. |
| **`Enable in manifest`** | Permits the item in the manifest, refreshes the server, then runs it as an allowed item. |

`Enable in manifest` is offered only when the manifest path is known and the server comes from workspace or project scope; a user- or builtin-scope server shows `Use once` alone, since rewriting it would edit files outside the project. Prohibited **tools** have no per-call override in either direction — the agent cannot be talked into one, and the CLI offers no `Use once` for them; only the manifest changes what tools a server exposes, which is why widening a tool happens from its [detail view](#mcp-tool-drill-down) rather than from this prompt. See [Tools & MCP Servers](../../guides/tools-and-mcp.md#use-mcp-prompts-and-resources).

## Providers Browser (`/providers`)

`/providers` (alias `/provider`) opens `Manage model providers` over the workspace's declarative `ModelProvider` manifests — `~/.config/tasksmith/agents/providers/*.provider.yaml` (User) and `<workspace-root>/.agents/providers/*.provider.yaml` (Workspace) — grouped by scope and manifest directory. Each row shows a colored status icon, provider name, status, and configured model count when available. Providers are listed by scope tier, closest to the repository first, then by name. The list is deliberately not filterable, so the single-letter actions below always reach the highlighted provider instead of typing into a search box. Every mutation patches the owning manifest through the daemon and hot-reloads the workspace, so the model catalog and gateway agree with the YAML immediately; there is no sidecar state.

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Enter` / `m` | Providers Browser | Opens the model manager for the highlighted provider. |
| `Space` / `e` | Providers Browser | Toggles `spec.enabled` on the highlighted provider's manifest. |
| `l` | Providers Browser | Opens the OAuth sign-in screen for the highlighted provider. |
| `a` / `n` | Providers Browser | Starts adding a provider from a preset or a custom manifest. |
| `Esc` | Providers Browser | Closes the browser and returns to the composer. |

See [provider keyboard shortcuts](./keybindings.md#providers-browser-providers) for navigation, inherited provider settings, and the `F2` alias editor.

The detail line below the list shows the highlighted provider’s adapter type, signed-in account email (when present), and configured endpoint.

| Status | Icon | Meaning |
| :--- | :--- | :--- |
| `Ready` | `✔` green | The provider is enabled and its credential requirements are satisfied. |
| `Local` | `●` cyan | The provider uses a local endpoint or the Ollama adapter. |
| `Needs sign-in` | `⚠` yellow | The configured credential or OAuth token is unavailable. |
| `Session expired` | `⚠` yellow | The stored OAuth session expired or was rejected; press `l` or run `/login <provider>` to sign in again. |
| `Invalid key` | `⚠` yellow | The configured API credential was rejected with `401 Unauthorized`; update the credential and reload. |
| `Invalid config` | `⚠` yellow | The provider manifest has an invalid configuration (see `/warnings` for details). |
| `Disabled` | `○` muted | The manifest sets `spec.enabled: false`. A workspace shadow over an enabled inherited manifest shows `Disabled (shadow)`. |

### Provider Model Manager

`Enter` / `m` on a provider opens `Models · <name>` with separate columns for the model label, ID, and `context · aliases`. The default completion model carries `(default)` beside its label. When you stage a new default, the marker moves immediately.

New entries inherit available limits, capabilities, supported reasoning settings, and pricing from the provider catalog or runtime profile. This also applies when a custom ID matches a known model.

[Keyboard shortcuts](./keybindings.md#provider-model-manager).

### Provider Model Catalog

`c` opens `Catalog · <name>`. The gateway queries the configured provider's live catalog when its Loom driver supports discovery. A driver with a profile catalog alone supplies its known profiles. When the gateway has no catalog, the picker uses the bundled preset. The pane is filter-first: every plain printable keystroke types into the filter (so queries like `sonnet` work even though letters like `s` are actions in other panes), entries already declared in the manifest show `✔ … declared`, and `Ctrl+S` adds the checked entries to `spec.models` in one write.

[Keyboard shortcuts](./keybindings.md#provider-model-catalog).

### Adding a Provider

`a` / `n` first lists the built-in presets (with auth and model count) plus `Custom provider…`; `Esc` returns to the browser. Regular providers show a field form — `Scope` (Workspace or User), `Name`, `Type` (custom only), `Endpoint`, `Auth env var`, `API key`, and `Create provider`:

[Keyboard shortcuts](./keybindings.md#adding-a-provider).

For Codex, Claude Code, and GitHub Copilot, the form only shows `Scope` and `Create provider`. These browser-only presets use fixed names, types, and OAuth settings; endpoint, auth env var, and API key overrides are rejected. After creating the preset, [sign in](#provider-sign-in-login) if needed. Adding a browser-only preset does not check live connectivity.

For regular providers, the daemon writes the manifest, stores any supplied key **exclusively** in the scope's `.env` (`~/.config/tasksmith/.env` or `<workspace-root>/.env`, with `.env` added to `.gitignore` in Workspace scope), references it from the manifest as `${env:<NAME>_API_KEY}`, and reloads the workspace. The CLI then reports catalog visibility (`Added provider <name> (Active) · N catalog models visible`).

## Model Alias Editor (`F2`)

Opened with `F2` from the [model manager](#provider-model-manager) or the [model picker](#model-picker-model), the editor is one card (`Model aliases · <provider>/<model> [User|Workspace]`, with the home-shortened manifest path below) backed by one daemon endpoint, so both commands edit the same YAML. It lists two groups of checkboxes:

- **Standard aliases** (always rendered): `@default` (*Primary session & main agent model*), `@fast` (*Fast subagents*), `@balanced` (*General coding & subagent tier*), `@smart` (*Stronger reasoning, review & planning*), and `@flagship` (*Frontier reasoning ceiling*), each with live assignment state — `assigned to this model`, `unassigned`, `currently <provider>/<model>`, a staged `moves from <provider>/<model>` once checked, `⚠ duplicate on …` when manual YAML edits collided (with `— saving assigns only here` appended while the row is checked), or `⚠ WORKSPACE.md overrides @<alias>` when `WORKSPACE.md` `spec.modelAliases` (or, for `@default`, `spec.defaultModels.completion`) redirects the alias — saving a manifest cannot move those, and the override warning outranks duplicate warnings. `@default` also warns when another provider currently wins it.
- **Custom aliases**: user-defined `@<name>` aliases currently on this model (uncheckable) plus an inline `n` prompt for a new one (lowercase letters, digits, `-`, `_`; standard names point at the checklist row instead of duplicating, and typing an alias owned elsewhere stages the move as a checked `moves from <provider>/<model>` row — or the `⚠ WORKSPACE.md overrides @<name>` warning when the workspace redirects it).

[Keyboard shortcuts](./keybindings.md#model-alias-editor-f2).

Saving sends the exact set the model should own: each reassigned alias is stripped from every other model across writable manifests **before** being added (the atomic move that keeps bare aliases 1-to-1), and checking `@default` routes through the owning manifest's `spec.defaultModels.completion` rather than a model alias. `WORKSPACE.md` `spec.modelAliases` entries are never rewritten — they shadow per-model aliases and are surfaced as a warning instead.

## Provider Sign-In (`/login`)

`/login [provider]` (aliases `/auth`, `/signin`) starts the daemon's browser sign-in. For Codex, [Claude Code](../providers/claudecode.md), and [GitHub Copilot](../providers/github-copilot.md), finish in the browser on the daemon host. Claude Code and Copilot require the corresponding `claude` or `copilot` CLI on that host. Neither uses callback paste, `--no-browser`, or a headless device-login flow. For Antigravity the screen also shows the authorization URL and a paste field.

[Keyboard shortcuts](./keybindings.md#provider-sign-in-login).

With no `[provider]` argument, the command resolves automatically when exactly one OAuth preset is available. To select one explicitly, use `/login antigravity`, `/login codex`, `/login claudecode`, or `/login github-copilot`. When the flow settles, the screen closes and posts the outcome to the conversation timeline. When an interactive REPL turn fails because an OAuth session expired, TaskSmith also offers an inline prompt to re-authenticate immediately. See [Subcommands](./flags-and-subcommands.md#subcommands) for signing in, checking status, or signing out from a shell.

## Daemon Browser (`/daemon`)

The connected view reports Status, Version (daemon build; `unknown` for daemons that predate version reporting), PID, Uptime, Socket, and open Workspaces. When a client launches against a daemon built from a different version, the daemon is restarted automatically only when every session is confirmed idle; otherwise the REPL opens with a warning notice telling the user how to restart the daemon (`tasksmith daemon stop` or `/daemon` → `r`) when it is safe, and plain commands print the same warning to stderr. The session sandbox backend is per-session state rather than daemon state: the REPL header shows the active session's backend next to the workspace path (`· native sandbox` / `· local sandbox`), and a workspace warning appears when kernel sandboxing is unavailable on the host and new sessions fall back to `local` isolation.

## Rules Browser (`/rules`)

Columns are Name, Status (`[Always]`, `[Loaded]`, or `[Standby]`), Scope (`[Workspace]`, `[Project]`, `[User]`, or `[Builtin]`), estimated tokens, and description. `[Loaded]` reflects rules injected in the current session, including `load_rule` calls.

## Tools Browser (`/tools`)

Columns are Name, Status (`[Eager]`, `[Deferred]`, `[Denied]`, `[Disabled]`, `[Inactive]`, or `[—]`), Scope (`[Workspace]`, `[Project]`, `[User]`, or `[Builtin]`), safety annotations (`[ReadOnly]`, `[Dangerous]`, `[OpenWorld]`), and description. Statuses are resolved for the active agent — falling back to the workspace default agent — so the same tool can be `[Eager]` for one agent and `[Deferred]` for another. `[—]` is shown when the daemon could not classify tools for an agent and the unclassified workspace listing is displayed instead. See [Tool Partitioning](../tools.md#5-tool-partitioning-eager-vs-deferred) for what each status means.
`[Disabled]` tools are unavailable through manifests — their own `spec.enabled: false` or a `WORKSPACE.md` `spec.policies.tools.exclude` entry — and never reach the model's context, but stay listed so `Space` can toggle them back. Disabling patches the manifest when the tool has a workspace-scope one and otherwise writes the `WORKSPACE.md` exclusion; enabling removes whichever opt-out exists. The title counts only available tools (`Tools (3 of 4 available)` when one is disabled). See [Enabling And Disabling Tools](../../guides/tools-and-mcp.md#inspect-and-toggle-built-in-tools-tools).
`/tools` takes no arguments; passing any is rejected before the browser opens.

## Tool Inspector

The inspector shows the tool name with its scope, status, and estimated token cost, followed by its description, `Command` (rendered argv, or `—` when the tool declares no command), `Annotations` (including `[Idempotent]` and the approval `userHint`), and pretty-printed `inputSchema` / `outputSchema` JSON blocks (`— (no schema declared)` when absent).

## Onboarding & Setup Wizard (`/setup`)

When TaskSmith is launched for the first time in an unconfigured directory, or when explicitly invoking `/setup`, the interactive wizard shows the animated TaskSmith mascot and the current workspace path in its header. It guides configuration across seven screens:

1. **Trust Prompt**: Review detected executable configurations (`.agents/tools/`, `.agents/mcps/`, `.env`) and grant folder or parent trust, or proceed in restricted mode.
2. **Provider Source**: Choose whether to reuse machine-wide global providers or configure project-specific credentials.
3. **Provider & Scope**: Select upstream AI provider preset and target configuration scope (`User`, `Workspace`, or `Project`).
4. **Credentials**: Enter and verify provider API keys, authenticate via browser OAuth (e.g. Google Cloud Code for Antigravity), or configure custom endpoints with real-time verification.
5. **Default Mode**: Choose the initial permission mode (`default`, `accept_edits`, `plan`, `ask`, `auto`).
6. **Tools & MCPs**: Selectively enable or disable built-in tools and global MCP servers. Skipped when the chosen provider scope is `User`, since these choices are recorded as repository `WORKSPACE.md` policy exclusions.
7. **Review & Write**: Review configured settings, write out manifests, and optionally offer `/init`.

For the **Claude Code** and **GitHub Copilot** presets, install `claude` or `copilot` on the daemon host before setup and complete the vendor's sign-in in the browser there. See [Provider Sign-In](#provider-sign-in-login).

Both `/setup` and [Add Provider](#adding-a-provider) install all preset models and keep unclaimed aliases. An alias assigned to another model, including through an inherited provider or a `WORKSPACE.md` override, is omitted from the new manifest. The result names each skipped alias and its owner. Use `F2` to move a provider alias intentionally, or edit `spec.modelAliases` for a workspace override.

[Keyboard shortcuts](./keybindings.md#onboarding--setup-wizard-setup).

## Sandbox Editor (`/sandbox`)

A three-tab editor over the workspace's sandbox settings, backed by the daemon's `/v1/sandbox` API. Project-scope changes persist to `$XDG_DATA_HOME/tasksmith/workspaces/<hash>/sandbox.json` and Global-scope changes to `~/.config/tasksmith/sandbox.json`. Changes apply to the selected scope (`Project` or `Global`) immediately and take effect for sessions created afterwards; the currently active session keeps its backend, shown on the status banner. The scope selector renders inline in the tab strip (`scope: [Project] · Global`) on the two editable tabs, and an in-flight write shows as `· saving…` in the strip, so rows never shift vertically.

| Tab | Content |
| :--- | :--- |
| **Policy** (`1 Policy`) | A live status banner — active session backend, host kernel backend health (`✔ … available` / `⚠ … unavailable` with the availability error and a word-wrapped `Fix: <remediation>` line, e.g. the Ubuntu 24.04 AppArmor unprivileged-userns restriction), and workspace trust — above an aligned table of the scope's settings (`Setting`, `Effective`, `<Scope> override`). The `mode` row cycles `native → local → inherit`; the `failIfUnavailable`, `allowUnsandboxed`, and `autoAllowBash` posture knobs show effective values with the scope's own override (`true`/`false`/`inherited`). Knobs pinned by the *other* scope or the workspace trust floor show `🔒 locked (<reason>)` and refuse edits; a scope is never locked by its own settings, and the Global scope is never locked, so every lock can be undone where it was written. A fixed two-row detail footer under the table shows the highlighted row's description, JSON key, and lock guidance. |
| **Exclusions** (`2 Exclusions (<n>)`) | The scope's `excludedCommands` patterns; add (`a`), edit in place (`e`), and remove (`d`) entries through a guided inline input. The list windows with the viewport when it grows long, and in Project scope a read-only `Inherited from Global (<n>)` section shows Global patterns. The tab warns whenever effective `allowUnsandboxed` is off, since excluded commands still run sandboxed then. |
| **Effective** (`3 Effective`) | Read-only, scrollable view of the effective merge: posture (mode, fail-if-unavailable, allow-unsandboxed, auto-allow bash, network mode with allowed/denied domains, union of excluded commands) plus filesystem rules (working directory and the canonical `allowWrite`, `denyWrite`, `denyRead`, `allowRead` path rules, home-shortened, with empty lists collapsing to `none`). A `lines X-Y of Z` footer tracks the viewport. |

[Keyboard shortcuts](./keybindings.md#sandbox-editor-sandbox).

## Status Line Editor (`/statusline`)

Preview changes to the [status line fields](./config-files.md#status-line-fields); save to apply or discard to restore the original layout.

[Keyboard shortcuts](./keybindings.md#status-line-editor-statusline).

## Warnings View (`/warnings`)

Lists active workspace warnings (including provider, model alias, and sandbox warnings) and MCP health warnings, with remediation guidance where available. Submit `/warnings` or click the status-line warning count in a mouse-enabled terminal when no other interactive view is open.

| Warning or notice | Where it appears |
| :--- | :--- |
| Routine workspace, provider, model alias, and MCP health warnings | `/warnings` and its status-line count. Passive MCP refresh does not announce changes in the transcript. |
| Workspace sandbox warnings | Header banner, `/warnings`, and its status-line count. |
| Operation-specific notices and failures, security notices, and explicit `/mcp` outcomes | Inline as before. |

The view does not rewrite previously printed scrollback. See the [CLI guide](../../guides/cli.md) for the workflow.

[Keyboard shortcuts](./keybindings.md#warnings-view-warnings).

## Turn Spinner and Summary

| Element | Format | Source |
| :--- | :--- | :--- |
| Spinner statistics | `(<elapsed> · ↑<input> ↓<output>)` | Elapsed time since the turn started streaming. `↑` and `↓` are the tokens sent and received in the turn, summed across its calls (not the context size): the session's token growth since the turn started plus the running usage of the in-flight call (`usage` on the stream chunk, cumulative per call). They start at zero. |
| Turn summary line | `<Verb> for <elapsed> · ↑<input> ↓<output> · <cost> · done <time>` | `turn_completed` timeline entry written by the daemon when the turn ends. Canceled and failed turns use `Stopped after` and `Failed after`. Completed turns under 5 seconds are skipped. The cost part is omitted when every call was subscription-billed. |
| Fork marker line | `⑂ Forked from "<title>"` / `⑂ Forked into "<title>"` | `forked` timeline entry written by the daemon when a session is forked. The forked session records `Forked from` naming the source; the source session records `Forked into` naming the new fork. |

## Usage Browser (`/usage`)

| Tab | Content |
| :--- | :--- |
| **Session** (`1`) | Total cost; API and wall-clock duration; code changes (lines added and removed); usage by model (uncached input, cache read, cache write, output, cost); prompt cache (requests, share of input from cache, misses, expected rebuilds after compaction, last use); cache read and write cost; tool calls with per-tool calls, errors, denied, canceled, total and slowest time, and lines changed; usage insights. |
| **Workspace** (`2`) | The selected range for the current workspace: cost and trend versus the previous period, sessions and average cost per session, API duration, code changes and cost per 100 changed lines, tokens, cache, usage grouped by the selected dimension, top sessions by cost, tools, insights. |
| **Global** (`3`) | The same as Workspace across all workspaces, plus usage by workspace and top sessions across workspaces. |
| **Quotas** (`4`) | Live multi-bucket quota snapshots of every quota-billed workspace provider: models sharing the same bucket IDs appear under a `Shared pool (N models)` list, with one bar, optional unit usage (`used/limit unit`), `unlimited` badge, and `resets in <countdown> at <HH:MM>` line per bucket. The session's model is marked `current`; each metered bar is gauged by remaining capacity — red under 20%, yellow under 50%, green from 50% up. Updates live from `quota_updated` events and `r` re-reads. |

[Keyboard shortcuts](./keybindings.md#usage-browser-usage).

Input tokens are shown as uncached, cache read and cache write, which do not overlap. A cost prefixed with `≥` is a lower bound because some calls had no pricing. Subscription-billed calls never count as unpriced: they are reported separately (`N of M calls were subscription-billed`). Insights (large-context share, cache write share, cache misses, compaction share, idle sessions, unpriced models, subscription-billed calls, provider failure rates, tool error, denial and slow-tool rates, spend trend) appear only when a fixed threshold is met. A cache miss is a drop in cache read versus the previous chat call; calls after a compaction are counted as expected rebuilds. The cause of a miss is not guessed: the view shows the time since the previous call.

## Stats Browser (`/stats`)

`/stats` answers how fast inference is, where `/usage` answers how much it cost. It reads the same metrics records and covers chat calls only: compaction re-reads a whole context and title generation is a tiny call, so including either would distort prompt speed.

| Section | Content |
| :--- | :--- |
| **Throughput** | Prompt processing in tokens per second over uncached input, token generation in tokens per second over output, time to first token and end-to-end latency as an average plus the slowest call. |
| **Per model** | One row per model with requests, prompt tokens (uncached input), output tokens, cached tokens, total tokens, and generation speed in tokens per second, ordered by total tokens. |
| **Footnote** | Cost for the range (or `not priced` when no call had pricing), which speeds were inferred and from what, or how many calls carried timing, and a note that only chat calls are counted. |

[Keyboard shortcuts](./keybindings.md#stats-browser-stats).

The Session tab re-reads its own metrics every 15 seconds while it is open; Workspace and Global are scanned on demand with `r`, because those ranges grow with history and gain nothing from a background query.

**Reported vs inferred speeds.** Only providers that return a per-call timing split (Ollama reports prompt-eval and eval durations) let `/stats` divide tokens by the exact phase that moved them. Providers speaking the OpenAI protocol return no such split, so both speeds are derived from the timings that do exist and are prefixed with `~`: prompt processing uses time to first token, since no token can arrive before the prompt has been read, and token generation uses total duration minus time to first token. Because time to first token also carries queueing and network overhead, the derived prompt speed is a floor rather than an estimate of the true rate. Deriving generation requires both timings to cover the same calls; when they do not, the figure stays `—` rather than inventing a number. A speed whose timing covers fewer calls than the range contains is also marked `~`, because it divides every call's tokens by part of the time. The footnote names whichever speeds were inferred.

Averages are taken over the calls that recorded the relevant timing, so a speed is never diluted by calls that measured nothing. A figure shown as `—` was not measured at all rather than being zero. Today begins at local midnight. A cost prefixed with `≥` is a lower bound because some calls had no pricing; a model with no pricing at all reads `not priced` rather than `≥ $0.00`.

## See Also

- [CLI reference index](../cli.md)
- [Sessions guide](../../guides/sessions.md)
- [Modes and planning guide](../../guides/modes-and-planning.md)
- [Models and providers guide](../../guides/models-and-providers.md)
- [Tools and mcp guide](../../guides/tools-and-mcp.md)
