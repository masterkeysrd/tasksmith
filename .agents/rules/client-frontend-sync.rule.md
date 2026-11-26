---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: client-frontend-sync
  displayName: Client-Frontend Sync
  description: Keeps the daemon-vs-client boundary strict and every shared protocol surface handled consistently across the TUI, exec, and ACP clients
  labels:
    category: quality
spec:
  priority: 50
  triggers:
    globs:
      - "api/**"
      - "client/**"
      - "internal/cli/**"
      - "internal/exec/**"
      - "internal/acp/**"
      - "internal/daemon/session/**"
    keywords:
      - event
      - slash command
      - session config
      - permission
      - question
      - ndjson
      - protocol
---

# Client-Frontend Sync

The daemon (`internal/daemon`) is the single source of truth for agent behavior; every
frontend (`internal/cli` TUI, `internal/exec` headless, `internal/acp` editor adapter) is a thin,
interchangeable driver of the same `api/` protocol over `client.Client`. This rule holds that
boundary and keeps protocol changes from landing on one client only.

## 1. Daemon-vs-client boundary

- Backend-owned behavior lives in the daemon: turn execution, workspace `.command.md` and skill
  resolution, `/mcp:<server>:<prompt>` rendering, permission policy, and `ErrUnknownCommand`
  classification for unrecognized bare `/name` prompts. Clients call `client.Send` and react to
  events; they never re-implement turn logic or silently swallow daemon errors.
- Client-owned behavior stays in the client: rendering, pickers, tabs, keybindings, and
  client-local convenience commands. A client-local `/slash` command must be UI-free data or an
  explicit API call (exec's `/model`, `/mode`, `/agent`, `/models`, `/usage`, `/compact`) — never
  a hidden state mutation that the daemon cannot observe.
- Anything a client cannot handle locally is forwarded to the daemon verbatim (exec forwards
  unknown `/name` and `!cmd` lines; the TUI forwards locally-unknown slash lines). Unknown input
  must produce the same daemon outcome (`400` / `unknown_command`) for every client.

## 2. Cross-client protocol invariants

When changing any of the following, update **all** existing frontends in the same change, or
scope the addition explicitly (UI-only) in the change description. For ACP, translate stream events,
mode/model changes, permissions, and daemon commands into their editor equivalents. Document any
unsupported interaction and terminate it explicitly; never leave a turn waiting for invisible input:

- **New `api.Event` types or payload fields**: decode in `Event.UnmarshalJSON` (`api/events.go`),
  handle in the TUI conversation/store layer, handle in `internal/exec` (stream pass-through plus
  any turn-state transition), and surface in docs. NDJSON (`api.WriteEvent`) and SSE must carry
  byte-identical payloads — never fork an "exec-only" event shape.
- **Session config fields (`api.SessionConfig`) and `UpdateSessionRequest`**: honor them in the
  daemon session, the TUI settings flows, and exec's flags/local commands (`/model`, `/mode`,
  `/agent`, `config` control frames) so the same field is settable from every client.
- **Permission/question payloads (`PermissionRequest`, `QuestionRequest`, decisions, responses)**:
  keep the interactive paths (TUI prompts, exec `permission`/`question` control frames) and the
  non-interactive guard (exec `needs_input` exit) consistent; a new decision kind or response
  field must be accepted by both clients and the daemon.
- **Cross-client `/slash` workflows**: backend-owned commands resolve only through
  `internal/daemon/session` (`resolveCommandExecution`); per-client dispatchers must forward
  anything they do not own instead of inventing divergent semantics.
- **Control-frame types (`api.ExecInputType`)**: each new frame type maps 1:1 onto an existing
  `api/` payload and `client.Client` method; framing helpers live in `api/exec.go`, orchestration
  in `internal/exec`, never in `client/` transport code.

## 3. Verification

- A protocol change is complete only when each existing frontend either handles it or explicitly
  opts out in writing (rule note, code comment at the dispatch site, or docs).
- Extend the shared fixtures: daemon-side behavior is covered by `internal/daemon/session` and
  daemon tests; end-to-end client handling by `internal/cmd` exec tests (text/json/jsonl,
  bidirectional frames, guard exit codes) and the TUI store/conversation tests.
