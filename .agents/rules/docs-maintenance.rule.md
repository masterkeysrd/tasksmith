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
      - "site/rspress.config.ts"
      - "site/pages/**"
      - "README.md"
      - "ROADMAP.md"
      - "AGENTS.md"
      - "api/**"
      - "client/**"
      - "internal/cmd/**"
      - "internal/cli/**"
      - "internal/ui/**"
      - "internal/tui/**"
      - "internal/exec/**"
      - "internal/acp/**"
      - "internal/daemon/**"
      - "internal/core/**"
      - ".agents/providers/**"
    keywords:
      - docs
      - documentation
      - readme
    useWhen: Authoring or updating documentation, README.md, or modifying user-facing CLI flags, built-in tools, WARP manifest schemas, providers, permissions, or subsystem architecture
---

# Documentation Maintenance & Progressive Disclosure

## 1. Source-to-Docs Routing Table

When behavior changes, update the matching guide and reference in the same change. Guide paths below are relative to `docs/guides/`, reference paths to `docs/reference/`, and daemon packages to `internal/daemon/`.

| Source or behavior | Guide | Reference |
| --- | --- | --- |
| `internal/cmd/`, installation and startup | `quickstart.md` | `cli/flags-and-subcommands.md` |
| `internal/cli/`, `internal/tui/`, `internal/ui/`, composer and shortcuts | `cli.md`, `appearance.md` | `cli/keybindings.md`, `cli/slash-commands.md`, `cli/config-files.md` |
| `internal/exec/`, `internal/acp/` | `headless-exec.md`, `acp.md` | `exec.md`, `cli/flags-and-subcommands.md` |
| `session/`, `timeline/`, `metrics/`, `journal/`, session history and usage | `sessions.md` | `cli/slash-commands.md`, `daemon-api.md` |
| `harness/`, delegation and plan review | `agents-and-subagents.md`, `modes-and-planning.md` | `manifests/agent.md`, `tools/agents-and-planning.md` |
| `permissions/`, `permreview/`, trust and approvals | `trust.md`, `permissions.md`, `auto-permission-review.md` | `manifests/workspace.md`, `manifests/decision.md`, `cli/keybindings.md` |
| `sandbox/`, `sandboxcfg/`, path and shell guards | `sandbox.md` | `cli/config-files.md`, `tools/execution-and-tasks.md` |
| `workspace/`, resource discovery and WARP | `workspaces.md`, `customizations.md`, `skills-and-rules.md`, `commands.md` | `manifests/workspace.md` (includes Context), `manifests/plugin.md`, `manifests/skill.md`, `manifests/rule.md`, `manifests/command.md` |
| `tools/`, `tasks/`, `mcp/`, `api/tools.go`, custom tool definitions | `tools-and-mcp.md` | Matching `tools/` page: `filesystem.md`, `execution-and-tasks.md`, `web.md`, `agents-and-planning.md`, or `workspace-resources.md`; `manifests/tool.md`, `manifests/mcp.md` |
| `gateway/`, `models/`, authentication and provider presets | `models-and-providers.md` | `providers.md` (shared schema), matching `providers/` page |
| `server/`, `broker/`, `api/`, `client/`, daemon transport and lifecycle | `sessions.md`, `headless-exec.md` | `daemon-api.md`, `exec.md` |

For changes to lifecycles or security boundaries, also update the matching `docs/architecture/` page (daemon, harness, gateway, sandbox, or permissions). Update `ROADMAP.md` when a tracked capability changes scope or completion status. Internal-only refactors, fixes, tests, or optimizations do not require documentation changes.

## 2. Modularity & User-Driven Style Guardrail

- Write `docs/guides/` for someone completing a task. Use a clear lead, **Before you start** when relevant, action-verb `##` headings for **On this page**, copy-paste examples, comparison tables where useful, and `## Next steps`. Aim for 120–260 lines per guide.
- Keep `docs/reference/` crisp and tabular. Put flags, defaults, fields, and safety annotations in `cli/`, `tools/`, `manifests/`, or `providers/` behind concise overview pages. Link to guides for walkthroughs.
- Never re-bloat `cli.md`, `tools.md`, `manifests.md`, or `customizations.md`. Extend a focused subpage or create one and register it in both `site/rspress.config.ts` and `docs/README.md`.
- When adding, removing, or renaming a page or heading, update incoming links and anchors, then run `npm run typecheck && npm run build` in `site/`. Resolve dead links before finishing.
- Describe architecture in terms of boundaries, lifecycles, and invariants. Keep always-on agent context short; put conditional guidance in rules and multi-step workflows in skills.
- Verify documented flags, arguments, and schema properties against their source definitions.

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
