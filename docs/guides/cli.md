# Interactive CLI & Terminal UI

TaskSmith provides a rich, responsive Terminal User Interface (TUI) built for autonomous agent collaboration, code exploration, interactive permission auditing, and background process monitoring.

---

## Installation

Install prebuilt binaries on macOS or Linux via **Homebrew**:

```bash
brew tap masterkeysrd/tasksmith https://github.com/masterkeysrd/tasksmith
brew install masterkeysrd/tasksmith/tasksmith
```

Install via the **shell installer** (installs to `~/.local/bin` with SHA-256 checksum verification):

```bash
curl -fsSL https://github.com/masterkeysrd/tasksmith/releases/latest/download/install.sh | sh
```

Or install from source using **Go 1.26+**:

```bash
go install github.com/masterkeysrd/tasksmith/cmd/tasksmith@latest
```

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
- When you run `tasksmith`, the CLI automatically checks for a running daemon on the standard Unix domain socket (`$XDG_RUNTIME_DIR/tasksmith/brain.sock`).
- If no daemon is active, the CLI automatically spawns a detached background daemon process (`tasksmith daemon serve`) and establishes a persistent IPC connection before opening the UI.
- If you exit the CLI, long-running background tasks and session states remain safely managed by the daemon.

### Resuming Sessions

To reconnect to an existing session, supply the `--session` flag:

```bash
tasksmith --session 018f3a52-9b24-7800-842a-594247e62a1c
```

To jump straight back into the workspace's most recently updated session without remembering its ID, use `-c` (`--continue`) — on the REPL, `exec`, or the `--print` one-shot:

```bash
tasksmith -c
```

`-c` is ignored when `--session` is given, and simply starts a fresh session when the workspace has none.

To branch a resumed session instead of continuing it in place, add `--fork` — the run attaches to a new session that starts from the source's full conversation, leaving the source untouched (`--title` optionally names the branch):

```bash
tasksmith -c --fork
tasksmith exec --session 018f3a52-9b24-7800-842a-594247e62a1c --fork "Try the other approach"
```

`--fork` needs a session to branch, so it is a usage error when neither `--session` nor `-c` resolves one. Inside a session, `/fork [title]` branches at any point — in the REPL it switches to the branch, and in headless `jsonl` runs the event stream follows the fork from that point on.

When you exit a session, TaskSmith prints a goodbye block pairing the brand mark with the session's changes, cost (or remaining quota for subscription-billed providers), token totals, and the exact command needed to resume where you left off:

```text
 ◥██████████◣   Changes    +42 -11 (5 files)
     ▐██▌       Cost       $0.18 (1m 02s)
    ◢████◣      Tokens     ↑ 25.1k (21.4k cached, 300 written) • ↓ 1.2k
                Resume     tasksmith --session 018f3a52-9b24-7800-842a-594247e62a1c
```

The `Cost` row reads `—` when calls have no pricing data, and switches to a `Quota` row (remaining `%` plus the reset countdown) when the session model is billed by quota. Colors render only on an interactive terminal; `NO_COLOR` and piped output stay plain.

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
- **Status & Warning Indicators**: If a provider lacks an API key or a configuration file contains an issue, a yellow warning banner (`⚠`) highlights the problem with troubleshooting instructions. Only problems that stay true until the workspace is re-read appear here; MCP server health is reported in the transcript instead, because it changes while you work.

### 2. Streaming Transcript

As the agent works, the transcript streams live updates directly into your terminal:
- **Collapsible Thinking Blocks**: Models with extended reasoning capabilities display an animated `○ Thinking (...)` block while deliberating, which collapses into a compact `● Thought (...)` entry once generation begins.
- **Tool Execution Cards**: Each tool invocation appears as a structured card displaying:
  - **Status Dot**: Green (`●`) on success, red (`●`) on failure, or pulsing (`○`) while executing.
  - **Action Verb & Target**: Clearly labeled operations such as `Read internal/config.go`, `Write src/util.ts`, `Bash go test ./...`, or `Fetch api.github.com`.
  - **Metrics & Timing**: Execution duration in milliseconds or seconds, bytes transferred, and unified line change statistics (`+14/-2`).
  - **Diff Previews**: Inline unified diffs for `edit` and `write` tools showing exact additions and deletions in color.
- **Warnings**: Model configuration warnings and MCP server health — a server that needs `/mcp auth <name>` or failed to connect — appear in the transcript with a colored left border. Use `/clear` to dismiss them.
- **Markdown Rendering**: Agent responses render GitHub-flavored Markdown with bold text, lists, syntax-highlighted code blocks, and formatted tables.

