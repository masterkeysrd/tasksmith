# Documentation Maintenance Rule

All agents and contributors modifying TaskSmith must maintain strict documentation synchronization, portability, and structural integrity.

---

## 1. When to Update Documentation

Whenever modifying code that alters user-facing behavior, configuration schemas, or system architecture, the corresponding documentation must be updated in the same change:

- **CLI Commands, Flags, & Shortcuts**:
  - Updates to `internal/cmd/`, `internal/cli/`, or REPL keybindings must update `docs/reference/cli.md`.
  - Every CLI flag, slash command alias, and keyboard shortcut table must reflect real source behavior.

- **Built-in System Tools**:
  - Updates to `internal/daemon/tools/builtin/`, `api/tools.go`, or tool manifests (`*.tool.md`) must update `docs/reference/tools.md`.
  - Tool parameters, output types, default permission behaviors, and safety annotations (`isReadOnly`, `isDangerous`, `isOpenWorld`) must match tool definitions exactly.

- **Manifest Specifications**:
  - Updates to `internal/daemon/workspace/types.go` or Warp schemas must update `docs/reference/manifests.md`.
  - Every resource kind (`Workspace`, `Context`, `Agent`, `Skill`, `Command`, `Decision`, `Tool`, `Toolkit`, `MCP`) must have up-to-date schema tables and annotated examples.

- **Model Providers & Gateways**:
  - Updates to `internal/daemon/gateway/` or provider presets must update `docs/reference/providers.md` and the appropriate guide in `docs/reference/providers/`.

- **User Workflows (`docs/guides/`)**:
  - Updates introducing or changing a user-facing concept or interactive workflow must update the corresponding guide (`cli.md`, `workspaces.md`, `permissions.md`, `customizations.md`, `tools-and-mcp.md`).

- **Subsystem Architecture (`docs/architecture/`)**:
  - Updates changing the high-level mental model, lifecycle, or security boundaries of subsystems must update the corresponding specification (`daemon.md`, `harness.md`, `gateway.md`, `sandbox.md`, `permissions.md`).

### When NOT to Update Documentation

- **Not every code change requires documentation updates.**
- Do **not** update `docs/` for internal bug fixes, code refactoring, test additions, performance optimizations, or low-level implementation details that do not alter user-facing behavior, configuration schemas, or high-level subsystem architecture.
- Only document changes that affect users or the conceptual architecture of TaskSmith.

---

## 2. Zero Machine-Local Paths

- **Forbidden Patterns**:
  - Never commit machine-local absolute file paths (e.g., `/Users/...`, `/home/...`, or system-specific user directories) in documentation, comments, examples, or tests.
  - Never use `file://` URI schemes in markdown links or documentation text.

- **Required Conventions**:
  - Always use **relative markdown links** for cross-document links (e.g., `../guides/workspaces.md`, `./providers.md`, `../architecture/sandbox.md`).
  - Use **portable paths** when documenting filesystem layout:
    - User configuration: `~/.config/tasksmith/` or `$XDG_CONFIG_HOME/tasksmith/`
    - Application state & data: `~/.local/share/tasksmith/` or `$XDG_DATA_HOME/tasksmith/`
    - Daemon runtime sockets: `/tmp/tsd-<uid>/` or `$XDG_RUNTIME_DIR/tasksmith/`
    - Logs: `~/.local/state/tasksmith/logs/` or `$XDG_STATE_HOME/tasksmith/logs/`
    - Workspace resources: `<workspace-root>/`, `.agents/`, `WORKSPACE.md`, `AGENTS.md`

---

## 3. Reference Style & Codebase Fidelity

- **Crisp & Tabular**:
  - Reference documentation must be schema-focused and organized with clear markdown tables.
  - Avoid duplicate narrative tutorials in reference docs; link to User Guides (`docs/guides/`) or Architecture Deep Dives (`docs/architecture/`) instead.

- **Strict Source Truth**:
  - Never fabricate flags, arguments, or schema properties. All documentation must be verified against actual Go structs, CLI commands, and manifest definitions.
