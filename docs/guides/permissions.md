# Permission & Security Policies

TaskSmith employs a multi-tiered security model that balances agent autonomy with developer oversight. You have full control over when tools run automatically, when TaskSmith must ask for your explicit approval, and which actions are strictly prohibited.

---

## Permission Modes

TaskSmith supports five session-wide permission modes. Each mode adjusts the approval threshold according to your workflow:

| Mode | Identifier | Tool Behavior | Recommended For |
| :--- | :--- | :--- | :--- |
| **Default** | `default` | Read-only inspection tools (`view`, `grep`, `glob`, `ls`) run automatically. Mutating operations (`write`, `edit`, `bash`, `web_fetch`) trigger an interactive approval prompt unless matched by an explicit allow rule. | Everyday pair-programming and general development. |
| **Accept Edits** | `accept_edits` | File write and edit operations (`write`, `edit`) execute automatically without prompting. Shell commands (`bash`) and network requests (`web_fetch`) still require explicit approval. | Fast iterative code refactoring where you want the agent to write files freely but audit shell executions. |
| **Plan** | `plan` | Strict read-only mode. All mutating tools (`write`, `edit`, `bash`) are automatically blocked. Only safe exploration tools are permitted. | Architectural reviews, code investigation, and drafting implementation plans safely without modifying code. |
| **Ask** | `ask` | Asks for confirmation before executing almost every tool call, except for actions matched by explicit allow rules. | High-security repositories, production environments, or when auditing an unfamiliar agent or tool. |
| **Auto** | `auto` | Autonomous mode. Automatically executes all tools without prompting, unless an action matches an explicit `deny` rule or workspace policy exclusion. | Automated CI/CD runs, headless batch scripts, or trusted local sandbox workflows. |

> [!NOTE]
> Even in `auto` mode, explicit `deny` rules and workspace policy exclusions are strictly enforced. To learn how Abstract Syntax Tree (AST) command analysis and filesystem path confinement prevent escapes under the hood, see the [Permissions Architecture](../architecture/permissions.md).

---

## Switching Modes in the CLI

You can change permission modes at any time without restarting your session:

1. **Cycle with Shortcut**: Press `Shift+Tab` in the composer to cycle through permission modes (`default` $\to$ `accept_edits` $\to$ `plan` $\to$ `ask` $\to$ `auto`).
2. **Slash Command**: Use `/mode` in the composer:
   ```text
   /mode accept_edits
   ```
   Omitting the argument (`/mode`) opens an interactive mode picker with descriptions.

---

## Interactive Tool Approvals

When an agent proposes an action requiring authorization, TaskSmith pauses execution and presents an interactive permission modal in the terminal. This includes `ask_question` in `ask` mode unless an allow rule matches; after approval, the question form appears. A deny rule or rejection prevents the question from appearing.

For example:

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

### Authorization Details
- **Tool & Target**: The modal displays the tool name (`bash`, `write`, `edit`, `web_fetch`) and its specific command, file path, or URL.
- **Unified Diff Preview**: If an agent requests to `edit` or `write` a file, TaskSmith displays an inline, syntax-highlighted unified diff showing exactly what lines will be added and removed before you approve.

### Approval Choices

| Option | Shortcut | Behavior |
| :--- | :--- | :--- |
| **Yes, allow once** | `y` or `Enter` on row 0 | Grants permission exclusively for this specific tool call. Future invocations of the same command or file will prompt again. |
| **Always allow `<pattern>`** | `a` or `Enter` on pattern rows | Automatically approves this action and any future matching action according to the selected persistence scope. |
| **No, deny** | `n` or `Enter` on last row | Rejects the tool call. The agent receives a permission-denied error and can attempt an alternative approach. |
| **Reject with feedback** | `r` | Opens an inline text input to provide explanatory feedback to the agent (e.g., *"Do not delete this test; update it to match the new return type"*). |

### Scoping Persistent Rules (`Tab`)

When selecting an **Always allow** pattern, press `Tab` to cycle the persistence scope:
- **`[session]`**: Stored in-memory for the duration of the current CLI session. Rules reset when you exit.
- **`[project]`**: Saved to the local workspace state directory (`~/.local/share/tasksmith/workspaces/<hash>/permissions.json`). Persists across sessions for this project only.
- **`[global]`**: Saved to your user-wide configuration (`~/.config/tasksmith/permissions.json`). Applies across every project on your machine.

---

## Rule Syntax & Practical Examples

Permission rules follow the normalized format:

```text
<tool-name>(<pattern>)
```

### Pattern Matching Rules
- `*`: Matches any character sequence within a token or path.
- `/**`: In filesystem paths, matches arbitrary recursive subdirectories.
- Shell commands: Matches the binary and command arguments prefix-wise (e.g., `bash(go test *)`).
- Network URLs: Matches hostnames or complete endpoints (e.g., `web_fetch(github.com)`).

### Practical Rule Examples