### 3. Multi-Line Composer

The bottom composer is your command center:
- **Multi-line Input**: Type naturally. Press `Shift+Enter` (or `Alt+Enter`) to insert a new line. Press `Enter` to submit your prompt.
- **Context Mentions (`@`) & Multimodal Files**: Type `@` in a prompt to trigger live fuzzy file and directory autocomplete. Results are ranked with recently read/modified files first. Selecting an entry inserts its shortest unique workspace suffix (e.g. `@composer.go `, `@cli/app.go `, `@screenshot.png `, `@spec.pdf `, or `@docs/ `), quoted if containing spaces. Explicit `@file:<path>` syntax is also supported. Text files support optional line ranges (e.g. `@composer.go#L10-L20` or `#L42`); supported images (`.png`, `.jpg`, `.webp`, `.gif`), PDFs, and audio files (`.wav`, `.mp3`, `.m4a`, `.ogg`, `.flac`) are snapshotted into `<session-dir>/media/<sha256>` and sent to multimodal models (while `.svg` stays on the text path). Typing `@rule:` opens rule autocomplete, `@skill:` opens skill autocomplete, and `@mcp:` opens MCP resource completion.
- **Clipboard Image Paste**: Press `Ctrl+V` or `Alt+V` in the composer when an image is on your clipboard to stage it as an inline `[Image #1]` pill; delete the `[Image #1]` token from your draft to detach it before sending. If the active model does not support the attached modality, TaskSmith omits the media block cleanly with a notice rather than poisoning the turn. If you later switch to a model that drops a modality active in the current context, the REPL asks whether to **Switch anyway** (omitting older attachments on future turns) or **Cancel**.
- **Prompt History**: Press `Up` or `Down` at the beginning/end of your input to cycle through previous prompts. While browsing, the composer's bottom border becomes a position rule (`───── history (3/42) ─────`) and autocomplete stays closed, so recalling a `/command` or `@` mention won't pop the completion menu. Press `Esc` to abandon browsing and get back the draft you were typing, or just start typing to keep the recalled entry as your new draft. History is persisted across sessions in `~/.local/state/tasksmith/repl.jsonl`.
- **Instant Bash Mode**: Type `!` as the first character on an empty composer line to enter interactive bash mode. The prompt updates to `! ` with a hint (`activated bash mode · esc to cancel`), allowing you to run shell commands directly through the daemon.
- **Permission Mode Cycling**: Press `Shift+Tab` to instantly cycle the active permission mode (`default` $\to$ `accept_edits` $\to$ `plan` $\to$ `ask` $\to$ `auto`).
- **Interrupt / Cancel**: Press `Esc` while an agent is streaming to immediately halt generation and cancel the active turn (and any user-invoked delegated `/command`), while background tasks and autonomous `invoke_agent` subagents keep running until stopped via `/tasks` or `/subagents` (`x`). Press `Ctrl+C` once to clear input, or twice within 2 seconds to exit the TUI.

### 4. Status Line

The status line under the composer shows configurable segments: the shortcut hint, the permission mode badge, context window usage, session cost/tokens, and the model with its thinking effort. The first enabled segment sits on the left, the rest on the right.

- **Configure it**: Run `/statusline` to open an editor with a live preview — toggle segments (`Space`), reorder them (`←`/`→`), switch compact mode (`c`), then save (`s`) or discard (`Esc`). The composition persists to `~/.config/tasksmith/statusline.json`, which you (or the agent) can also edit directly; file edits apply on the next REPL start.
- **Narrow terminals**: When the window is too narrow, segments shrink or hide in a fixed order (model, then usage, then hint, then context). The permission mode badge is never hidden, because it is safety-relevant.

### 5. Terminal Window Status

TaskSmith also reports the session's live state to the terminal itself, so background tabs, taskbars, and multiplexers stay informative without looking at the pane:

- **Window/tab title**: the session title once the daemon has generated one, falling back to the workspace directory name — with no `TaskSmith` prefix, so narrow tab bars show as much of the real label as possible.
- **Native progress bar**: Terminals that support the OSC 9;4 progress sequence show an indeterminate bar while the agent works, a paused bar when it is waiting on you (a permission prompt or an agent question), and an error bar when a turn fails.
- **Program status**: Terminals that speak the Program Status Protocol (OSC 7501) receive a structured record (`working`, `blocked` with the reason — permission, question, or auth — `done`, `error`, or `idle`), which is what powers richer tab badges and notifications.

