# Sandbox & Permissions Configuration

Two independent user-config layers govern what a session may do, plus one repository layer that can only tighten them. Configure the right one; never substitute one for the other.

| Layer | File | Decides | Edit surface |
| --- | --- | --- | --- |
| OS isolation | `sandbox.json` | What the OS kernel sandbox permits: backend, writable/readable paths, network egress, unsandboxed escape hatch. | Client sandbox settings UI, daemon `/v1/sandbox` API, or direct JSON edit |
| Tool authorization | `permissions.json` | Whether a tool call requires approval: default mode, disabled tools/MCPs, and allow/deny rule lists. | Client permissions UI, daemon `/v1/permissions` API, or direct JSON edit |
| Repository policy ceiling | `WORKSPACE.md` `spec.policies.tools` (and agent policies in `.agents/`) | Monotonic `allow < ask < deny` ceiling committed to git. | Manifest edit; may only narrow the layers above, never widen them |

A tool call runs only if it passes all three: policy ceiling → permission rules/modes → kernel sandbox.

## Storage locations

| Scope | Portable path | Notes |
| --- | --- | --- |
| Global sandbox | `~/.config/tasksmith/sandbox.json` (`$XDG_CONFIG_HOME/tasksmith/sandbox.json`) | User-wide defaults, personal `excludedCommands`, workstation locks. |
| Project sandbox | `~/.local/share/tasksmith/workspaces/<hash>/sandbox.json` (`$XDG_DATA_HOME/tasksmith/workspaces/<hash>/sandbox.json`) | Workspace-specific overrides managed via the daemon API; **never committed to git**. |
| Global permissions | `~/.config/tasksmith/permissions.json` (`$XDG_CONFIG_HOME/tasksmith/permissions.json`) | Machine-wide default mode and allow/deny rules. |
| Project permissions | `~/.local/share/tasksmith/workspaces/<hash>/permissions.json` (`$XDG_DATA_HOME/tasksmith/workspaces/<hash>/permissions.json`) | Workspace-scoped default mode and allow/deny rules; **never committed to git**. |

`sandbox.json` is written atomically via temp-file rename at `0600`; `permissions.json` is written at `0644`.

## Self-protection invariant (mandatory guidance)

`$XDG_CONFIG_HOME/tasksmith` and `$XDG_DATA_HOME/tasksmith` are kernel write-denied (`DeniedWritePaths`) inside `native` sandboxes **and** classified `SafetyUnsafe` by the path guard for every non-read tool action. This means:

- Never attempt to write, patch, or script edits to `sandbox.json` or `permissions.json` — the write will be denied, and attempting it looks like privilege escalation to the user.
- Instead, guide the user to their client's sandbox/permissions settings UI (when using the terminal CLI, load the `cli-customizations` skill for interactive views and shortcuts), or supply the exact JSON snippet for the fields they want changed so the user edits the file on disk themselves.
- Saving through the daemon's sandbox settings API live-refreshes open sessions' runtime sandbox policies automatically; after a manual `sandbox.json` edit on disk, ask the user to trigger a workspace reload in their client so open sessions pick up the updated posture on the next command (only the `mode` backend itself stays fixed per session until a new session is created).

## `sandbox.json` schema

Every key is optional; an empty document means "all defaults".

