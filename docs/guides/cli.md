# Interactive CLI & Terminal UI

TaskSmith provides a rich, responsive Terminal User Interface (TUI) built for autonomous agent collaboration, code exploration, interactive permission auditing, and background process monitoring.

---

## Starting TaskSmith

### Launching in a Project Directory

Run `tasksmith` from any project directory to launch the interactive TUI:

```bash
cd my-project
tasksmith
```

You can target a specific directory without changing your current working directory using `--workspace` (`-w`):

```bash
tasksmith -w ~/projects/backend-api
```

### Automatic Daemon Lifecycle

TaskSmith separates its terminal interface from its core execution engine:
- The **Brain Daemon** manages workspace indexing, session state, background tasks, tool sandboxes, and provider connections.
- When you run `tasksmith`, the CLI automatically checks for a running daemon on the standard Unix domain socket (`~/.local/share/tasksmith/daemon.sock`).
- If no daemon is active, the CLI automatically spawns a detached background daemon process (`tasksmith daemon serve`) and establishes a persistent IPC connection before opening the UI.
- If you exit the CLI, long-running background tasks and session states remain safely managed by the daemon.

### Resuming Sessions

To reconnect to an existing session, supply the `--session` flag:

```bash
tasksmith --session 018f3a52-9b24-7800-842a-594247e62a1c
```

When you exit a session, TaskSmith prints the exact command needed to resume where you left off:

```text
Resume this session with:
    tasksmith --session 018f3a52-9b24-7800-842a-594247e62a1c
```

You can also browse and switch between past sessions interactively at any time using the `/sessions` slash command.

---

## TUI Layout & Views

The TaskSmith terminal interface is organized into a clean vertical flow:

```text
┌────────────────────────────────────────────────────────────────────────┐
│  ▲ TaskSmith v0.2.0                                                    │
│  anthropic/claude-sonnet-4-5 with auto effort                          │
│  ~/projects/acme-service                                               │
│  ⚠ Provider "anthropic" has no API key set in .env                     │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│  User: Add structured JSON logging to the HTTP server                  │
│                                                                        │
│  ○ Thinking (analyzing main.go and logger package)...                  │
│  ● Read internal/server/server.go (42 lines, 1.4 KB)                   │
│  ● Edit internal/server/server.go (+12/-3)                             │
│                                                                        │
│  I've updated the server logging configuration to output JSON.         │
│                                                                        │
├────────────────────────────────────────────────────────────────────────┤
│  [default] ❯                                                          │
│  Type a message, /command, or !bash                                    │
└────────────────────────────────────────────────────────────────────────┘
```

### 1. Header Banner & Workspace Warnings

The top banner displays:
- **Logo & Version**: Current TaskSmith build version (e.g., `v0.2.0`).
- **Active Model & Reasoning Effort**: The daemon-resolved model and current reasoning effort tier (e.g., `anthropic/claude-sonnet-4-5 with auto effort`). Draft selections are resolved by the daemon; context usage is available after the session is created.
- **Workspace Path**: The normalized workspace path (shortening the user home directory to `~`).
- **Status & Warning Indicators**: If a provider lacks an API key, or if a configuration file contains an issue, a yellow warning banner (`⚠`) highlights the problem with troubleshooting instructions.

### 2. Streaming Transcript

As the agent works, the transcript streams live updates directly into your terminal:
- **Collapsible Thinking Blocks**: Models with extended reasoning capabilities display an animated `○ Thinking (...)` block while deliberating, which collapses into a compact `● Thought (...)` entry once generation begins.
- **Tool Execution Cards**: Each tool invocation appears as a structured card displaying:
  - **Status Dot**: Green (`●`) on success, red (`●`) on failure, or pulsing (`○`) while executing.
  - **Action Verb & Target**: Clearly labeled operations such as `Read internal/config.go`, `Write src/util.ts`, `Bash go test ./...`, or `Fetch api.github.com`.
  - **Metrics & Timing**: Execution duration in milliseconds or seconds, bytes transferred, and unified line change statistics (`+14/-2`).
  - **Diff Previews**: Inline unified diffs for `edit` and `write` tools showing exact additions and deletions in color.
