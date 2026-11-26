# Decision Manifest (`*.decision.md`)

Configures probabilistic System 1 decision classifiers for deterministic routing or rubric scoring. Placed under `.agents/decisions/<name>.decision.md` (and trust-fingerprinted alongside `.agents/tools/` and `.agents/mcps/`). TaskSmith ships a built-in `permission-reviewer` decision (`kind: Decision`) that evaluates risky `auto`-mode permission cards across the `local > workspace > user > system` hierarchy.

## Example

The same `diff` supplies three different answers: a thresholded yes/no finding, a named coverage category and an ordered architecture rating. Declaring the rubric alone does not schedule it to run.

```markdown
---
apiVersion: warp/v1alpha1
kind: Decision
metadata:
  name: code-review-gate
  displayName: Code Review Quality Gate
  description: Classifies pull requests for security and stability risks
spec:
  models:
    - typesafe/jev-latest
  argumentHint: "Usage: evaluate code diff against security criteria"
  arguments:
    - name: diff
      description: Unified diff to evaluate
      required: true
  questions:
    - name: introduces_vulnerability
      type: predicate
      instructions: "Determine if the diff introduces a SQL injection, XSS, or unauthorized path traversal."
      threshold: 0.85
      minConfidence: 0.90
      criteria:
        true: "Contains unescaped user input passed to SQL or shell commands."
        false: "Input is fully validated, parameterized, and sanitized."
    - name: test_coverage_tier
      type: choice
      instructions: "Evaluate the test coverage included with this change."
      options:
        - value: complete
          description: "Unit and integration tests cover all modified code paths."
        - value: partial
          description: "Basic smoke tests present, but edge cases omitted."
        - value: none
          description: "No automated tests included with the diff."
    - name: architecture_score
      type: score
      instructions: "Rate conformity to clean architecture principles."
      levels:
        - label: low
          description: "Tight coupling, business logic mixed into presentation."
        - label: medium
          description: "Reasonable modularity with minor boundary leaks."
        - label: high
          description: "Exemplary decoupling and clear inversion of control."
---

Evaluate the provided pull request diff against the defined criteria.
```

## Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `models` | array of strings | Preferred decision models (e.g. `typesafe/jev-latest`). |
| `argumentHint` | string | Guidance string for invocation syntax. |
| `arguments` | array of objects | Structured parameter declarations accepted by this decision. |
| `questions` | array of objects | Structured evaluation questions. |

### Question Object Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `name` | string | Unique question identifier. |
| `type` | string | Question kind: `predicate` (boolean), `choice` (categorical), or `score` (ordinal). |
| `instructions` | string | Specific evaluation instructions for this question. |
| `threshold` | float | Minimum decision probability threshold (0.0 to 1.0) for `predicate` or target `choice` option (for `permission-reviewer`, validated in `(0, 1]`, defaulting to `0.85` from the built-in manifest). |
| `minConfidence` | float | Minimum required model confidence score. |
| `criteria.true` | string | Explicit positive condition description for `predicate`. |
| `criteria.false` | string | Explicit negative condition description for `predicate`. |
| `options` | array of `{value, description}` | Allowed categories for `choice` questions. |
| `levels` | array of `{label, description}` | Ordinal rubric levels for `score` questions. |

## Argument fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `name` | string | **Yes** | Identifier used for template variable substitution (`{{.name}}` or `$name`). |
| `description` | string | No | Guidance describing the purpose and expected format of the argument. |
| `required` | boolean | No | When `true`, invocation fails validation if the argument is omitted and has no default. |
| `default` | string | No | Fallback value automatically injected by the renderer when omitted. |

## See Also

- [Automatic permission review](../../guides/auto-permission-review.md)
- [Manifest hierarchy and metadata](../manifests.md)
