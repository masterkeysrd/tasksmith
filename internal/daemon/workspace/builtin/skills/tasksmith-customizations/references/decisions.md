# Decisions

A Decision describes a probabilistic evaluation using a shared rubric and structured questions. Questions can represent a binary predicate, a categorical choice, or a score on an ordered scale. This is a WARP resource definition, not a permission decision or a deterministic rule.

## Discovery and support

Decision manifests are commonly stored in `.agents/decisions/<name>.decision.md` in a workspace or project, or under `$XDG_CONFIG_HOME/tasksmith/agents/decisions/` for a user. They follow the normal resource hierarchy (`local > workspace > user > system`) and can also be supplied by system resources or workspace plugins. Project `.agents/decisions/**` files participate in workspace trust fingerprinting (restricted workspaces ignore repo overrides, and edits re-prompt trust).

TaskSmith evaluates the built-in `permission-reviewer` Decision at runtime when a session is in `auto` mode (`internal/daemon/permreview`):
- It must expose a single `choice` question named `verdict` with options `approve_once` and `defer_to_user`.
- `spec.models` defaults to `["@permission-reviewer", "@default", "@fast"]` (`@default` resolves strictly against the `decision` role; non-`@default` entries try `decision` then `completion` adapted via `LLMAdapter`).
- `permission-reviewer.decision.md` is the single source of truth for calibration: Go validates that `threshold` is in `(0, 1]` (inheriting the built-in manifest's `0.85` threshold when a higher-tier override omits `threshold`) and evaluates both native `decision.Provider` and adapted `LLMAdapter` models against that exact threshold without separate daemon floors.
- General user-defined `Decision` triggers on `Rule` manifests are still not evaluated at runtime.

## Definition

A Markdown manifest has WARP YAML frontmatter followed by a general evaluation rubric. The body automatically becomes `instructions`. Questions can be declared in the manifest; the WARP model also allows them to be supplied dynamically by a runtime that supports evaluation.

| Field | Type | Meaning |
| --- | --- | --- |
| `spec.models` | string[] | Prioritized decision model identifiers intended to override workspace and provider defaults. |
| `spec.argumentHint` | string | Single-line description of expected input. |
| `spec.arguments` | object[] | Structured parameters with `name`, `description`, `required`, and `default`. |
| `spec.questions` | object[] | Predicate, choice, or score questions. |

The instruction body is rendered as a template when the workspace rendering API is used. Named arguments can be referenced as `$name` or `{{.name}}`; defaults and required-argument checks apply during rendering. Rendering the rubric is not the same as evaluating the questions.

### Questions

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Question identifier, unique within the Decision. |
| `type` | string | `predicate`, `choice`, or `score`. |
| `instructions` | string | Question-specific condition or prompt. |
| `threshold` | float | Minimum probability threshold in `(0, 1]` (used by `predicate` and as the single source of truth for `permission-reviewer`'s `verdict` choice). |
| `minConfidence` | float | Minimum answer confidence from `0.0` to `1.0`. |
| `criteria` | object | Predicate criteria under `true` and `false`. |
| `options` | object[] | At least two choices for `choice`; each has a `value` and optional `description`. |
| `levels` | object[] | At least two ordered levels for `score`; each has a `label` and optional `description`. |

The question `name` and `type`, each option's `value`, and each level's `label` identify the intended results. For `permission-reviewer`, the daemon enforces the `verdict` `threshold` declared in the manifest (default `0.85` in the built-in system manifest) and optional `minConfidence`. Custom `Decision` manifests outside `permission-reviewer` remain declarative only.

## Example

```markdown
---
apiVersion: warp/v1alpha1
kind: Decision
metadata:
  name: pr-triage
  description: Classify pull request review needs
spec:
  models: [typesafe/jev-latest]
  questions:
    - name: security_sensitive
      type: predicate
      instructions: Does the change affect access control?
      threshold: 0.7
      criteria:
        "true": Changes authentication or permissions.
        "false": Does not change authentication or permissions.
    - name: reviewer_team
      type: choice
      instructions: Which team should review the change?
      options:
        - value: security
          description: Authentication or permissions changes.
        - value: backend
          description: Service or database changes.
    - name: urgency
      type: score
      levels:
        - label: low
          description: Routine maintenance.
        - label: high
          description: Critical production issue.
---
# Pull request triage
Evaluate the change against each question using the available evidence.
```

General custom Decision triggers remain un-evaluated; only the built-in `permission-reviewer` Decision (and overrides of that name) runs at runtime in `auto` mode.
