# Headless Execution Reference (`exec` / `-p`)

`tasksmith exec` runs TaskSmith without a terminal UI: one prompt in, one answer out, with exit codes and machine-readable output a script or CI job can branch on. This page documents every flag, the NDJSON wire protocol for bidirectional sessions, the daemon events you'll receive, and the exit-code contract. For workflow-oriented walkthroughs, see the [Headless Execution & CI guide](../guides/cli.md#headless-execution--ci).

Headless runs are **not** a second implementation. They drive the same daemon, sessions, permission engine, and sandbox as the REPL — `exec` is just another client. Anything the REPL can do to a session (send prompts, answer permission prompts, switch models mid-session), `exec` can do too.

---

## 1. Two Ways to Invoke

| Invocation | Form | Best for |
| :--- | :--- | :--- |
| **`exec` subcommand** | `tasksmith exec [flags] [prompt...]` | Full flag set; explicit headless automation. |
| **Root shorthand** | `tasksmith -p [flags] [prompt...]` (`--print`) | Quick one-liners. Reuses the REPL's session flags plus the headless flags below. |

Both start the Brain daemon automatically when none is reachable, so the same command works over SSH or inside Docker (`ssh host tasksmith -p "..."`, `docker exec -i ctr tasksmith exec ...`).

```bash
tasksmith -p "Summarize what this project does"
git diff | tasksmith -p "Review this diff for bugs"
```

---

## 2. Flags