```text
# Shell Execution Rules
bash(git status *)               # Allow git status commands
bash(git diff *)                 # Allow git diff commands
bash(go test *)                  # Allow Go test executions
bash(npm test *)                 # Allow npm test commands
bash(cargo check *)              # Allow cargo check

# Filesystem Access Rules
write(src/**)                    # Allow writing anywhere under the src/ directory
edit(internal/server/*.go)       # Allow editing Go files in internal/server/
write(*.md)                      # Allow writing root Markdown documentation files

# Network Access Rules
web_fetch(api.github.com)        # Allow fetching GitHub API endpoints
web_fetch(docs.rs)               # Allow fetching Rust documentation

# Catch-All Rules
*(src/**)                        # Allow any tool operating inside src/
*(*)                             # Allow all tools and targets
```

---

## Managing Rules with `/permissions`

TaskSmith includes an interactive rule editor accessible via the `/permissions` slash command (aliases: `/perm`, `/perms`):

```text
┌─ Permissions Manager ──────────────────────────────────────────────────┐
│ [Project Scope]                                                        │
│   Allow:                                                               │
│     • bash(go test *)                                                  │
│     • write(internal/**)                                               │
│   Deny:                                                                │
│     • bash(rm -rf *)                                                   │
│                                                                        │
│ [Global Scope]                                                         │
│   Allow:                                                               │
│     • bash(git status *)                                               │
│                                                                        │
│ a Add rule · e Edit rule · d Delete rule · esc Close                   │
└────────────────────────────────────────────────────────────────────────┘
```

- **Add Rule (`a`)**: Define a new rule by selecting the scope (`project` or `global`), effect (`allow` or `deny`), and typing the `tool(pattern)` string.
- **Edit Rule (`e`)**: Update an existing rule pattern.
- **Delete Rule (`d`)**: Remove a rule from your configuration.

---

## Persistent Configuration Schema (`permissions.json`)

TaskSmith stores persistent permission settings in `permissions.json` across two scopes:
- **Global Configuration**: `~/.config/tasksmith/permissions.json` (`$XDG_CONFIG_HOME/tasksmith/permissions.json`)
- **Project Configuration**: `~/.local/share/tasksmith/workspaces/<hash>/permissions.json` (`$XDG_DATA_HOME/tasksmith/workspaces/<hash>/permissions.json`)

### Schema Specification

```json
{
  "schema": 1,
  "defaultMode": "default",
  "disabledTools": ["download"],
  "disabledMcps": ["linear"],
  "allow": [
    "bash(go test *)",
    "write(internal/**)"
  ],
  "deny": [
    "bash(rm -rf *)"
  ]
}
```

### Hierarchy & Merge Semantics

When evaluating effective permissions for a session, TaskSmith merges global and project-level settings using deterministic rules:

| Field | Type | Merge Semantics | Description |
| :--- | :--- | :--- | :--- |
| `schema` | integer | — | Schema format version for format migrations. |
| `defaultMode` | string | **Project overrides Global** | Default session permission mode (`default`, `accept_edits`, `plan`, `ask`, `auto`). Setting `/mode` or pressing `Shift+Tab` overrides both for the current session only. |
| `disabledTools` | array of strings | **Monotonic Union** | Combined list of disabled tool names. Restrictions are monotonic: any tool disabled in either global or project scope is completely excluded from the agent's model context up front. |
| `disabledMcps` | array of strings | **Monotonic Union** | Combined list of disabled MCP server names. Servers disabled in either global or project scope will not be exposed or queried. |
| `allow` | array of strings | **Union** | Normalized `tool(pattern)` rules that auto-approve matching actions. |
| `deny` | array of strings | **Union (Strict Priority)** | Normalized `tool(pattern)` rules that unconditionally reject matching actions. |

> [!NOTE]
> Disabled tools and MCP servers are not merely rejected at runtime; they are stripped from the model's active tool manifests when constructing conversation prompts. The agent never sees disabled tools, eliminating hallucinations and unnecessary permission prompts.

---

## Isolation Settings (`sandbox.json`)

While `permissions.json` decides *whether* a tool call needs your approval, `sandbox.json` decides *what the operating-system isolation itself permits* once a command runs. It lives in the same two scopes:

- **Global Configuration**: `~/.config/tasksmith/sandbox.json` (`$XDG_CONFIG_HOME/tasksmith/sandbox.json`)
- **Project Configuration**: `~/.local/share/tasksmith/workspaces/<hash>/sandbox.json` (`$XDG_DATA_HOME/tasksmith/workspaces/<hash>/sandbox.json`) — never committed to git.

```json
{
  "schema": 1,
  "mode": "native",
  "failIfUnavailable": false,
  "allowUnsandboxed": true,
  "autoAllowBash": false,
  "excludedCommands": ["git commit *"],
  "filesystem": {
    "allowWrite": ["~/.kube"],
    "denyWrite": ["./vendor"],
    "denyRead": ["~/Secrets"],
    "allowRead": ["~/Secrets/pub"]
  },
  "network": {
    "mode": "allowlist",
    "allowedDomains": ["*.example.com"],
    "deniedDomains": []
  }
}
```

