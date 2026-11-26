# Agent Manifest (`*.agent.md`)

Defines autonomous agent personas, allowed tools, subagents, and cognitive configuration. Placed under `.agents/agents/<name>.agent.md` or `~/.config/tasksmith/agents/`.

## Example

`security-auditor` extends `main` and accepts both human and agent callers. Its `policies.tools.include` limits it to inspection tools even if the parent persona normally writes files.

```markdown
---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: security-auditor
  displayName: Security Auditor
  description: Audits code for vulnerabilities, injection flaws, and auth gaps
spec:
  extends: main
  triggers:
    - human
    - agent
  models:
    - anthropic/claude-opus-5
    - openai/o3
  temperature: 0.2
  thinking:
    type: effort
    effort: high
  tools:
    - view
    - grep
    - glob
  skills:
    - go-security
    - owasp-checklist
  commands:
    - audit
  subagents:
    - code-searcher
  rules:
    - go-conventions
  policies:
    tools:
      include:
        - view
        - grep
        - glob
      exclude:
        - write
        - edit
        - remove
        - bash
---

You are an expert application security auditor.
Your goal is to inspect code for OWASP Top 10 vulnerabilities, unauthorized path traversals, and cryptographic weaknesses.
Never attempt to modify code directly; report your findings with precise line references and remediation steps.
```

## Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `extends` | string | Qualified or short name of a base agent to inherit prompts and capabilities from. |
| `triggers` | array of strings | Allowed caller types (`human`, `agent`, `system`). Empty allows all callers. Agents without `human` cannot be selected by human users via `--agent` or `/agent`. |
| `models` | array of strings | Ordered model preferences (`provider/model`, model alias such as `@fast` or `@default`, `@inherit`, or glob patterns such as `"*"`, `"ollama/*"`, and `"!pattern"` negations). When an agent runs as a subagent, this list also acts as the allowlist enforced on `invoke_agent`'s `model` parameter. See [tier aliases](../providers.md#tier-aliases). |
| `temperature` | float | Sampling temperature override (`0.0` to `2.0`). Sent when supported; omitted with a session warning when unsupported. |
| `thinking.type` | string | Reasoning strategy: `toggle`, `effort`, `budget`, or `adaptive`. |
| `thinking.effort` | string | Effort level for the `effort` strategy (e.g. `low`, `medium`, `high`). |
| `thinking.budgetTokens` | integer | Thinking token budget for the `budget` strategy. |
| `thinking.enabled` | boolean | Thinking toggle for the `toggle` strategy. |
| `tools` | array of strings | Eager tools loaded on the first request (`*`, `!pattern`, or explicit tool names). Defaults to `[]` when omitted. Policy-allowed tools not listed in `spec.tools` (and MCP tools by default) are loaded as deferred tools discoverable via `tool_search`. Advising more than 20 eager tools emits a warning. |
| `skills` | array of strings | Allowed skills (`*`, `!pattern`, or explicit skill names). |
| `commands` | array of strings | Allowed slash commands (`*`, `!pattern`, or explicit command names). |
| `subagents` | array of strings | Allowed subagents (`*`, `!pattern`, or explicit agent names). Only agents allowing the `agent` trigger can be invoked as subagents. |
| `rules` | array of strings | Rule references (`*`, qualified/short names, or globs). Each explicitly listed rule is attached as an always-on rule for this agent. Rules with `triggers.always: true` attach regardless of this field. `["*"]` also surfaces conditional rules for on-demand loading; omitted or `[]` attaches only always-on rules. |
| `policies.tools` | object | Unbypassable security policy ceiling for eager tools, deferred tools (`tool_search`), and skill/command tools (`include`, `exclude`, `dangerous`, `openWorld`, `readOnly`, `ask`). The resolved tool list is an explicit allowlist; an agent whose resolved tools are empty receives zero tools. |
| `sandbox` | string | WARP sandbox reference, or `none`. This schema field does not override TaskSmith daemon isolation. |

*The Markdown body below the frontmatter contains the system persona prompt.*

Three built-ins are activated automatically when the workspace gives them something to do, so they may appear in the model's context without being listed in `spec.tools`: `tool_search` when any tool is deferred, `load_rule` when the agent resolves at least one conditional rule, and `read_mcp_resource` when a connected MCP server publishes a permitted resource. `policies.tools` still governs them, so denying one removes it.

## Model selection

| `models` entry | Delegated session behavior |
| --- | --- |
| Concrete ID or alias | First resolvable preference supplies the default. |
| `@inherit` | Select the delegating session's active model. |
| `"ollama/*"`, `"!ollama/tiny"` | Match or exclude requested overrides; globs do not select a concrete default. |
| Empty list | Use workspace defaults and accept any explicit override. |

## See Also

- [Agents and subagents](../../guides/agents-and-subagents.md)
- [Manifest hierarchy and metadata](../manifests.md)