- **Warnings**: Model configuration warnings appear in the transcript with a colored left border. Use `/clear` to dismiss them.
- **Markdown Rendering**: Agent responses render GitHub-flavored Markdown with bold text, lists, syntax-highlighted code blocks, and formatted tables.

### 3. Multi-Line Composer

The bottom composer is your command center:
- **Multi-line Input**: Type naturally. Press `Shift+Enter` (or `Alt+Enter`) to insert a new line. Press `Enter` to submit your prompt.
- **Prompt History**: Press `Up` or `Down` at the beginning/end of your input to cycle through previous prompts. History is persisted across sessions in `~/.local/state/tasksmith/repl.jsonl`.
- **Instant Bash Mode**: Type `!` as the first character on an empty composer line to enter interactive bash mode. The prompt updates to `! ` with a hint (`activated bash mode · esc to cancel`), allowing you to run shell commands directly through the daemon.
- **Permission Mode Cycling**: Press `Shift+Tab` to instantly cycle the active permission mode (`default` $\to$ `accept_edits` $\to$ `plan` $\to$ `ask` $\to$ `auto`).
- **Interrupt / Cancel**: Press `Esc` while an agent is streaming to immediately halt generation and cancel running tools. Press `Ctrl+C` once to clear input, or twice within 2 seconds to exit the TUI.

---

## Slash Commands & Configuration

Type `/` in the composer to open the interactive autocomplete menu. Commands and available skills, including built-in skills, appear together; type a name to filter them. Use the arrow keys to navigate and `Tab` to insert a suggestion.

Some built-in commands:

| Command | Usage | Description |
| :--- | :--- | :--- |
| `/help` | `/help` | List all available slash commands and descriptions. |
| `/model` | `/model [model_id]` | Open an interactive model picker or switch directly to a registered model. |
| `/agent` | `/agent [agent_name]` | Switch the active agent persona (e.g., `main` or a custom agent). |
| `/effort` | `/effort [level\|default]` | Select a model-supported thinking effort; the picker and autocomplete show available descriptions. |
| `/budget` | `/budget [tokens\|default]` | Show or set a model-supported thinking token budget. |
| `/thinking` | `/thinking [on\|off\|default]` | Show or change a model-supported thinking toggle. |
| `/mode` | `/mode [default\|accept_edits\|plan\|ask\|auto]` | Inspect or change the session permission mode. |
| `/sessions` | `/sessions [session_id]` | Open the interactive session browser to switch, rename (`F2`), or delete (`F4`) sessions. |
| `/new` | `/new [model]` | Start a fresh session draft. |
| `/changes` | `/changes` | Open the interactive file changes browser. |
| `/revert` | `/revert [path] [entry_id]` | Revert tracked file modifications for a specific path or the entire session. |
| `/tasks` | `/tasks` | Open the background task monitor. |
| `/permissions` | `/permissions` | Open the interactive permission rules manager. |
| `/theme` | `/theme [name]` | Switch the terminal color theme. |
| `/clear` | `/clear` | Start a fresh session draft. |
| `/compact` | `/compact [focus]` | Compact conversation history to free context window space, with optional focus instructions. |
| `/context` | `/context` | Show visual token usage grid and context window breakdown by category. |
| `/usage` | `/usage [session\|workspace\|global]` | Open the usage browser for the session, the workspace, or all workspaces. |
| `/workspace` | `/workspace [reload]` | Reload the active workspace resources from disk. |
| `/exit` | `/exit` (or `/quit`, `/q`) | Exit the REPL. |

Resolve any pending permission request or question before using `/compact`. Commands and background task notifications remain queued until the interaction is resolved.

If compaction fails, TaskSmith reports the error and preserves the existing conversation instead of continuing with an incomplete summary.

For `/effort`, `/budget`, and `/thinking`, the active model determines supported controls. Unsupported commands remain visible and report why they cannot be used. `default` clears the session thinking override and returns to the agent or provider default. When no default is known, the UI shows `provider default` and leaves the choice to the provider. Changing one control replaces the previous thinking override.

For a complete reference of flags, keybindings, and autocomplete shortcuts, see the [CLI Reference](../reference/cli.md).

