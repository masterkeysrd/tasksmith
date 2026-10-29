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

Inside the TaskSmith interactive REPL, slash commands provide instant control over session state, permission modes, thinking, and resource inspection. When no thinking default is known, the UI shows `provider default` and leaves the choice to the provider:

| Slash Command | Aliases | Arguments | Description |
| :--- | :--- | :--- | :--- |
| **`/help`** | `/h`, `/?` | *(none)* | Opens the interactive slash command browser and help viewer. |
| **`/mode`** | | `[mode]` (`default`, `accept_edits`, `plan`, `ask`, `auto`) | Inspects current permission mode or switches to the specified mode. |
| **`/model`** | | `[model_id]` | Displays active model, switches to `model_id`, or opens the interactive model picker if no argument is provided. |
| **`/agent`** | | `[agent_name]` | Displays active agent, switches to `agent_name`, or opens the interactive agent picker if no argument is provided. |
| **`/effort`** | | `[level\|default]` | Set an allowed model-specific effort or open the picker, which shows available descriptions. |
| **`/budget`** | | `[tokens\|default]` | Show or set a supported thinking token budget. |
| **`/thinking`** | | `[on\|off\|default]` | Show or set a supported thinking toggle. |
| **`/theme`** | | `[theme_name]` | Changes terminal UI color theme, or opens the interactive theme picker if no argument is provided. |
| **`/clear`** | | *(none)* | Creates and transitions to a fresh draft session. |
| **`/exit`** | `/quit`, `/q` | *(none)* | Gracefully terminates and exits the interactive REPL. |
| **`/new`** | | `[model]` | Creates and transitions to a fresh draft session, optionally overriding the model. |
| **`/sessions`** | `/session`, `/ls` | `[session_id]` | Switches to `session_id`, or opens the interactive session browser to switch, rename, or delete sessions. |
| **`/changes`** | | *(none)* | Opens the interactive changes browser to inspect tracked file modifications, diffs, and hashes. |
| **`/revert`** | | `[path] [entry_id]` | Reverts uncommitted file mutations for a specific file, change entry, or the entire session. |
| **`/permissions`** | `/perm`, `/perms` | *(none)* | Opens the interactive permissions browser to inspect and edit project and global allow/deny rules. |
| **`/tasks`** | | *(none)* | Opens the background task manager to inspect running commands and stream task logs. |
| **`/compact`** | | `[focus]` | Compacts conversation history to free context window space, with optional focus instructions. Requires pending permissions and questions to be resolved first. |
| **`/context`** | | *(none)* | Displays visual token usage grid and detailed context window breakdown by category. |
| **`/skills`** | | *(none)* | Opens the interactive skills browser to inspect runbooks, token estimates, and reload workspace skills. |

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

### Skill Loading in the Transcript

Agent calls to `load_skill` show the skill name and loading, loaded, or failed status. Failures include the first error line below the status. Skill arguments and rendered instructions are not displayed in the widget.

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

### Autocomplete & History

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Tab` | Autocomplete | Inserts or completes the currently highlighted autocomplete suggestion. |
| `↑` / `↓` | Autocomplete | Navigates candidate suggestions up and down when the autocomplete menu is visible. |
| `↑` / `↓` | Composer | Cycles backward and forward through prompt history when autocomplete is inactive. |

### Modals & Pickers (General)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `k` | Modals & Pickers | Move selection cursor up. |
| `↓` / `j` | Modals & Pickers | Move selection cursor down. |
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
| **Workspace DB** | `~/.local/share/tasksmith/workspaces/<hash>/tasksmith.db` | SQLite database storing turn journals, messages, and session state (`$XDG_DATA_HOME`). | Permanent |
| **Project Rules** | `~/.local/share/tasksmith/workspaces/<hash>/permissions.json` | Project-scoped persistent permission allow and deny rules. | Permanent |
| **Fallback Socket** | `~/.local/share/tasksmith/daemon.sock` | Fallback Unix domain socket path when `/tmp` runtime dir is unavailable. | Ephemeral |
| **Runtime Socket** | `/tmp/tsd-<uid>/b.sock` | Active Unix domain IPC socket used by CLI and daemon (`$XDG_RUNTIME_DIR`). | Ephemeral |
| **Process Lock** | `/tmp/tsd-<uid>/daemon.lock` | File lock ensuring a single background daemon runs per user. | Ephemeral |
| **Daemon Logs** | `~/.local/state/tasksmith/logs/daemon.jsonl` | Structured JSON Lines log file with daily rolling rotation (`$XDG_STATE_HOME`). | Permanent |
| **Workspace Root** | `<workspace-root>/WORKSPACE.md` | Workspace declaration, project boundaries, defaults, and security policies. | Permanent |
| **Workspace Env** | `<workspace-root>/.env` | Workspace-scoped secrets and environment variables. | Permanent |
| **Workspace Agents** | `<workspace-root>/.agents/` | Workspace-tier agents, skills, commands, decisions, and tool manifests. | Permanent |