| Key | Merge Semantics | Description |
| :--- | :--- | :--- |
| `mode` | **Project overrides Global** | Default isolation backend for new sessions (`native` or `local`). A session flag or explicit request still wins over both. |
| `failIfUnavailable` | **Monotonic lock (OR)** | When any scope enables it, sessions requiring `native` fail loudly on hosts without kernel sandboxing instead of quietly falling back to `local`. |
| `allowUnsandboxed` | **Monotonic lock (AND)** | Gates per-command unsandboxed execution. Any scope setting it `false` locks the hatch off; restricted (untrusted) workspaces always force it `false`. |
| `autoAllowBash` | **Project overrides Global** | Auto-approves safe, sandboxed shell plans in `default`/`accept_edits` modes. |
| `excludedCommands` | **Union** | Command patterns exempt from the kernel sandbox. Only honored in trusted workspaces. |
| `filesystem.*` | **Union with fixed precedence** | Extra writable directories (`allowWrite`), additional read-only paths (`denyWrite`), read masks (`denyRead`), and narrower re-opens inside a mask (`allowRead`). Project-relative paths resolve against the workspace root; `~` expands to your home. Built-in protected paths (agent resources, Git hooks/config, TaskSmith's own stores) can never be made writable, and TaskSmith's secret stores can never be re-opened. |
| `network.*` | **Union** | Stored subprocess network posture (`open`, `allowlist`, or `disabled`) with domain allow/deny lists (exact hosts or `*.domain` wildcards). Kernel-level domain filtering is enforced in a later sandbox rollout; today `native` sandboxes keep loopback networking. |

Both files are written atomically with owner-only (`0600`) permissions, and TaskSmith refuses to let tools or sandboxed commands modify them — a sandboxed process can never loosen its own next invocation. Edits apply to running sessions on the next workspace reload (`/workspace reload`). Repository-committed manifests (`WORKSPACE.md`, `.agents/`) may only narrow these settings, never widen them.

See [Sandbox Architecture](../architecture/sandbox.md) for how the merged posture is enforced by the kernel.

---

## Workspace Declarative Policies (`WORKSPACE.md`)

In addition to interactive rules, team-wide security baselines can be committed directly to version control in `WORKSPACE.md`. Policies are declared under `spec.policies.tools` and are enforced as a monotonic `allow < ask < deny` ceiling that is evaluated before permission modes and allow rules:

```markdown
---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: backend-service
spec:
  policies:
    tools:
      # Tools that execute automatically without prompting
      include:
        - view
        - glob
        - grep
      # Tools that always prompt for developer approval
      ask:
        - bash
        - write
        - edit
        - web_fetch
      # Tools that are completely disabled for all agents
      exclude:
        - download
---
```

The `tools` ceiling also accepts the annotation-driven actions `dangerous` and `openWorld` (each `allow`, `ask`, or `deny`; defaulting to `ask` and `allow` respectively) and a boolean `readOnly` that restricts the workspace to `isReadOnly` tools:

```markdown
---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: backend-service
spec:
  policies:
    tools:
      readOnly: true
      dangerous: deny
      openWorld: ask
---
```

`include`, `exclude`, and `ask` match a tool's short name, `namespace/name`, or fully-qualified `namespace/Kind/name` pattern. When an agent requests a tool call, TaskSmith first verifies that the tool is permitted by the workspace ceiling; excluded or denied tools cannot be authorized by interactive prompts, and an `ask` ceiling forces approval unless a matching user allow rule grants the plan.

An active `Agent` manifest may further narrow this ceiling through its own `spec.policies.tools` (for example, excluding `write` or `edit`), but it can never widen it: the effective policy is the stricter of the workspace and agent ceilings (`allow < ask < deny`).

### Kernel Read-Only Enforcement

Permission gates decide *whether* a tool call may run; the OS kernel sandbox independently decides what a running process can touch. Read-only kernel enforcement is deliberately decoupled from permission modes and operates on two levels:

- **Session level**: When the workspace is restricted (untrusted repository) or the effective workspace/agent `policies.tools.readOnly` policy is true, the session's sandbox enforces the workspace root as read-only at the kernel level — even while the session runs in `default` mode.
- **Per-command**: Whenever an authorized plan is classified `ReadOnly` — in any mode — the individual `bash` or custom-tool process runs under a read-only kernel profile. If `shellguard` or an `isReadOnly` annotation misclassifies a mutating command, the kernel still blocks the write.

Kernel enforcement requires the native sandbox backend (Apple Seatbelt on macOS, Bubblewrap on Linux); sessions fall back to in-process confinement with a startup notice when it is unavailable. See [Sandbox Architecture](../architecture/sandbox.md) for the profile details, including host credential paths (`~/.ssh`, `~/.aws`, `~/.gnupg`, `~/.kube`, `~/.docker`, `~/.netrc`, and TaskSmith's own config/data secrets) shared with `shellguard` and masked from child processes.

For keyboard shortcuts and command syntax, see the [CLI Reference](../reference/cli.md). For the underlying security implementation, see the [Permissions Architecture](../architecture/permissions.md).
