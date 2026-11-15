# Sandbox & Permissions in the Interactive CLI

Four interactive surfaces control isolation and authorization from inside the REPL. Drive them
rather than editing the underlying JSON with file tools — TaskSmith's own stores are write-denied
to tools, so a file-tool edit fails by design.

## `/sandbox` — three-tab sandbox editor

Backed by the daemon's `/v1/sandbox` API. Project-scope edits persist to
`$XDG_DATA_HOME/tasksmith/workspaces/<hash>/sandbox.json`; Global-scope edits to
`~/.config/tasksmith/sandbox.json`. Saving through `/sandbox` immediately live-refreshes open
sessions' runtime sandbox policies (filesystem paths, network posture and proxy allowlist/denylist,
`allowUnsandboxed`, `autoAllowBash`, and `excludedCommands`) — a hand-edit to `sandbox.json` on
disk instead needs `/workspace reload`. Only the `mode` backend (`native` vs `local`) is fixed per
session at creation, so the currently active session keeps the backend shown on the status banner
and `mode` changes apply to sessions created afterwards.

| Tab | Content |
| --- | --- |
| `1 Policy` | Live status banner (active session backend, host kernel backend health with remediation, workspace trust) above a `Setting / Effective / <Scope> override` table. The `mode` row cycles `native → local → inherit`; `failIfUnavailable`, `allowUnsandboxed`, `autoAllowBash` show effective values plus the scope's own override. Knobs pinned by the *other* scope or the trust floor show a lock and refuse edits; a scope is never locked by its own settings, and Global is never locked. |
| `2 Exclusions (<n>)` | The scope's `excludedCommands` patterns: add (`a`), edit in place (`e`), remove (`d` / `Backspace` / `Delete`) via guided inline input. Project scope also shows a read-only `Inherited from Global (<n>)` section. Warns when effective `allowUnsandboxed` is off (excluded commands still run sandboxed then). |
| `3 Effective` | Read-only scrollable effective merge: posture (mode, fail-if-unavailable, allow-unsandboxed, auto-allow bash, network mode with allowed/denied domains, union of excluded commands) plus canonical filesystem path rules (home-shortened). |

Keybindings:

| Key | Where | Action |
| --- | --- | --- |
| `Tab` / `Shift+Tab` / `←` / `→` / `1` `2` `3` | Editor | Switch tab. |
| `s` | Policy / Exclusions | Switch edited scope (`Project` ↔ `Global`). |
| `↑` / `↓` / `k` / `j` / `Ctrl+P` / `Ctrl+N` | Policy / Exclusions | Move the row cursor. |
| `Space` / `Enter` / `t` | Policy | Cycle default mode (`native → local → inherit`) or toggle the highlighted knob (an unset knob toggles from its effective value). |
| `x` / `Backspace` / `Delete` | Policy | Clear the mode or knob override so the scope inherits again. |
| `a` / `e` / `d` (`Backspace`, `Delete`) | Exclusions | Add / edit / remove a pattern (`Enter` submits, `Esc` cancels input). |
| `↑` / `↓` / `k` / `j` / `PgUp` / `PgDn` / `Space` / `g` / `G` | Effective | Scroll the viewport. |
| `Esc` | Editor | Close and return to the composer. |

The scope selector renders inline in the tab strip (`scope: [Project] · Global`) on the two
editable tabs; an in-flight write shows `· saving…`.

**When to edit `sandbox.json` directly instead**: the editor covers `mode`, the three posture
knobs, and `excludedCommands`. The `filesystem.{allowWrite,denyWrite,denyRead,allowRead}` and
`network.{mode,allowedDomains,deniedDomains}` lists are edited in the JSON file (hand the user the
exact snippet; see the `tasksmith-customizations` skill's `sandbox-and-permissions.md` for the
full schema and merge semantics). After a hand-edit, run `/workspace reload`.

## `--sandbox native|local` startup flag

Selects the isolation backend for sessions this client *creates*, persisted with the session
config. It is resolved per session at creation — one daemon serves native and local sessions side
by side — and cannot change afterwards (resuming a session with a differing request warns instead).
`native` degrades to `local` with a warning when the host probe shows kernel sandboxing is
unusable. Changing `Default mode` in `/sandbox` only affects new sessions for the same reason:
backends are immutable per session, so the active REPL keeps its startup backend.

## `!command` shell mode

Type `!` on an empty composer line to enter bash mode (`! ` prompt, `esc` cancels); submitted
commands run directly through the daemon. In trusted workspaces with `allowUnsandboxed` enabled
they run **unsandboxed** — they are explicit human-initiated host actions. In restricted
workspaces `allowUnsandboxed` is forced off, so `!` commands run inside the kernel sandbox (and
the workspace itself is kernel read-only). The agent's `bypass_sandbox` retries and
`excludedCommands` exclusions share the same `allowUnsandboxed` gate.

## `/permissions` — rule browser (`/perm`, `/perms`)

Two steps: pick a scope (`Project` / `Global`), then browse one of two lists (`allowlist` /
`denylist`) of rules like `bash(git *)` or `edit(internal/**)`.

| Key | Where | Action |
| --- | --- | --- |
| `↑` / `↓` | Scope step | Choose `Project` or `Global`. |
| `Enter` | Scope step | Enter the rule lists. |
| `Tab` / `→` / `←` | Rules | Switch between allowlist and denylist. |
| `a` | Rules | Add a rule (inline input; `Enter` submits, `Esc` cancels). |
| `e` | Rules | Edit the highlighted rule in place. |
| `d` / `Backspace` / `Delete` | Rules | Delete the highlighted rule. |
| `Esc` | Rules / Scope | Back one step / close. |

Rules persist to `permissions.json` in the selected scope (Global: `~/.config/tasksmith/`,
Project: `$XDG_DATA_HOME/tasksmith/workspaces/<hash>/`). Deny beats allow; a plain `bash(...)`
deny also blocks unsandboxed variants, while `bash_unsandboxed(...)` covers only bypass retries.
Approving a prompt with a persistent scope mints the same rule shapes, so review after bulk
approvals is normal.
