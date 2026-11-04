# Rules

A Rule is an ambient instruction: a standard or guardrail an agent must follow when it applies. It is not a TaskSmith permission rule and not project guidance in `AGENTS.md`; it constrains behavior through the prompt, while permissions still decide what a tool may do. See `workspace-and-context.md` for `WORKSPACE.md` and `AGENTS.md`.

## Discovery and runtime

Rules are discovered recursively in user, workspace, project, and system resource directories, conventionally at `.agents/rules/<name>.rule.md`, and follow the normal namespace hierarchy.

TaskSmith resolves them against the active agent and injects them in two ways:

- **Always-on** rules (`triggers.always: true`) are rendered into a `<workspace_rules>` block in the static system prompt, so they sit in the cached prefix on every turn.
- **Conditional** rules appear only as a name-and-trigger entry in `<available_rules>`. The agent loads the body with `load_rule` when relevant, or the harness injects it automatically when a trigger fires.

An injection writes the rendered body into a hidden `<system_reminders>` message and emits a `● Loaded Rule …` line in the transcript, so the user sees it without the raw XML appearing in the conversation. Each rule is injected at most once per compaction epoch; loaded and suggested sets reset on compaction, so long sessions do not accumulate stale rule bodies.

## Triggers

| Trigger | Evaluation | Result |
| --- | --- | --- |
| `triggers.always` | Static prompt rendering | Full body always present. |
| `triggers.globs` | A tool reads, writes, searches, or executes against a matching workspace-relative path, including `bash` working directories and paths reported by `glob`, `grep`, and `ls` output | Auto-injects the full body. |
| `triggers.tools` | A matching tool is invoked | Auto-injects the full body. |
| `triggers.keywords` | A keyword matches a user message on a word boundary | Suggests only: the agent is prompted to call `load_rule` if applicable. Never auto-loads. |
| `triggers.useWhen` | Shown to the model in `<available_rules>` | Advisory text only. |
| `triggers.decision` | **Unsupported for rules** | Ignored, with a startup warning that the reference is dropped. |

Globs are workspace-relative and support `**` plus `!` negation; a pattern without a slash also matches on base name. Tool patterns support `*` wildcards and match case-insensitively.

A user can force a rule with `@rule:<name>` anywhere in a prompt, which loads it immediately instead of waiting for a trigger.

## Attaching rules to an agent (`spec.rules`)

`spec.rules` in an Agent manifest adds rules on top of always-on activation:

| `spec.rules` | Effect |
| --- | --- |
| Omitted or `[]` | Only `triggers.always: true` rules apply. Conditional rules are not visible to the agent. |
| Explicit names or globs | Matched rules attach as always-on for this agent, in addition to any always rule. |
| `["*"]` | Attaches every rule in scope; always rules stay always, conditional rules stay loadable on demand. |

There is no opt-out from an always-on rule: it reaches every agent in scope regardless of `spec.rules`. Commands may also declare `spec.rules`, whose bodies are embedded when the command runs.

## Guidance on cost

An always-on rule is paid on every turn by every agent. Reserve `triggers.always` for constraints that must never be skipped, keep such bodies short, and prefer conditional triggers so a long body only enters context when it is actually relevant. Use `keywords` for cheap discovery rather than automatic enforcement, since a keyword match only advises the model.

## Definition

The Markdown body supplies the instructions. Unlike skills, rules declare no `spec.arguments`; the body renders only against resource-level template globals such as `{{.WorkspaceDir}}`, `{{.ProjectDir}}`, and `{{.Agent}}`.

| Field | Meaning |
| --- | --- |
| `spec.priority` | Ordering when several rules inject together; higher wins, ties break by name. |
| `spec.triggers.always` | Inject unconditionally for every agent in scope. |
| `spec.triggers.globs` | Path patterns, including negations such as `!**/vendor/**`. |
| `spec.triggers.tools` | Tool names or glob patterns that activate the rule. |
| `spec.triggers.keywords` | Word-boundary keywords that produce a suggestion. |
| `spec.triggers.useWhen` | Condition shown to the model for on-demand loading. |
| `spec.triggers.decision` | Accepted by the parser but ignored at runtime; emits a warning. |

```markdown
---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: portable-docs
  description: Keep documentation paths portable
spec:
  priority: 10
  triggers:
    globs: ["**/*.md", "!**/testdata/**"]
    keywords: [docs, paths]
    useWhen: Editing Markdown documentation
---
Use relative links and portable paths; never write machine-local absolute paths.
```

Inspect what a workspace resolved with `/rules`, which shows each rule's status (`[Always]`, `[Loaded]`, `[Standby]`), scope, triggers, and token estimate, and reloads definitions from disk with `r`.
