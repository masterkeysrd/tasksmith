# Headless Execution (`tasksmith exec` / `-p`)

Everything below is implemented in `internal/cmd/exec.go` (flags), `internal/exec`
(runner, writers, slash dispatcher), and `api/exec.go` (NDJSON framing). Verify against
those files before quoting any flag, frame field, or exit code.

## When to use which surface

- One-shot automation: `tasksmith exec "prompt"` or the root shorthand
  `tasksmith -p "prompt"`.
- Scripted bidirectional sessions: `tasksmith exec --input-format jsonl`.
- Programmatic embedding: the public `client` package (Go SDK), which speaks the same
  daemon HTTP + SSE API.

## Flags

`tasksmith exec [prompt...]` accepts:

| Flag | Values | Notes |
| :--- | :--- | :--- |
| `--input-format` | `text` (default), `jsonl` | `jsonl` enables the bidirectional protocol |
| `--output-format` | `text` (default), `json`, `jsonl` | see Output framing below |
| `--agent` | agent name | new or resumed (`--session`) sessions |
| `--model` | `provider/model` or `@alias` | new or resumed (`--session`) sessions |
| `--effort` | effort level or `default` | reasoning effort override |
| `--mode` | `default`, `accept_edits`, `plan`, `ask`, `auto` | permission mode |
| `--sandbox` | `native`, `local` | new sessions only (immutable after creation) |
| `--session` | session id | resume an existing session |
| `--continue` (`-c`) | bool | resume the workspace's most recently updated session; ignored with `--session`, fresh when none exists |
| `--fork` | bool | branch the session resolved by `--session` or `-c` and run on the fork; usage error when no session resolves |
| `--title` | string | new session title (names the fork with `--fork`) |
| `--max-turns` | integer | client-side cap; 0 = unlimited |
| `--ephemeral` | bool | delete the session when the run ends |
| `--force` | bool | force a resumed session's `--model` switch even when it drops active modalities |

The root command's `-p`/`--print` shorthand reuses the REPL's flags plus
`--input-format`, `--output-format`, `--mode`, `--max-turns`, and `--ephemeral`; it does
not repeat every `exec` flag. Both surfaces start the daemon when none is reachable.

## Input rules

- `text` (default): with a prompt argument, piped stdin is prepended as context (`@file`,
  `@rule:`, `@skill:`, and `@mcp:` mentions resolve from the prompt argument only, never
  from piped stdin); with no argument, piped stdin becomes the prompt. An interactive TTY
  with no prompt is a usage error (exit `2`).
- `jsonl` (bidirectional): stdin is a stream of newline-delimited control frames and the
  session stays open until a `close` frame or EOF. A prompt argument is optional (it is
  submitted as the first turn). Frames accept an optional `session_id`; for `permission`
  and `question`, `exec` auto-routes to the owning parent or child subagent by
  `request_id` / `tool_call_id` when `session_id` is omitted.

Control frames (one JSON object per line, schema `api.ExecInputFrame`):

| Type | Payload field | Effect |
| :--- | :--- | :--- |
| `send` | `content` (bare text) or full `send` (`api.SendRequest`) | start a turn; bare content still routes local slash commands and `!` shell input |
| `permission` | `permission` (`api.PermissionDecision`) | answer a pending permission request |
| `question` | `question` (`api.QuestionResponse`) | answer a pending question |
| `config` | `config` (`api.UpdateSessionRequest`) | update the session configuration |
| `cancel` | — | cancel the in-flight turn |
| `close` | — | finish once pending turns complete |

In `text` mode a pending permission or question cannot be answered: the run terminates
with status `needs_input` and exit code `4` (the in-flight turn is canceled in the
background) instead of hanging.

## Output framing

- `text`: assistant reply streamed to stdout; diagnostics on stderr.
- `json`: one `api.ExecResult` envelope (session id, status, assistant text, usage,
  file changes, error).
- `jsonl`: header frame (`{"type":"session"}`, schema `api.ExecHeaderFrame`) first, then
  daemon events verbatim for the session and its child subagents keyed by `session_id`
  (schema `api.Event`, including `session_created`/`session_updated`/`session_deleted`),
  inline command results (`{"type":"command_result"}`, schema `api.ExecCommandResultFrame`
  with human `output` and structured `data`), and one terminal `{"type":"result"}`
  (`api.ExecResult`) frame last.

With `--output-format jsonl`, stdout carries frames only; diagnostics go to stderr.

## Slash commands without the TUI

`exec` dispatches these locally: `/model [--force] [id]`, `/models`, `/mode [mode]`,
`/agent [name]`, `/agents`, `/effort [level|default]`, `/thinking [on|off|adaptive|default]`,
`/budget [tokens|default]`, `/usage`, `/compact [focus]`, `/fork [title]`, `/exit` (`/quit`),
`/help`. `/fork` branches the current session and rebinds the run to the fork (rejected while
the source session is running). Everything else — workspace commands and skills, `!` shell
input, and `mcp:<server>:<prompt>`
references — goes to the daemon. An unrecognized bare `/name` fails with `unknown slash
command` (exit `1`); paths and `mcp:` references are never mistaken for commands.

## Exit codes

| Code | Meaning |
| :--- | :--- |
| `0` | success |
| `1` | generic or daemon error (including unknown slash commands) |
| `2` | usage error (bad flags or input) |
| `4` | needs input (permission/question pending in non-interactive mode) |
| `5` | canceled (signal or `cancel` frame) |

## Embedding (Go SDK)

The `client` package owns daemon connection and lifecycle: `client.ConnectOrStart`
(connect, or spawn a detached daemon and wait until healthy), `client.SpawnDetached`,
`client.WaitForSocketHealthy`, and `client.DefaultSocketPath`. Build a headless runner
around `client.ConnectOrStart` plus the session/stream APIs, or drive `tasksmith exec
--output-format jsonl` as a subprocess and parse frames with
`api.NewExecFrameReader` / `api.WriteNDJSON`.
