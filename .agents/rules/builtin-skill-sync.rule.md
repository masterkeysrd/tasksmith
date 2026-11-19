---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: builtin-skill-sync
  displayName: Built-in Skill Synchronization
  description: Keeps embedded built-in agent skills in sync when user-facing configuration schemas, WARP manifests, CLI flags, or interactive command views change
  labels:
    category: quality
spec:
  priority: 45
  triggers:
    globs:
      - "api/**"
      - "internal/cmd/**"
      - "internal/cli/command/**"
      - "internal/daemon/workspace/**"
      - "internal/daemon/sandbox/**"
      - "internal/daemon/sandboxcfg/**"
      - "internal/daemon/permissions/**"
      - "internal/daemon/gateway/**"
      - "internal/daemon/mcp/**"
      - "internal/daemon/tools/builtin/**"
    useWhen: Adding or changing user-facing configuration schemas, WARP manifest fields, CLI flags, or interactive slash-command views
---

# Built-in Skill Synchronization

TaskSmith ships embedded skills under `internal/daemon/workspace/builtin/skills/` so the agent knows
how to configure TaskSmith features and guide users accurately. Whenever a change adds or alters a
user-configurable surface — WARP manifest fields, daemon or CLI XDG config files (`sandbox.json`,
`permissions.json`, `statusline.json`, `theme.json`), CLI flags/subcommands, interactive REPL
`/command` editors, template variables, or security/self-protection rules — update the matching
built-in skill in the same change alongside `docs/`.

Do **not** churn built-in skills for internal bug fixes, refactors, performance work, or test-only
changes that leave user-facing behavior and schemas unchanged.

## 1. Keep the two built-in skills cleanly separated

1. **`tasksmith-customizations`** (`internal/daemon/workspace/builtin/skills/tasksmith-customizations/`)
   Owns **client-agnostic** daemon and workspace configuration:
   - WARP resource schemas and precedence (`WORKSPACE.md`, `AGENTS.md`, `Rule`, `Skill`, `Agent`,
     `Command`, `Decision`, `Tool`, `MCP`, `ModelProvider`, `Plugin`).
   - Daemon-owned XDG configuration files (`~/.config/tasksmith/sandbox.json`,
     `~/.config/tasksmith/permissions.json`, and per-workspace
     `~/.local/share/tasksmith/workspaces/<hash>/` overrides), merge/lock rules, and template
     context fields.
   - Security and self-protection invariants the agent must respect at runtime (for example, when
     XDG policy files are kernel write-denied and must be configured through user flows or explicit
     JSON snippets rather than covert file writes).
   - Never hardcode CLI-only keyboard shortcuts or REPL-only views in `tasksmith-customizations` or
     `main.agent.md`, since the daemon backs multiple clients.

2. **`cli-customizations`** (`internal/daemon/workspace/builtin/skills/cli-customizations/`)
   Owns **CLI-specific** configuration, flags, and interactive workflows:
   - CLI XDG files (`~/.config/tasksmith/statusline.json`, `theme.json`).
   - CLI flags and subcommands (`--sandbox`, `-c`, `exec`, `auth`, `daemon`).
   - Interactive REPL `/command` views, tabs, and keybindings (`/statusline`, `/sandbox`,
     `/permissions`, `/providers`, `/model`, `/tools`, `/mcp`), plus `!command` shell behavior.

## 2. Authoring and verification checklist

- **Complementary, high-signal `description` + `useWhen`**: both fields are rendered into
  `<available_skills>` on every turn (`spec.keywords` is not rendered into the prompt). Keep each to
  1–2 crisp sentences without over-simplifying: `metadata.description` names the concrete surfaces
  and files the skill covers once, while `spec.useWhen` states the user intents/tasks that trigger
  loading it without repeating `description` word-for-word or dumping exhaustive flag lists.
- **Progressive-disclosure `SKILL.md` front pages**: keep each `SKILL.md` compact (~40–80 lines) —
  scope, non-negotiable safety/credential guardrails, and the `references/*.md` routing table. Place
  detailed schemas, keymaps, quota/exit-code tables, and examples in `references/*.md` verified
  against real Go structs and CLI handlers (never fabricate flags, keys, or defaults).
- **Zero machine-local paths**: use portable paths (`~/.config/tasksmith/`,
  `$XDG_CONFIG_HOME/tasksmith/`, `$XDG_DATA_HOME/tasksmith/`, `<workspace-root>/`) and relative
  links only; never write machine-local absolute paths or local-filesystem URI schemes.
- **Verify embedded sync**: when adding or renaming any file under
  `internal/daemon/workspace/builtin/`, assert in `internal/daemon/workspace/builtin_sync_test.go`
  that `EnsureBuiltinDir()` syncs it and embedded fallback renders cleanly.
