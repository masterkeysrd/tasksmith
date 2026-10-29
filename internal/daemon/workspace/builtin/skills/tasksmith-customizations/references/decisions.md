# Decisions

A Decision describes a probabilistic evaluation using a shared rubric and structured questions. Questions can represent a binary predicate, a categorical choice, or a score on an ordered scale. This is a WARP resource definition, not a permission decision or a deterministic rule.

## Discovery and support

Decision manifests are commonly stored in `.agents/decisions/<name>.decision.md` in a workspace or project, or under `$XDG_CONFIG_HOME/tasksmith/agents/decisions/` for a user. They follow the normal resource hierarchy and can also be supplied by system resources or workspace plugins.

**TaskSmith does not currently support evaluating Decision resources.** The workspace can discover, look up, and render their instruction templates, but no runtime path evaluates their questions or produces probabilistic answers. Declaring a Decision, selecting decision models, or referring to a Decision from a Rule does not make it execute. The fields below describe the WARP schema, not active TaskSmith evaluation behavior.

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
| `threshold` | float | Predicate probability threshold from `0.0` to `1.0`. |
| `minConfidence` | float | Minimum answer confidence from `0.0` to `1.0`. |
| `criteria` | object | Predicate criteria under `true` and `false`. |
| `options` | object[] | At least two choices for `choice`; each has a `value` and optional `description`. |
| `levels` | object[] | At least two ordered levels for `score`; each has a `label` and optional `description`. |

The question `name` and `type`, each option's `value`, and each level's `label` identify the intended results. Thresholds and confidence settings describe evaluation behavior for a supporting runtime; TaskSmith does not currently apply them.

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

The model ID is illustrative and the manifest will not be evaluated by the current TaskSmith runtime.