### Headless Flags (`exec` and `-p`)

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--print` | `-p` | `off` | Root flag only: run the positional prompt headlessly instead of starting the REPL. |
| `--input-format <format>` | | `text` | stdin handling: `text` or `jsonl` (bidirectional control frames). See [§3](#3-input-rules). |
| `--output-format <format>` | | `text` | stdout framing: `text`, `json`, or `jsonl`. See [§4](#4-output-framing). |
| `--effort <level>` | | *(model default)* | Reasoning effort level (e.g. `low`, `medium`, `high`, or `default`). |
| `--mode <mode>` | | `default` | Permission mode: `default`, `accept_edits`, `plan`, `ask`, `auto`. |
| `--max-turns <n>` | | `0` | Stop after this many completed turns (`0` = unlimited). Enforced client-side; most useful in bidirectional runs. |
| `--ephemeral` | | `off` | Delete the session once the run settles. Ideal for throwaway CI runs. |
| `--force` | | `off` | Force a resumed session's `--model` switch even if it drops input modalities active in live context. |

### Shared Session Flags

`exec` also accepts the REPL session flags with identical meaning:

| Flag | Description |
| :--- | :--- |
| `--session <id>` | Resume an existing session (UUIDv7) instead of creating one. With a config flag (`--model`, `--mode`, `--agent`, `--effort`), overlays it onto the resumed session. |
| `--continue` (`-c`) | Resume the workspace's most recently updated top-level session. Ignored when `--session` is given; creates a fresh session when the workspace has none. |
| `--fork` | Branch the resolved session (`--session` or `-c`) and run on the fork instead of mutating the source. `--title` names the fork; a usage error when no session resolves. |
| `--agent <name>` | Agent persona for the session (new or resumed). |
| `--model <id>` | Model or `@alias` for the session (e.g. `openai/gpt-5.5`, `@fast`). |
| `--title <title>` | Human-readable title for a new session. |
| `--sandbox <mode>` | Isolation backend for a new session: `native` or `local`. Fixed at creation. |

---

## 3. Input Rules

### `text` (default)

| Situation | Behavior |
| :--- | :--- |
| Prompt argument + piped stdin | Piped content is **prepended as context** to the prompt (`git diff \| tasksmith -p "review"`). `@file`, `@rule:`, `@skill:`, and `@mcp:` mentions are resolved only from the user prompt argument, never from piped stdin. |
| No argument + piped stdin | Piped content **becomes the prompt** (sent as raw text without `@`-mention scanning). |
| No argument + interactive TTY | Usage error (exit `2`) — headless mode never blocks waiting on a terminal. |

### `jsonl` (bidirectional)

stdin is a stream of newline-delimited JSON **control frames** (`api.ExecInputFrame`, one object per line). The run stays open across turns until a `close` frame or stdin EOF. A prompt argument is optional; when present it's submitted as the first turn. Every control frame also accepts an optional `"session_id"` field when explicitly targeting a child subagent; for `permission` and `question` replies, `exec` automatically routes to the owning parent or subagent by `request_id` / `tool_call_id` when `"session_id"` is omitted.

| Frame | Payload field | Maps to | Effect |
| :--- | :--- | :--- | :--- |
| `{"type":"send","content":"..."}` | `content` (bare text) | local dispatch → `client.Send` | Start a turn. Bare content still routes local `/slash` commands and `!` shell input exactly like typing in the REPL. |
| `{"type":"send","send":{...}}` | `send` | `api.SendRequest` | Full send: `content`, `attachments`, `command`, `compact`, etc. |
| `{"type":"permission","permission":{...}}` | `permission` | `api.PermissionDecision` | Answer a live `permission_requested` prompt. |
| `{"type":"question","question":{...}}` | `question` | `api.QuestionResponse` | Answer a live `question_requested` prompt. |
| `{"type":"config","config":{...}}` | `config` | `api.UpdateSessionRequest` | Change `mode`, `model`, `agent`, `title`, or `thinking` mid-session. |
| `{"type":"cancel"}` | — | `client.Cancel` | Abort the in-flight turn; the session stays open for more frames. |
| `{"type":"close"}` | — | — | Drain pending turns, print the terminal result, exit. |

Payload field names (all snake_case, from the canonical `api/` structs):

- **`send`** (`api.SendRequest`): `id`, `content`, `blocks`, `command`, `decision`, `question`, `compact`, `attachments`.
- **`permission`** (`api.PermissionDecision`): `request_id` and `tool_call_id` (copy both from the request you're answering), `kind` (`allow_once` / `allow_session` / `reject`), optional `grant_pattern`, `scope` (`session` / `project` / `global`), `feedback`.
- **`question`** (`api.QuestionResponse`): `request_id`, `tool_call_id`, optional `canceled`, and `answers[]` — each `{question, confirmed?, selected?[], text?}`.
- **`config`** (`api.UpdateSessionRequest`): `title` plus a `config` object (`agent`, `model`, `mode`, `thinking`).

Blank lines between frames are ignored; a single frame may not exceed 8 MiB.

### Non-Interactive Guard

In `text` input mode nothing can answer a permission or question prompt. Rather than hang, the run terminates immediately: final status `needs_input`, exit `4`, and the in-flight turn is canceled in the background. If your automation may touch tools under `--mode ask`, use `--input-format jsonl`.

---

## 4. Output Framing

### `text` (default)

Assistant reply (or `!command` shell output) streamed to stdout. Diagnostics and daemon notices go to stderr, so `tasksmith -p "..." > out.txt` captures clean output.

### `json`

Exactly one `api.ExecResult` envelope on stdout:

```json
{
  "session_id": "0193...",
  "status": "completed",
  "text": "Final assistant reply…",
  "usage": {
    "session_id": "0193...",
    "totals": { "calls": 3, "tokens": { "input": 8120, "output": 412 }, "cost_nano": 12400 },
    "changes": { "lines_added": 14, "lines_removed": 6, "files": 1 }
  },
  "changes": [ { "path": "src/parser.go", "kind": "modified", "additions": 14, "deletions": 6 } ],
  "error": ""
}
```

| Field | Notes |
| :--- | :--- |
| `type` | Absent in `--output-format json`; carries `"result"` when the same envelope terminates a `jsonl` stream. |
| `status` | `completed` · `failed` · `canceled` · `needs_input`. |
| `text` | Merged assistant text of the run's turns. |
| `usage` | Full `api.SessionUsage`: `totals` (calls, `tokens.input/output/...`, `cost_nano` in nano-USD) plus per-model/tier/tool breakdowns. Omitted when unavailable. |
| `changes` | File mutations tracked for the session (`path`, `kind` = `created`/`modified`/`deleted`, `additions`, `deletions`, ...). |
| `error` | Present only on failure. |

### `jsonl` (stream)

One JSON object per line, in order:

1. **Header** — `{"type":"session", ...}` with `session_id`, `workspace`, `model`, `config`, `sandbox_mode`, `daemon_version`. Published first so a caller that never created the session (a piped one-shot) still learns its id.
2. **Daemon events** — verbatim `api.Event` envelopes for the root session **and** any child subagents it spawns, byte-identical to the SSE stream and keyed by `session_id` (plus `session_created`, `session_updated`, and `session_deleted` lifecycle events for the session tree). See [§5](#5-daemon-events-youll-receive).
3. **Command results** — `{"type":"command_result","command":"/models","ok":true,"output":"...","data":[...]}` for client-local `/slash` commands, carrying both human `output` text and typed `data` payloads (`[]api.Model`, `[]api.Agent`, `api.SessionUsage`, `*api.ThinkingSetting`). Daemon turn events never use this type, so harness output and client output are always distinguishable.
4. **Terminal result** — one `api.ExecResult` tagged `{"type":"result", ...}` (same fields as `--output-format json`) as the very last line.

stdout carries frames **only** — never interleave your own writes with `--output-format jsonl`; diagnostics go to stderr.

---

## 5. Daemon Events You'll Receive

Event frames share the SSE envelope: `{"id":..., "type":..., "topic":..., "payload":{...}}`. Events most exec consumers handle:

| `type` | Payload | When it fires |
| :--- | :--- | :--- |
| `chunk` | `chunk` (streaming delta: `id`, `content[]` blocks, `done`, cumulative `usage`) | Assistant text/reasoning/tool-call tokens stream in. |
| `entry_added` | `entry` (persisted conversation entry) | A message or tool result was persisted; its text supersedes streamed chunks. |
| `status_changed` | session status (`thinking`, `executing`, `waiting_permission`, `idle`, ...) | Turn progress; `idle` also confirms a cancel finished. |
| `permission_requested` | `permissions[]` (`id`, `tool_call_id`, `tool_name`, `summary`, `diff`, `grant_patterns`, `unsandboxed`) | Tool call awaiting a decision — answer with a `permission` frame. |
| `question_requested` | `questions[]` | `ask_question` awaiting answers — reply with a `question` frame. |
| `usage_updated` | session usage totals | After each LLM call. |
| `quota_updated` | provider quota windows | Subscription-billed providers. |
| `completed` | `session_id` | A turn (or a batch of queued turns) finished. |
| `error` | `error` message | Turn failure (e.g. `unknown slash command`); the run exits non-zero. |

Other REPL-oriented events (`task_updated`, `todos_updated`, `queue_updated`, `compaction_started`, `context_updated`, ...) also pass through; ignore what you don't render.

---

## 6. Slash Commands Without the TUI

`exec` resolves a small UI-free set **locally** (no daemon turn is started) and forwards everything else to the daemon — which owns workspace commands, skills, and `/mcp:<server>:<prompt>` exactly as in the REPL:

| Command | Arguments | Description |
| :--- | :--- | :--- |
| `/model` | `[--force] [model_id]` | Shows the active model, or switches (id or alias; unknown ids rejected without a turn). Fails on `capability_mismatch` unless `--force` is passed when switching drops active-context modalities. |
| `/models` | — | Lists chat-capable models, marking the active one. |
| `/mode` | `[mode]` | Shows or sets the permission mode. |
| `/agent` | `[name]` | Shows or sets the session agent. |
| `/agents` | — | Lists workspace agents (`data` carries `[]api.Agent`). |
| `/effort` | `[level\|default]` | Shows or sets reasoning effort on the active model. |
| `/thinking` | `[on\|off\|adaptive\|default]` | Shows or sets the model thinking strategy. |
| `/budget` | `[tokens\|default]` | Shows or sets the thinking token budget. |
| `/usage` | — | Prints session usage (`data` carries `api.SessionUsage`). |
| `/compact` | `[focus]` | Triggers daemon-side compaction as a turn. |
| `/fork` | `[title]` | Branches the current session and rebinds the run to the fork (`data` carries the forked `api.Session`). In `jsonl` mode the event stream follows the fork from that point on; a running source session is rejected. |
| `/login` | `/auth`, `/signin` · `[provider]` | Fails with a pointer to `tasksmith auth login [provider] [--paste]`: provider sign-in waits on a browser or a pasted redirect URL, which a headless run cannot supply. |
| `/help` | — | Lists local commands and the forwarding rule. |
| `/exit` | `/quit` | Ends the run (pending turns drain first in bidirectional mode). |

Results print as plain text in `text` output and as `command_result` frames in `jsonl`. `!command` shell input works in both modes. An unrecognized bare `/name` fails fast with `unknown slash command` (exit `1`) instead of being sent to the model; filesystem paths (`/etc/hosts`) and `mcp:` references are never mistaken for commands.

---

## 7. Exit Codes

| Code | Meaning | Typical cause |
| :--- | :--- | :--- |
| `0` | Success. | Turn(s) completed. |
| `1` | Generic failure. | Connection, session, or turn error (including unknown slash commands). |
| `2` | Usage error. | Invalid flags, or no prompt in text-input mode from a TTY. |
| `4` | Needs input. | A permission/question came up and nothing could answer it — rerun with `--input-format jsonl`. |
| `5` | Canceled. | `{"type":"cancel"}` frame or Ctrl-C. |

CI pattern:

```bash
tasksmith -p --output-format json "Update the changelog for HEAD"
case $? in
  0) echo done ;;
  4) echo "rerun needs --input-format jsonl to approve tools" ;;
  *) echo "failed"; exit 1 ;;
