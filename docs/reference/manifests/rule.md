# Rule Manifest (`*.rule.md`)

Ambient constraints, coding standards, and tool/path guardrails that govern agent behavior. Placed under `.agents/rules/<name>.rule.md` (workspace) or `<subproject>/.agents/rules/<name>.rule.md` (project). A rule is a behavioral instruction, not a permission rule; enforcement of tool and file access still follows [Permissions](../../architecture/permissions.md).

## Injection Model

| Rule kind | How it reaches the model |
| :--- | :--- |
| **Always-on** (`triggers.always: true`) | Rendered into a `<workspace_rules>` block in the static system prompt, so it stays in the cached prefix on every turn. |
| **Conditional** (any trigger other than `always`) | Listed by name in an `<available_rules>` discovery block only. The agent loads the full body on demand with `load_rule`, or the harness auto-injects it when a trigger fires. |

Conditional rules activate through three deterministic paths plus one advisory path, each injecting the full rendered body into a hidden `<system_reminders>` message and emitting a visible `● Loaded Rule …` timeline line:

| Trigger | Evaluated when | Effect |
| :--- | :--- | :--- |
| `@rule:<name>` in a user prompt | Any user message | Loads the full rule immediately (`manual`). |
| `triggers.tools` | A matching tool is invoked | Loads the full rule (`tool`). |
| `triggers.globs` | A tool reads, writes, searches, or executes against a matching workspace-relative path | Loads the full rule (`glob`). |
| `triggers.keywords` | A keyword appears in a user message | Advisory only: suggests the rule so the agent may call `load_rule`. |

Each rule is injected at most once per compaction epoch; `LoadedRules` and `SuggestedRules` reset on compaction, so long sessions do not accumulate stale rule bodies.

## Example

Editing a Go source file can load this rule; mentioning “sentinel” only suggests it. The negated path pattern excludes `testdata` from the glob trigger, not from the independent tool trigger.

```markdown
---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: go-error-handling
  displayName: Go Error Handling Conventions
  description: Enforces sentinel error wrapping and prohibits panics in Go packages
  labels:
    category: backend
spec:
  priority: 10
  triggers:
    globs:
      - "**/*.go"
      - "!**/testdata/**"
    tools:
      - write
      - edit
    keywords:
      - error
      - sentinel
      - panic
    useWhen: "Writing, refactoring, or reviewing error handling logic in Go source files."
---

# Go Error Handling Rules

1. Always wrap lower-level errors with `%w` and a package-prefixed context string.
2. Define exported sentinel errors (`ErrInvalidConfig`, `ErrNotFound`) in `types.go` or `errors.go`.
3. Never use `panic` for recoverable runtime errors.
```

## Spec Reference

| Field | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `priority` | integer | `0` | Ordering when several rules resolve together; higher wins, ties break by `metadata.name`. |
| `triggers.always` | boolean | `false` | Injects the full body into `<workspace_rules>` for every agent in scope. |
| `triggers.globs` | array of strings | `nil` | Workspace-relative path patterns supporting `**` and `!` negation. A pathless pattern also matches on base name. |
| `triggers.tools` | array of strings | `nil` | Tool names or glob patterns (`*`, `github__*`); matched case-insensitively, also against short names of qualified tools. |
| `triggers.keywords` | array of strings | `nil` | Case-insensitive word-boundary keywords or phrases that produce a suggestion, never an automatic load. |
| `triggers.useWhen` | string | `""` | Natural-language condition shown to the agent in `<available_rules>`. |
| `triggers.decision` | string | `""` | **Unsupported.** Decision triggers are not evaluated for rules; setting this adds a workspace warning in [`/warnings`](../cli/slash-commands.md#warnings-view-warnings) that the reference is ignored. |

*The Markdown body below the frontmatter contains the rule instructions. Unlike skills, rules declare no `spec.arguments`; the body renders only against resource-level template globals such as `{{.WorkspaceDir}}`, `{{.ProjectDir}}`, and `{{.Agent}}`.*

## Attaching Rules to an Agent (`spec.rules`)

`spec.rules` in an Agent manifest adds rules on top of always-on activation, and its behavior is frequently misunderstood:

| `spec.rules` | Effect |
| :--- | :--- |
| Omitted or `[]` | Only rules with `triggers.always: true` apply. Conditional rules are not visible to the agent. |
| Explicit names or globs | The matched rules are attached as always-on rules for this agent, in addition to any `triggers.always: true` rule. There is no way to opt out of an always-on rule. |
| `["*"]` | Attaches every rule in scope. Always-on rules stay always-on; conditional rules are attached as conditional, so they appear in `<available_rules>` and can be loaded with `load_rule`. |

Because a workspace-wide or user-wide `triggers.always: true` rule reaches every agent, reserve `always` for constraints that must never be skipped. Prefer conditional triggers for anything narrower, so the body only enters context when relevant.

## See Also

- [Skills and rules](../../guides/skills-and-rules.md)
- [Manifest hierarchy and metadata](../manifests.md)
