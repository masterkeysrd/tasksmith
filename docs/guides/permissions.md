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

When an agent proposes an action requiring authorization, TaskSmith pauses execution and presents an interactive permission modal in the terminal:

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

## Workspace Declarative Policies (`WORKSPACE.md`)

In addition to interactive rules, team-wide security baselines can be committed directly to version control in `WORKSPACE.md`:

```markdown
---
name: backend-service
policies:
  # Tools that execute automatically without prompting
  allow_tools:
    - view
    - glob
    - grep

  # Tools that always prompt for developer approval
  ask_tools:
    - bash
    - write
    - edit
    - web_fetch

  # Tools that are completely disabled for all agents
  deny_tools:
    - download
---
```

Or using fine-grained glob inclusion and exclusion under `policies.tools`:

```markdown
---
policies:
  tools:
    include:
      - "*"
    exclude:
      - download
      - remove
---
```

When an agent requests a tool call, TaskSmith first verifies that the tool is permitted by workspace policies. Excluded or denied tools cannot be authorized by interactive prompts.

For keyboard shortcuts and command syntax, see the [CLI Reference](../reference/cli.md). For the underlying security implementation, see the [Permissions Architecture](../architecture/permissions.md).