---

## Interactive Modals (`<Slot />`)

When TaskSmith needs user interaction—such as requesting permission, answering agent questions, browsing sessions, or inspecting diffs—it mounts a dedicated modal inside the `<Slot />` viewport directly above the status line.

### Permission Approval Prompts

When an agent invokes a tool that requires user consent (such as executing an unsanctioned `bash` command, modifying a file outside allow rules, or fetching an external URL), the TUI suspends tool execution and displays an authorization card:

```text
┌─ Permission required ──────────────────────────────────────────────────┐
│ bash · go test ./...                                                  │
│                                                                        │
│ ❯ Yes, allow once                                                      │
│   Always allow bash(go test *)                               [session] │
│   Always allow bash(go *)                                    [session] │
│   No, deny                                                             │
│                                                                        │
│ ↑/↓ Navigate · tab Scope · enter Confirm · r Feedback                 │
└────────────────────────────────────────────────────────────────────────┘
```

- **Navigate**: Use `↑` / `↓` (or `k` / `j`) to choose an approval level:
  - **Yes, allow once** (`y` or `Enter` on row 0): Grants permission exclusively for this single tool call.
  - **Always allow `<pattern>`** (`a` or `Enter` on pattern rows): Grants permission for matching calls.
  - **Scope Cycling**: Press `Tab` to cycle the persistence scope between `[session]` (in-memory for current session), `[project]` (saved to workspace rules), or `[global]` (saved to `~/.config/tasksmith/permissions.json`).
  - **No, deny** (`n` or `Enter` on last row): Denies execution.
- **Reject with Feedback (`r`)**: Press `r` to open an inline feedback field. You can explain *why* the action was rejected (e.g., *"Do not delete this file; modify the existing implementation instead"*), returning the feedback to the agent so it can course-correct.

For full details on rules and pattern syntax, see the [Permissions Guide](./permissions.md).

### Structured Agent Questions (`ask_question`)

When an agent needs human guidance or clarifying requirements, it invokes the built-in `ask_question` tool. Permission rules apply; in `ask` mode, approve the tool call before the question form appears unless an allow rule matches. Denied calls do not show a question.

The interactive question form supports:

- **Single Select**: Use `↑` / `↓` to highlight an option, then press `Enter` to select. Selecting the final *"Custom answer..."* option opens an inline input field to enter free-form text.
- **Multi-Select**: Use `↑` / `↓` to highlight options and press `Space` to toggle selections (`[✓]` / `[ ]`). Press `Enter` to confirm your selections.
- **Confirmation**: Select between `Yes` and `No`.
- **Multi-Step Questionnaires**: When an agent asks multiple questions at once, a step bar (`Step 1 of 3`) lets you navigate back and forth using `Left` / `Shift+Tab` and `Right` before final submission. Press `Esc` to cancel the questionnaire.

---

## File Changes, Diffs & Revert

TaskSmith automatically tracks every file created, modified, or deleted by tools during a session.

### The Changes Browser (`/changes`)

Run `/changes` to open the Tracked Changes browser:

```text
┌─ Tracked Changes ──────────────────────────────────────────────────────┐
│ Path                               Kind         Changes                │
│ ❯ internal/server/server.go        modified     +14/-2                 │
│   internal/config/config.go        modified     +8/-0                  │
│   pkg/logger/json.go               created      +45/-0                 │
│                                                                        │
│ ↑/↓ Select · enter View diff · r Revert · esc Close                   │
└────────────────────────────────────────────────────────────────────────┘
```

- **Filter**: Start typing to filter changed files by name.
- **View Diff**: Press `Enter` on any file to open a full, colored unified diff viewer inside `<Slot />`. Press `Esc` to return to the file list.
- **Revert File**: Press `r` while highlighting a file to open a confirmation dialog (`Revert changes to internal/server/server.go?`). Confirming reverts the file on disk to its baseline state before the session began.
- **Revert Entire Session**: Run `/revert` in the composer to roll back all file modifications across the workspace.

---

## Turn Progress and Summary

A turn runs from your message until the agent's final answer, including every model call, tool run and automatic compaction in between.