Everything is capability-probed: a terminal that does not support one of these sequences simply never receives it. The title and status are cleared when the REPL exits, so your shell tab never keeps a stale TaskSmith state.

---

## Slash Commands & Configuration

Type `/` in the composer to open the interactive autocomplete menu. Commands and available skills, including built-in skills, appear together; type a name to filter them. Use the arrow keys (or `Ctrl+P`/`Ctrl+N`) to navigate and `Tab` to insert a suggestion.

Some built-in commands:

| Command | Usage | Description |
| :--- | :--- | :--- |
| `/help` | `/help` | List all available slash commands and descriptions. |
| `/model` | `/model [--force] [model_id]` | Open an interactive picker of the chat-capable models or switch directly to one, by id or `@alias`. |
| `/agent` | `/agent [agent_name]` | Switch the active agent persona (e.g., `main` or a custom agent). |
| `/effort` | `/effort [level\|default]` | Select a model-supported thinking effort; the picker and autocomplete show available descriptions. |
| `/budget` | `/budget [tokens\|default]` | Show or set a model-supported thinking token budget. |
| `/thinking` | `/thinking [on\|off\|adaptive\|default]` | Show or change a model-supported thinking toggle or adaptive strategy. |
| `/mode` | `/mode [default\|accept_edits\|plan\|ask\|auto]` | Inspect or change the session permission mode. |
| `/plan` | `/plan [prompt]` | Switch to `plan` mode, optionally submitting a planning prompt immediately. |
| `/sessions` | `/sessions [session_id]` | Open the interactive session browser to switch, rename (`F2`), fork (`F3`), or delete (`F4`) sessions. |
| `/new` | `/new [model]` | Start a fresh session draft. |
| `/fork` | `/fork [title]` | Branch the current session into a new top-level session that keeps the conversation, and switch to it. |
| `/changes` | `/changes` | Open the interactive file changes browser. |
| `/revert` | `/revert [path] [entry_id]` | Revert tracked file modifications for a specific path or the entire session. |
| `/tasks` | `/tasks` | Open the background task monitor. |
| `/subagents` | `/subagents` (or `/subagent`) | Open the subagents browser and live session inspector. |
| `/permissions` | `/permissions` | Open the interactive permission rules manager. |
| `/sandbox` | `/sandbox` | Open the interactive sandbox configuration editor to inspect kernel sandboxing health and edit isolation settings per scope. |
| `/rules` | `/rules` | Open the workspace rules browser to inspect rule status and triggers, or reload rules from disk. |
| `/tools` | `/tools` | Open the workspace tools browser to inspect each tool's status for the active agent, its safety annotations and schemas, or reload tools from disk. |
| `/mcp` | `/mcp` (or `/mcps`), `/mcp auth <name>`, `/mcp restart <name>` | Open the MCP browser to inspect servers and everything they offer, authorize or restart one, and run prompts or attach resources. |
| `/theme` | `/theme [name]` | Switch the terminal color theme. |
| `/clear` | `/clear` | Start a fresh session draft. |
| `/compact` | `/compact [focus]` | Compact conversation history to free context window space, with optional focus instructions. |
| `/context` | `/context` | Show visual token usage grid and context window breakdown by category. |
| `/usage` | `/usage [session\|workspace\|global\|quotas]` | Open the usage browser for the session, the workspace, all workspaces, or quota windows. |
| `/stats` | `/stats [session\|workspace\|global]` | Open the inference speed browser: prompt processing and token generation rates per model. |
| `/workspace` | `/workspace [reload]` | Reload the active workspace resources from disk. |
| `/exit` | `/exit` (or `/quit`, `/q`) | Exit the REPL. |

Resolve any pending permission request or question before using `/compact`. Commands and background task notifications remain queued until the interaction is resolved.

If compaction fails, TaskSmith reports the error and preserves the existing conversation instead of continuing with an incomplete summary.

For `/effort`, `/budget`, and `/thinking`, the active model determines supported controls. Unsupported commands remain visible and report why they cannot be used. `/thinking` accepts `on` and `off` for a model with a thinking toggle and `adaptive` for a model with an adaptive strategy, so a model supporting either makes the command usable. `default` clears the session thinking override and returns to the agent or model default, which the commands report as `default (<strategy>)`. When neither the agent nor the model declares a default, the UI shows `provider default` and leaves the choice to the provider. Changing one control replaces the previous thinking override.

