# Permission & Guard Engine

The **Permission & Guard Engine** is the security enforcement subsystem in TaskSmith that controls tool execution, validates shell commands, and prevents unauthorized filesystem or network operations.

---

## 1. Architectural Overview

```mermaid
flowchart TD
    subgraph Invocation ["Tool Invocation"]
        ToolCall["Tool Call Request\n(bash, edit, write, view, web_fetch)"]
    end

    subgraph EvaluationPipeline ["Layered Permission Pipeline"]
        ModeCheck{"Session Mode Check\n(default | plan | auto)"}
        PathGuard{"Path Confinement (guard)\nWithin Workspace Boundary?"}
        ShellGuard{"Shell AST Inspection (shellguard)\nCompound Syntax Analysis"}
        RuleEngine{"Rule Matching Hierarchy\n(deny -> allow -> ask)"}
    end

    subgraph Resolution ["Resolution & Dispatch"]
        AutoExecute["Auto-Approved\n(Execute in Sandbox)"]
        UserPrompt["Interactive Prompt\n(CLI Approval Request)"]
        Reject["Access Denied\n(Return Security Error)"]
    end

    ToolCall --> ModeCheck
    ModeCheck -->|Plan Mode & Mutating Tool| Reject
    ModeCheck -->|Allowed by Mode| PathGuard

    PathGuard -->|Path Traversal / Escape| Reject
    PathGuard -->|Path Confined| ShellGuard

    ShellGuard -->|Destructive / Blocked Pattern| Reject
    ShellGuard -->|Syntactically Safe| RuleEngine

    RuleEngine -->|Explicit Deny| Reject
    RuleEngine -->|Explicit Allow| AutoExecute
    RuleEngine -->|Explicit Ask / Default| UserPrompt

    UserPrompt -->|User Approves (once, session, workspace)| AutoExecute
    UserPrompt -->|User Denies| Reject
```

---

## 2. Core Architectural Design

### 1. Layered Evaluation Pipeline
Every tool call issued by an agent passes through a multi-stage security pipeline before reaching the execution environment:
- **Session Mode Enforcement**: Evaluates the tool against the active session mode. In **Plan Mode** (`plan`), all mutating tools (`write`, `edit`, `bash`) are automatically blocked. In **Auto Mode** (`auto`), pre-approved non-destructive operations execute autonomously.
- **Hierarchical Rule Engine**: Custom permission rules are evaluated in strict priority order: **`deny` $\to$ `allow` $\to$ `ask`**. A matching `deny` rule immediately terminates execution with an error, taking precedence over all other settings.

### 2. Shell AST Inspection (`shellguard`)
Rather than relying on naive regex pattern matching, TaskSmith parses every shell command into an Abstract Syntax Tree (AST):
- **Deep Syntax Decomposition**: Pipelines (`a | b`), sequential commands (`a && b`, `a ; b`), subshells `(a)`, and command substitutions (`$(a)`, `` `a` ``) are unpacked and analyzed individually.
- **Smuggling Prevention**: Attackers cannot bypass guards by hiding dangerous commands inside quoted variables, encoded subshells, or deep pipe chains.
- **Destructive Command Classification**: High-risk system patterns (such as root deletions, raw device writes, and unconfined network downloads piped into shells) are intercepted and rejected before execution.

### 3. Path Confinement & Boundary Guards (`guard`)
File manipulation tools (`view`, `edit`, `write`, `glob`) are strictly bound to the workspace:
- **Path Canonicalization**: Evaluates relative paths and resolves symlinks to ensure targets remain within the workspace root directory.
- **Boundary Validation**: Traversal attempts (`../../etc/passwd`) or symlinks resolving to external files are immediately blocked at the guard layer before reaching the sandbox filesystem capability.

### 4. Scoped Approvals & Persistence
When a tool invocation requires explicit user approval, the interactive prompt allows the user to scope their decision:
- **`once`**: Applies exclusively to the single in-flight tool call.
- **`session`**: Stored in-memory for the duration of the active CLI session, auto-approving matching invocations until exit.
- **`workspace`**: Persisted to the workspace's local state directory (`~/.local/share/tasksmith/workspaces/<hash>/permissions.json`), automatically allowing matching operations in future sessions for that project.
- **`global`**: Configured globally in `~/.config/tasksmith/permissions.json` across all projects on the workstation.
