---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: docs-maintenance
  displayName: Documentation Maintenance & Progressive Disclosure
  description: Synchronization, portability, and progressive-disclosure rules when changing user-facing behavior, schemas, or documentation
  labels:
    category: quality
spec:
  priority: 45
  triggers:
    globs:
      - "docs/**"
      - "README.md"
      - "ROADMAP.md"
      - "AGENTS.md"
      - "api/**"
      - "internal/cmd/**"
      - "internal/cli/**"
      - "internal/exec/**"
      - "internal/acp/**"
      - "internal/daemon/tools/builtin/**"
      - "internal/daemon/workspace/**"
      - "internal/daemon/gateway/**"
      - "internal/daemon/sandbox/**"
      - "internal/daemon/sandboxcfg/**"
      - "internal/daemon/permissions/**"
      - "internal/daemon/mcp/**"
    keywords:
      - docs
      - documentation
      - readme
    useWhen: Authoring or updating documentation, README.md, or modifying user-facing CLI flags, built-in tools, WARP manifest schemas, providers, permissions, or subsystem architecture
---

# Documentation Maintenance & Progressive Disclosure

Maintain strict documentation synchronization, progressive disclosure, portability, and source fidelity whenever modifying TaskSmith's user-facing surfaces or documentation.

---

## 1. When to Update Documentation

Whenever modifying code that alters user-facing behavior, configuration schemas, or system architecture, update the corresponding documentation in the same change:

- **CLI Commands, Flags, & Shortcuts**:
  - Updates to `internal/cmd/`, `internal/cli/`, or REPL keybindings must update `docs/reference/cli.md`.
  - Every CLI flag, slash command alias, and keyboard shortcut table must reflect real source behavior.
- **Built-in System Tools**:
  - Updates to `internal/daemon/tools/builtin/`, `api/tools.go`, or tool manifests (`*.tool.md`) must update `docs/reference/tools.md`.
  - Tool parameters, output types, default permission behaviors, and safety annotations (`isReadOnly`, `isDangerous`, `isOpenWorld`) must match tool definitions exactly.
- **Manifest Specifications**:
  - Updates to `internal/daemon/workspace/` or WARP schemas must update `docs/reference/manifests.md`.
  - Every resource kind (`Workspace`, `Context`, `Agent`, `Skill`, `Command`, `Decision`, `Rule`, `Tool`, `MCP`, `ModelProvider`, `Plugin`) must have up-to-date schema tables and annotated examples.
- **Model Providers & Gateways**:
  - Updates to `internal/daemon/gateway/` or provider presets must update `docs/reference/providers.md` and the appropriate guide in `docs/reference/providers/`.
- **User Workflows (`docs/guides/`)**:
  - Updates introducing or changing a user-facing concept or interactive workflow must update the corresponding guide (`cli.md`, `workspaces.md`, `permissions.md`, `customizations.md`, `tools-and-mcp.md`).
- **Subsystem Architecture (`docs/architecture/`)**:
  - Updates changing the high-level mental model, lifecycle, or security boundaries of subsystems must update the corresponding specification (`daemon.md`, `harness.md`, `gateway.md`, `sandbox.md`, `permissions.md`).
- **Public Roadmap (`ROADMAP.md`)**:
  - When completing, rescoping, or adding a milestone capability tracked in `ROADMAP.md`, update its checkbox/entry in `ROADMAP.md` in the same change.

### When NOT to Update Documentation

- Do **not** update `docs/` for internal bug fixes, code refactoring, test additions, performance optimizations, or low-level implementation details that do not alter user-facing behavior, configuration schemas, or high-level subsystem architecture.

---

## 2. Progressive Disclosure & Style

Layer documentation so readers and agents only pay for the level of detail their task needs:

- **Reference Docs (`docs/reference/`) — Crisp & Tabular**:
  - Schema-focused Markdown tables only (flags, parameters, fields, defaults, types).
  - Avoid narrative tutorials in reference docs; link to User Guides (`../guides/`) or Architecture Deep Dives (`../architecture/`) instead.
- **User Guides (`docs/guides/`) — Workflow-Oriented**:
  - Explain end-to-end concepts and practical examples; link to `../reference/` for exhaustive field tables rather than duplicating schemas.
- **Architecture Specs (`docs/architecture/`) — Boundaries & Invariants**:
  - Focus on mental models, state lifecycles, and security boundaries rather than line-by-line code walkthroughs that go stale.
- **Agent Context (`AGENTS.md`, `.agents/rules/`, `.agents/skills/`)**:
  - Keep always-on context (`AGENTS.md`, `triggers.always: true`) short, high-signal, and focused on non-obvious repository-wide knowledge; place path/tool-specific guardrails in conditional `.agents/rules/*.rule.md` and multi-step domain playbooks in `.agents/skills/<name>/SKILL.md` + `references/*.md`.
- **Strict Source Truth**:
  - Never fabricate flags, arguments, or schema properties. Verify every documented detail against actual Go structs, CLI commands, and manifest definitions.

---

## 3. Zero Machine-Local Paths

- **Forbidden Patterns**:
  - Never commit machine-local absolute home or user-directory paths in documentation, comments, examples, or tests.
  - Never use local file-scheme URIs in Markdown links or documentation text.
- **Required Conventions**:
  - Always use **relative Markdown links** for cross-document links (e.g., `../guides/workspaces.md`, `./providers.md`, `../architecture/sandbox.md`).
  - Use **portable paths** when documenting filesystem layout:
    - User configuration: `~/.config/tasksmith/` or `$XDG_CONFIG_HOME/tasksmith/`
    - Application state & data: `~/.local/share/tasksmith/` or `$XDG_DATA_HOME/tasksmith/`
    - Daemon runtime sockets: `$XDG_RUNTIME_DIR/tasksmith/brain.sock` (fallback `~/.local/state/tasksmith/run/brain.sock`)
    - Logs: `~/.local/state/tasksmith/logs/` or `$XDG_STATE_HOME/tasksmith/logs/`
    - Workspace resources: `<workspace-root>/`, `.agents/`, `WORKSPACE.md`, `AGENTS.md`