For a complete reference of flags, keybindings, and autocomplete shortcuts, see the [CLI Reference](../reference/cli.md).

---

## Interactive Modals (`<Slot />`)

When TaskSmith needs user interaction—such as requesting permission, answering agent questions, browsing sessions, or inspecting diffs—it mounts a dedicated modal inside the `<Slot />` viewport directly above the status line.

Every modal and browser moves its cursor with `↑` / `↓`, and each accepts the readline-style `Ctrl+P` / `Ctrl+N` equivalents too, so you can scroll without leaving the home row. Page-wise scrolling works with `PgUp` / `PgDn` or `Ctrl+B` / `Ctrl+F`. Where a view also lists `k` / `j`, those keep working.

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

When an agent needs human guidance or clarifying requirements, it invokes the built-in `ask_question` tool. Permission rules and workspace policies apply; if an `ask` policy covers `ask_question`, approve the tool call before the question form appears unless an allow rule matches. Denied calls do not show a question.

The interactive question form supports:

- **Single Select**: Use `↑` / `↓` to highlight an option, then press `Enter` to select. Selecting the final *"Custom answer..."* option opens an inline input field to enter free-form text.
- **Multi-Select**: Use `↑` / `↓` to highlight options and press `Space` to toggle selections (`[✓]` / `[ ]`). Press `Enter` to confirm your selections.
- **Confirmation**: Select between `Yes` and `No`.
- **Multi-Step Questionnaires**: When an agent asks multiple questions at once, a step bar (`Step 1 of 3`) lets you navigate back and forth using `Left` / `Shift+Tab` and `Right` before final submission. Press `Esc` to cancel the questionnaire.

### Plan Review Prompts (`exit_plan_mode`)

When an agent finishes drafting a plan and calls `exit_plan_mode(path)`, the turn pauses and opens a compact **Plan review** prompt in `<Slot />` showing the plan path, line count, and four choices. Press `p` to toggle a scrollable Markdown viewer for the full plan (`p` or `Esc` returns to the choices without canceling):

- **1. Approve & auto-accept edits (`accept_edits`)**
- **2. Approve with standard prompts (`default`)**
- **3. Approve autonomous (`auto`)**
- **4. Keep planning** (`4` or `r` opens an inline feedback input so you can request revisions while staying in `plan` mode; `Esc` cancels and stays in `plan` mode).

---

## Sandbox Configuration (`/sandbox`)