esac
```

---

## 8. Recipes

**Pipe a diff through review** (stdin becomes context):

```bash
git diff main | tasksmith -p "Review this diff for bugs and security issues"
```

**Machine-readable one-shot** with status branching:

```bash
result=$(tasksmith -p --output-format json "Fix the typo in README.md")
jq -r '.status, .text' <<<"$result"
```

**Stream tokens as they generate** (`chunk` events, text blocks only):

```bash
tasksmith exec --output-format jsonl "Explain the build system" \
  | jq -rj 'select(.type=="chunk") | .payload.chunk.content[]? | select(.type=="text") | .text'
```

**Bidirectional session answering a permission prompt** — send a frame per line; the loop watches for `permission_requested`, approves it, and closes when the turn completes:

```bash
tasksmith exec --input-format jsonl --output-format jsonl > stream.ndjson <<'FRAMES'
{"type":"send","content":"Bump the version in package.json to 1.2.0"}
FRAMES
```

A driver script reads `stream.ndjson` events and writes `{"type":"permission","permission":{"request_id":"...","tool_call_id":"...","kind":"allow_once"}}` back to the process's stdin to approve, then `{"type":"close"}` to finish.

**Throwaway CI session** (nothing persisted after the run):

```bash
tasksmith -p --ephemeral --mode accept_edits "Add unit tests for internal/exec"
```

**Resume a session from a previous run** (the id came from the header or result envelope):

```bash
tasksmith exec --session 0193abcd-... "Now apply the review feedback from before"
```

**Branch a session before continuing** (the source stays untouched; `--title` optionally names the fork):

```bash
tasksmith exec --session 0193abcd-... --fork "Try the alternative approach"
```

---

## 9. Embedding (Go SDK)

The public `client` package (`github.com/masterkeysrd/tasksmith/client`) is the Go SDK surface — the same one `exec` itself uses:

- `client.ConnectOrStart(...)` — connect to the daemon on `brain.sock`, or spawn a detached healthy one and wait. `client.DefaultSocketPath()`, `client.SpawnDetached`, `client.WaitForSocketHealthy` cover custom setups.
- Session and stream calls (`CreateSession`, `Send`, `Cancel`, `Watch`, `ForkSession`, ...) mirror what the CLI does over HTTP + SSE.

Two integration styles:

1. **In-process**: import `client` and drive the daemon API directly.
2. **Subprocess**: run `tasksmith exec --output-format jsonl` and parse frames with `api.NewExecFrameReader` (`NextEvent`, `NextInputFrame`) / `api.WriteNDJSON` — the identical framing documented above, which is also the contract for thin TypeScript/Python subprocess wrappers.

---

## See Also

- [CLI Command Reference](./cli.md) — REPL flags, slash commands, keybindings, daemon subcommands.
- [Headless Execution & CI guide](../guides/cli.md#headless-execution--ci) — workflow walkthroughs.
- [Daemon Architecture](../architecture/daemon.md) — what runs behind `brain.sock`.