| Key | Type | Default | Semantics |
| --- | --- | --- | --- |
| `schema` | int | `1` | Document version. |
| `mode` | `"native"` \| `"local"` | platform default (native when the kernel backend probes usable) | Backend for sessions created afterwards. |
| `failIfUnavailable` | bool (nullable) | `false` | When `true`, a native request on a host without kernel sandboxing fails session creation instead of silently degrading to `local`. |
| `allowUnsandboxed` | bool (nullable) | `true` (forced `false` in restricted workspaces) | Escape hatch for `bypass_sandbox`, `excludedCommands`, and human-initiated direct shell execution. |
| `autoAllowBash` | bool (nullable) | `false` | Auto-approves safe, sandboxed bash plans in `default`/`accept_edits` modes (native backend only). |
| `excludedCommands` | string[] | `[]` | AST-matched command patterns exempt from the kernel wrapper. |
| `filesystem.allowWrite` | string[] | `[]` | Extra writable directories outside the built-in set. |
| `filesystem.denyWrite` | string[] | `[]` | Additional read-only paths. |
| `filesystem.denyRead` | string[] | `[]` | Read masks (hidden even from listing). |
| `filesystem.allowRead` | string[] | `[]` | Narrower re-opens inside a `denyRead` mask. |
| `network.mode` | `"open"` \| `"allowlist"` \| `"disabled"` | `open` (`allowlist` implied when `allowedDomains` is non-empty and mode unset; explicit `open` always wins) | Subprocess network posture. |
| `network.allowedDomains` | string[] | `[]` | Exact hosts, `*.domain` wildcards, or IP literals (`::1`, `[::1]`). Under `allowlist` in native mode, child traffic routes through a daemon-side proxy; unlisted hosts prompt when a client subscriber is attached and are refused immediately in headless/unattached sessions. |
| `network.deniedDomains` | string[] | `[]` | Always refused, winning over allow rules and approvals; enforced in every mode. |

Path values: `~` and `~/...` expand to the home directory; project-relative paths resolve against the workspace root; plain relative paths are rejected in global scope.

### Merge rules (project over global)