- **While the agent works**, the spinner shows the elapsed time and the tokens used so far, for example `⠋ Thinking...  (12s · ↑12.4k ↓1.8k)`. `↑` is the total tokens sent and `↓` the total received in this turn, summed across all of its model calls. They are not the context size (the status line shows that). They start at zero and count up, including the call in flight when the provider reports running usage, and otherwise update as each call finishes.
- **When the turn ends**, a dim summary line is added to the conversation, for example `Smithed for 42s · ↑12.4k ↓1.8k · $0.03 · done 2:15 PM`, where `↑` and `↓` are the tokens sent and received in the whole turn. The verb comes from a small rotating set (Smithed, Forged, Crafted, Tempered, Worked). Canceled and failed turns read `Stopped after 3.1s` and `Failed after 9.0s`. A cost with a leading `≥` is a lower bound because some calls had no pricing.
- Completed turns shorter than 5 seconds get no summary line. A turn waiting for a permission or question is not finished, and its elapsed time keeps counting.

The summary is stored in the session timeline, so it reappears when you reopen or switch back to the session.

---

## Usage and Cost (`/usage`)

TaskSmith records every LLM call (chat turns, compaction and title generation, including failed and canceled calls), every tool execution, and every file change. The status line shows the session's running cost and token total; run `/usage` to open the usage browser in the slot above the status line:

```text
Usage
[1 Session]  2 Workspace  3 Global

Total cost:            $0.42
Total duration (API):  41s
Total duration (wall): 18m 20s
Total code changes:    312 lines added, 48 lines removed
Usage by model:
    anthropic/claude-sonnet:  20.0k uncached input, 76.0k cache read, 8.0k cache write, 6.2k output ($0.35)
Prompt cache:          12 requests · 79% of input tokens from cache · 2 misses (last 6m 00s ago; 1h 02m since the previous call; 8.0k tokens re-cached)
Tool calls:            41 total · 38 ok · 1 error · 2 denied · 0 canceled
```

- **Tabs** (`1`/`2`/`3` or `←`/`→`): the current session, the current workspace, and all workspaces. `/usage workspace` and `/usage global` open the browser on that tab.
- **Group by** (`m` model, `p` provider, `a` agent, `u` purpose, `i` pricing tier, `d` day) and **range** (`t`: 24 hours, 7 days, 30 days, all time) apply to the Workspace and Global tabs.
- **Tools** show calls, errors, permission denials, cancellations, total and slowest time, and lines changed per tool.
- **Insights** such as "40% of cost came from calls above 150k context" or "`bash` failed 40% of its calls" appear only when they apply, each with a short hint.

Input tokens are shown split into **uncached**, **cache read** and **cache write**, so the three never overlap. Cost is estimated from the model's `cost` pricing when the call is made and then frozen, so later price changes don't rewrite history. If a model has no pricing, its calls are **unpriced**: they still count toward calls and tokens, but their cost is unknown, so totals are shown as a lower bound (for example `≥ $1.23`) with a note. Usage survives session deletion.

---

## Background Tasks Monitor (`/tasks`)

When an agent runs a persistent daemon command, dev server, or test suite using the background tasks capability, TaskSmith runs the process in the background and reports its status in the TUI:

- **Active Task Indicator**: While background tasks are active, a compact status badge appears above the status bar.
- **Opening the Task Monitor**: Run `/tasks` to open the Background Tasks manager in `<Slot />`:

```text
┌─ Background Tasks ─────────────────────────────────────────────────────┐
│ [17:42:10] npm run dev (vite --port 3000)                              │
│ ❯ [17:43:05] go test -v ./...                                          │
│                                                                        │
│ ↑/↓ Select · enter View output · a Show all · esc Close               │
└────────────────────────────────────────────────────────────────────────┘
```

- **Live Streaming Output**: Press `Enter` on any task to view a live, auto-scrolling log viewer tailing the last 50 lines of output.
- **Terminate Tasks (`x`)**: Press `x` while highlighting a running or pending task to terminate the background process immediately.
- **Toggle View (`a`)**: Press `a` to toggle between showing only currently running tasks or all session tasks (including completed or canceled processes).