Run `/sandbox` to open the interactive sandbox configuration editor. It reads the same `sandbox.json` settings described in the [Permissions Guide](./permissions.md#isolation-settings-sandboxjson) and presents them in three tabs (`Tab` / `Shift+Tab` / `←` / `→` / `1`–`3` to switch):

- **`1 Policy`**: a live status banner — the active session's backend, the host kernel backend's health (including a word-wrapped `Fix: …` remediation line when kernel sandboxing is unavailable, for example the Ubuntu 24.04 AppArmor unprivileged-userns restriction), and workspace trust — above a table of the scope's settings. `Space` / `Enter` / `t` cycle the default `mode` (`native → local → inherit`) or toggle the highlighted `failIfUnavailable` / `allowUnsandboxed` / `autoAllowBash` knob; `x` / `Backspace` clears an override so the scope inherits again. A knob pinned by the other scope or by the workspace trust floor shows `🔒 locked (…)` and refuses edits.
- **`2 Exclusions (<n>)`**: list, add (`a`), edit in place (`e`), and remove (`d`) `excludedCommands` patterns for the selected scope, with Global patterns echoed read-only in Project scope.
- **`3 Effective`**: a read-only, scrollable view of the fully merged configuration — posture (mode, fail-if-unavailable, allow-unsandboxed, auto-allow bash, network posture and domains, union of excluded commands) plus the canonical allow/deny path rules.

Press `s` on either editable tab to switch the scope between `Project` and `Global` (the selector shows inline in the tab strip). Edits are saved immediately and apply to sessions created afterwards; the active session keeps its current backend, shown on the status banner. For keybindings and tab details, see the [CLI Reference](../reference/cli.md#sandbox-editor-sandbox).

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
- **While a delegated command runs**, the spinner names the child session that owns the work instead of claiming this session is thinking, for example `⠋ Waiting on subagent (main: Staging changes)  (12s · ↑0 ↓0)`. It appears for commands configured with `context: clean` or `context: fork`, or ones targeting a different agent, which execute in a child session. The hint reads `subagent running • esc to cancel`, and `Esc` interrupts the child. The child's own progress is reported in the [bottom activity bar](#subagents--live-inspection-subagents); the spinner here only tells you the turn is still in flight and cancellable.
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

- **Tabs** (`1`/`2`/`3`/`4` or `←`/`→`): the current session, the current workspace, all workspaces, and quota windows. `/usage workspace`, `/usage global`, and `/usage quotas` open the browser on that tab.
- **Group by** (`m` model, `p` provider, `a` agent, `u` purpose, `i` pricing tier, `d` day) and **range** (`t`: 24 hours, 7 days, 30 days, all time) apply to the Workspace and Global tabs.
- **Quotas** (`4`): subscription-billed providers show one remaining-capacity bar per quota bucket (`5h`, `7d`, request pools, or `unlimited`), grouped under a `provider · account` heading with each bucket's reset countdown and optional `used/limit unit` counts. Models sharing the same bucket IDs are listed together as a shared pool, and the session's model is marked `current`.
- **Tools** show calls, errors, permission denials, cancellations, total and slowest time, and lines changed per tool.
- **Insights** such as "40% of cost came from calls above 150k context" or "`bash` failed 40% of its calls" appear only when they apply, each with a short hint.

Input tokens are shown split into **uncached**, **cache read** and **cache write**, so the three never overlap. Cost is estimated from the model's `cost` pricing when the call is made and then frozen, so later price changes don't rewrite history. If a model has no pricing, its calls are **unpriced**: they still count toward calls and tokens, but their cost is unknown, so totals are shown as a lower bound (for example `≥ $1.23`) with a note. Calls on subscription-billed providers are never counted as unpriced — they draw on a quota window instead, and their remaining capacity is on the Quotas tab. Usage survives session deletion.

---

## Inference Speed (`/stats`)

Where `/usage` reports what a session cost, `/stats` reports how fast the models behind it were. It reads the same recorded calls, counting chat calls only: compaction re-reads an entire context and title generation is a tiny call, so either one would distort prompt speed.

```text
Stats
[1 Session]  2 Workspace  3 Global    range: today

Throughput:
Prompt processing:     1255 tok/s  uncached input
Token generation:      63.8 tok/s
Time to first token:   1.3s avg  3.1s worst
End to end:            4.9s avg  21s worst

Per model:
  MODEL                       REQ    PROMPT   OUTPUT    CACHED     TOTAL    TOK/S
  anthropic/claude-sonnet      42     52.3k     9.1k    260.1k    321.5k  70.0 tok/s
  openai/gpt-5.2               17     82.0k     3.4k      6.0k     91.4k  51.5 tok/s

Cost:                  $5.02
Timing coverage: processing 54 of 59 calls.
Chat calls only; compaction and title calls are excluded.
```

- **Prompt processing** divides uncached input by time spent processing the prompt, so cached tokens don't inflate the number. **Token generation** divides output tokens by generation time.
- **Tabs and ranges** work as in `/usage`. `/stats` opens on **today**, which begins at local midnight; `t` cycles today, 7 days, 30 days and all time.
- **Averages cover only the calls that recorded that timing.** A figure shown as `—` was never measured rather than being slow.
- **Speeds prefixed with `~` are inferred.** Providers speaking the OpenAI protocol return no prompt-eval/eval timing split, so prompt processing is bounded by time to first token and generation by total duration minus time to first token. The footnote says which speeds were inferred. Ollama reports the split directly, so its speeds carry no prefix.
- **The Session tab refreshes itself** every 15 seconds while it is open; press `r` to re-scan the Workspace and Global tabs.

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

---

## Subagents & Live Inspection (`/subagents`)

When an agent delegates work using `invoke_agent`, TaskSmith spawns an asynchronous child session (`@explore`, `@general`, or a custom agent) and surfaces its real-time status in the TUI without interrupting your main conversation flow:

- **Bottom Activity Bar**: Running subagents and running background tasks share the status block directly below the composer. Each running subagent displays its persona, task title, current activity (first line of its active thought, `"Generating"`, or current tool action such as `"Reading files"` / `"Editing files"`), and live elapsed time:
  ```text
  ● Agent(explore: Trace auth token refresh)  Reading files · 14s
  ```
- **Non-Blocking Permission & Question Alerts**: When a background subagent needs permission or asks a question, a warning banner appears above the composer so your typing is never interrupted:
  - Press `Ctrl+Y` to approve the pending permission request (`allow_once`) immediately without leaving the composer.
  - Press `Ctrl+G` to open the waiting subagent's live view inside `<Slot />` to inspect full details, choose a broader permission scope, or answer a questionnaire.
- **Subagents Browser (`/subagents`)**: Run `/subagents` (or `/subagent`) to list active child subagents (or press `a` to toggle completed/idle subagents), inspect their live conversation (`Enter`), or terminate a running subagent (`x`).
- **Live Navigable Subagent Viewer**: Selecting a subagent opens its live streaming chat view inside `<Slot />`, complete with inline permission/question prompts and keyboard scrolling (`↑`/`↓` or `j`/`k` or `Ctrl+P`/`Ctrl+N`, `PgUp`/`PgDn` or `Ctrl+U`/`Ctrl+D` or `Ctrl+B`/`Ctrl+F`, `g`/`G` for top/bottom, `x` to kill, and `Esc` to return).

## Headless Execution & CI

Headless runs use the same daemon, sessions, permissions, and sandbox as the TUI — only the interface changes. `tasksmith exec "prompt"` (or the root shorthand `tasksmith -p "prompt"`) starts the daemon when none is reachable, runs one turn, prints the assistant's reply, and exits with a status code scripts can branch on. The exhaustive flag, frame, and exit-code tables live in the [Headless Execution Reference](../reference/exec.md).

### One-Shot Prompts

```bash
tasksmith -p "Summarize what this project does in two sentences"

# Piped stdin becomes context for the prompt (or the prompt itself when omitted):
git diff | tasksmith -p "Review this diff for bugs and security issues"
```

In scripts, prefer `--output-format json` to get a single machine-readable envelope with the session id, final status, assistant text, usage, and file changes:

```bash
result=$(tasksmith -p --output-format json "Fix the typo in README.md")
status=$(echo "$result" | jq -r .status)   # completed | needs_input | canceled | failed
```

Exit codes let CI distinguish outcomes without parsing output: `0` success, `1` error, `2` usage error, `4` needs input (a permission or question came up and nothing could answer it), `5` canceled. A run that hits `--mode ask` and exits `4` should be rerun with `--input-format jsonl` so the script can answer requests programmatically.

Useful automation flags: `--session <id>` resumes an existing session, `--ephemeral` deletes the session when the run ends, and `--max-turns N` caps how many turns one invocation may take.

### Bidirectional Sessions (`--input-format jsonl`)

For long-lived scripted sessions — chat bridges, IDE backends, CI pipelines that must approve tool calls — run `tasksmith exec --input-format jsonl --output-format jsonl`. Stdin becomes a stream of newline-delimited JSON control frames (`send`, `permission`, `question`, `config`, `cancel`, `close`), and stdout emits a `{"type":"session"}` header frame, every daemon event verbatim, and a terminal `{"type":"result"}` frame. This is the only mode in which a script can answer a permission prompt mid-run:

```bash
tasksmith exec --input-format jsonl --output-format jsonl <<'FRAMES' | jq -r 'select(.type == "chunk") | .payload.chunk.content[]? | select(.type == "text") | .text'
{"type":"send","content":"Refactor the parser and run the tests"}
{"type":"close"}
FRAMES
```

Because events arrive as the same `api.Event` schema the TUI consumes, any process can render progress, watch for `permission_requested`, and reply with a `permission` frame carrying the request id and decision. See the [stdin control frames](../reference/exec.md#3-input-rules) table for payload shapes.

### Slash Commands & Shell Input Without the TUI

Inside headless runs (including mid-session `send` frames), `/model`, `/models`, `/mode`, `/plan`, `/agent`, `/agents`, `/usage`, `/compact`, `/fork`, `/exit`, and `/help` work locally without starting a turn; workspace commands, skills, `!` shell input, and `mcp:<server>:<prompt>` references go through the daemon. An unrecognized bare `/name` fails fast with `unknown slash command` instead of being sent to the model. See [Slash Commands Without the TUI](../reference/exec.md#6-slash-commands-without-the-tui).

### Embedding (Go SDK)

The public `client` package speaks the daemon's HTTP + SSE API directly — `client.ConnectOrStart` connects or spawns a healthy detached daemon, and session/stream calls mirror what the CLI does. Embedders can either drive the client API or run `tasksmith exec --output-format jsonl` as a subprocess and parse frames with `api.NewExecFrameReader`.