- **Nullable booleans** (`failIfUnavailable`, `allowUnsandboxed`, `autoAllowBash`): an omitted key in project scope inherits global; a present key overrides — except the monotonic locks below.
- **Monotonic locks**: `failIfUnavailable` is OR-merged (either scope enabling it can never be relaxed by the other). `allowUnsandboxed` is AND-merged (either scope setting `false` locks the hatch off), and a restricted (untrusted) workspace forces it `false` regardless.
- **Arrays** (`excludedCommands`, `filesystem.*`, `network.*` domains): deduplicated unions. `excludedCommands` is additionally cleared entirely when the `allowUnsandboxed` hatch is locked off.
- **Path overlap precedence**: writes resolve as built-in protected floor > user `denyWrite` > user `allowWrite`. Reads resolve narrowest-canonical-prefix-wins between `denyRead` and `allowRead`, with ties going to `denyRead`. TaskSmith's own secret stores (`$XDG_CONFIG_HOME/tasksmith/.env`, `$XDG_DATA_HOME/tasksmith/credentials`, `$XDG_DATA_HOME/tasksmith/mcp`) can never be re-opened through `allowRead`.
- **Built-in floors** (workspace `.agents`, `.git/hooks`, `.git/config`, shell dotfiles, TaskSmith's XDG stores, host credential dirs like `~/.ssh`) are always write/read denied and cannot be removed by any scope.

Example — allow a build cache, hide secrets, and pin a CI box to an API allowlist:

```json
{
  "schema": 1,
  "mode": "native",
  "failIfUnavailable": true,
  "filesystem": {
    "allowWrite": ["~/build-cache"],
    "denyRead": ["~/Documents/private"],
    "allowRead": ["~/Documents/private/public-notes"]
  },
  "network": {
    "mode": "allowlist",
    "allowedDomains": ["*.pypi.org", "registry.npmjs.org"],
    "deniedDomains": ["metadata.example.internal"]
  }
}
```

## `permissions.json` schema

Every key is optional; an empty document leaves the built-in `default` permission mode with empty rule lists.

| Key | Type | Default | Semantics |
| --- | --- | --- | --- |
| `schema` | int | `1` | Document version. |
| `defaultMode` | `"default"` \| `"accept_edits"` \| `"plan"` \| `"ask"` \| `"auto"` | `"default"` | Starting permission mode for new sessions in this scope. |
| `allow` | string[] | `[]` | `tool(pattern)` rules that pre-authorize matching tool plans. |
| `deny` | string[] | `[]` | `tool(pattern)` rules that reject matching tool plans outright. |

Tool and MCP availability is not configured here: it is deterministic from WARP manifests (`spec.enabled`) and `WORKSPACE.md` policies (`spec.policies.tools.exclude`, `spec.policies.mcps.exclude`). A legacy document carrying `disabledTools`/`disabledMcps` keys still decodes; the keys are ignored.

### Merge rules & `tool(pattern)` syntax

- **Scope merge**: `project.defaultMode` overrides `global.defaultMode` when non-empty; `allow` and `deny` merge as deduplicated unions across `global` and `project`. `deny` always beats `allow`.
- **Rule format**: every entry in `allow` and `deny` must parse as `tool(pattern)` with non-empty `tool` and `pattern` (no nested parentheses). Wildcard `*(*)` matches every tool and target.
- **Common pattern shapes**:
  - Commands: `bash(git *)`, `bash(go test *)` (prefix `*` matches commands starting with that prefix).
  - Unsandboxed bypass namespace: `bash_unsandboxed(git commit *)` — `allow` rules in `bash(...)` never auto-approve `bypass_sandbox` retries, and `bash_unsandboxed(...)` never widens sandboxed `bash` approvals. For `deny`, `bash(git *)` blocks both sandboxed and unsandboxed variants, while `bash_unsandboxed(git *)` blocks only unsandboxed execution (`bypass_sandbox` or `excludedCommands`).
  - Filesystem paths: `edit(internal/**)`, `write(docs/**)` (`/**` matches a directory subtree).
  - Network targets: `net(api.example.com)`, `net(*.pypi.org)`, `fetch(https://docs.example.com/*)`.

```json
{
  "schema": 1,
  "defaultMode": "accept_edits",
  "allow": ["bash(go test *)", "bash_unsandboxed(git commit *)", "net(api.example.com)"],
  "deny": ["bash(rm -rf *)", "edit(.env*)"]
}
```

## Escape hatches: `excludedCommands` vs `bypass_sandbox` vs rules

Three controlled routes run a command without the kernel wrapper. All require `allowUnsandboxed` enabled (never available in restricted workspaces or read-only sessions).

| Route | Where configured | Rule namespace | Prompt behavior |
| --- | --- | --- | --- |
| `excludedCommands` | `sandbox.json` (both scopes) | ordinary `bash(...)` rules still apply | No extra prompt; pre-exempted by pattern. |
| `bypass_sandbox: true` retry | agent-requested per command | isolated `bash_unsandboxed(...)` | Prompts in **every** mode, including `auto`, unless matched by an explicit `bash_unsandboxed(...)` allow rule. |
| Human-initiated direct shell execution | client shell action | *(n/a — human-initiated)* | Runs unsandboxed in trusted workspaces when `allowUnsandboxed` is on; sandboxed otherwise. |

`excludedCommands` matching is strict shell-AST analysis: only simple commands qualify. Any redirection, subshell, `$(...)` command substitution, variable-expanded argument, environment prefix, `cd` outside the workspace, `eval`, or background operator disqualifies the command, which then stays sandboxed. Prefer a narrow pattern (`"git commit *"`) over broad (`"*"`); a plain `bash(git *)` **deny** rule blocks both sandboxed and unsandboxed variants, while `bash_unsandboxed(git *)` denies only the unsandboxed variant.

### `autoAllowBash` × permission mode matrix

| Mode | Safe sandboxed bash with `autoAllowBash: true` |
| --- | --- |
| `plan` | Denied unless read-only (mode rule wins). |
| `ask` | Still prompts (mode rule wins). |
| `default` / `accept_edits` | Auto-approved (native backend only; unsafe, outside, unsandboxed, denied, or rule-asked plans still prompt). |
| `auto` | Already auto-approved by the mode itself. |

Network plans are never widened by `autoAllowBash`. Under a network allowlist, no permission mode — including `auto` — silently opens an unlisted host: only `sandbox.json` `network.allowedDomains` or an explicit `net(host)` allow rule permits it without prompting (persistent project/global host approvals append to `sandbox.json` `network.allowedDomains`, while one-shot and session approvals record in-memory `net(host)` rules; unattached sessions with no active client subscriber refuse unlisted hosts immediately).

See `workspace-and-context.md` for the `WORKSPACE.md` `spec.policies.tools` ceiling, and load the `cli-customizations` skill (`references/sandbox-and-permissions.md`) when guiding a terminal CLI user through interactive views or approval prompts.
