# Rules

A WARP Rule describes instructions and the conditions under which they are intended to apply. It is a resource definition, not a TaskSmith permission rule and not the same as project guidance in `AGENTS.md`.

## Discovery and runtime

Rule manifests can be discovered recursively in user, workspace, and project resource directories, commonly as `.agents/rules/<name>.rule.md`. They participate in the normal kind/name and namespace hierarchy. WARP parses and stores these definitions, but TaskSmith currently does not resolve, activate, or inject Rule resources into agent prompts. A matching glob, an `always` trigger, or a reference in an Agent, Context, or Workspace therefore does not itself enforce the rule. Project `AGENTS.md` context is a separate mechanism; see `workspace-and-context.md`.

## Definition

The Markdown body supplies the instructions. The following fields describe WARP's rule model; their presence does not imply TaskSmith evaluates them.

| Field | Meaning |
| --- | --- |
| `spec.priority` | Intended ordering priority; higher values take precedence. |
| `spec.triggers.always` | Intended unconditional activation within scope. |
| `spec.triggers.globs` | Path patterns, including negated patterns such as `!**/vendor/**`. |
| `spec.triggers.useWhen`, `keywords` | Semantic relevance cues. |
| `spec.triggers.decision` | Reference to a Decision. |
| `spec.triggers.tools` | Tool or Toolkit names associated with activation. |

```markdown
---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: portable-docs
  description: Keep documentation paths portable
spec:
  priority: 0
  triggers:
    globs: ["**/*.md"]
    keywords: [docs, paths]
    useWhen: Editing Markdown documentation
---
Use relative links and portable paths; do not include machine-local paths.
```
