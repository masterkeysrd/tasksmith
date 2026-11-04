# Agents

An Agent defines an actor's persona, model preferences, and available resources. TaskSmith selects an agent for a session and combines its rendered instructions with workspace instructions. Agent manifests are commonly stored in `.agents/agents/<name>.agent.md` and discovered in system, user, workspace, and current project scopes. The workspace can select a default with `spec.defaultAgent`; short and qualified names follow the resource hierarchy.

## Definition

An Agent uses WARP YAML frontmatter and a Markdown body. The body supplies `instructions` automatically; write the persona below the closing `---`, not as a frontmatter field. The body is rendered as a template for the session. The built-in main agent's template includes project context, but custom agents do not automatically receive it; see `workspace-and-context.md`.

All `spec` fields are optional:

| Field | Type | Meaning |
| --- | --- | --- |
| `extends` | string | Parent Agent short name or qualified reference such as `system/Agent/main`. |
| `triggers` | string[] | Allowed caller types: `human`, `agent`, or `system`. An empty list means unrestricted in WARP. All agents appear in `/agent` with a trigger badge, but human invocation (`--agent`, `/agent`, and human-initiated session creation or update) is restricted to agents that allow `human`. These are not natural-language trigger phrases. |
| `models` | string[] | Prioritized model identifiers; an unspecified or empty list leaves model selection to defaults. |
| `temperature` | float | Sampling temperature, conventionally `0.0`–`2.0`; when omitted it is unset rather than an explicit `0.0`. |
| `thinking` | object | Extended-reasoning configuration. `type` accepts `toggle`, `effort`, `budget`, or `adaptive`; all four strategies are supported end-to-end. For example `type: effort` with `effort: high`. |
| `tools` | string[] | Eager tools loaded on the first request (`*`, `!pattern`, or explicit tool names). When omitted, defaults to `[]`. Policy-allowed tools not listed here (and MCP tools by default) load as deferred tools discoverable via `tool_search`. Declaring more than 20 eager tools emits a warning. |
| `skills` | string[] | Skill names, qualified references, paths, or patterns to resolve. |
| `commands` | string[] | Command names, qualified references, paths, or patterns to resolve. |
| `subagents` | string[] | Agent names, qualified references, paths, or patterns to resolve for delegation. |
| `rules` | string[] | Rule names, qualified references, or patterns; each match attaches as an always-on rule for this agent. Rules with `triggers.always` attach regardless of this field, and `["*"]` also surfaces conditional rules for `load_rule`. See `rules.md`. |
| `policies` | object | Security and access settings, including tool restrictions under `policies.tools`. |

When a session has no explicit model, TaskSmith passes the agent's model list to model resolution and uses its first result; if nothing resolves, it falls back to the first listed identifier. An explicit session model takes precedence.

For skills, commands, subagents, and rules, an omitted list or `["*"]` selects all effective resources in WARP; an explicit `[]` selects none. Resolution also skips references that cannot be found. These are resolved-resource semantics, not a guarantee that every runtime path enforces every selector. In particular, slash-command lookup uses the workspace's command and skill lookup, rather than the resolved agent command list.

WARP filters resolved subagents to those whose `triggers` are empty or include `agent`. This is a resolution filter, not general enforcement of every invocation source by TaskSmith. When an agent's resolved `subagents` list is non-empty, TaskSmith registers the built-in `invoke_agent` and `manage_agents` tools for the session. Built-in agents include `system/Agent/main` (default coding persona), `system/Agent/explore` (fast read-only codebase explorer), `system/Agent/general` (general-purpose subagent with `subagents: []`), and `system/Agent/title-generator`.

## Inheritance

`extends` recursively resolves the parent, including its own ancestors. WARP copies the resolved parent as the merge base and replaces its metadata, namespace, and directory with the child's. The merge rules are:

1. Nonempty child `models`, `thinking`, and `triggers` replace the parent's values; a specified `temperature`, including `0.0`, replaces the parent's. Empty lists or maps for these override fields do not clear inherited values.
2. Child `tools`, `skills`, `commands`, `subagents`, and `rules` are appended to the parent lists. If a parent list is omitted and the child specifies one, the child list becomes the effective list; otherwise an empty child list does not clear the parent list. Referenced resources are resolved after merging.
3. Tool policy actions `dangerous` and `openWorld` override their parent values when specified. `include` and `exclude` lists are combined as a set union; exclusion remains effective even if a name is included.
4. Parent instructions precede child instructions, separated by a blank line when both are present. Circular chains, including self-extension, are rejected by WARP resolution.

An inherited wildcard remains in an appended list and therefore continues to select all effective resources. Use a qualified parent when extending an agent with the same short name, to avoid resolving the child itself.

## Tool access and policies

Agents declare their eager tools with the top-level `spec.tools` field (`*`, `!pattern`, or explicit tool names; `[]` when omitted). WARP resolves the agent's tools from `spec.tools`, then filters them with the merged `spec.policies.tools`: a nonempty `include` must match, `exclude` takes precedence, and `dangerous: deny` or `openWorld: deny` filters tools with the corresponding annotations. TaskSmith uses the resulting tool names as an explicit allowlist when constructing the session's tool container. The `readOnly` and `ask` policy actions are evaluated as permission ceilings during execution, alongside workspace permissions and sandbox checks.

**Eager vs. deferred loading:** tools listed in `spec.tools` load eagerly on the first request (`DeferLoading: false`). Policy-allowed tools not listed in `spec.tools` — including custom `*.tool.md` tools and `*.mcp.yaml` MCP tools, which defer by default — load as deferred tools (`DeferLoading: true`), withheld from the initial model context and discoverable via the built-in `tool_search` tool. `tool_search` is automatically activated as an eager tool whenever the session resolves at least one deferred tool, and is not registered when there are none.

**Eager tool threshold:** TaskSmith always respects `spec.tools`, but when an agent resolves to more than 20 eager tools (16 built-in baseline tools plus a 4-tool buffer), it emits a workspace and session warning diagnostic (`too_many_eager_tools`) advising the user to reduce `spec.tools` and rely on deferred loading.

**Unbypassable policy ceiling:** `policies.tools` is the unbypassable security policy ceiling for eager tools, deferred tools (`tool_search`), and skill/command tools. An agent cannot grant itself any tool outside this ceiling; when deferred tools exist, `tool_search` is added to the agent's `include` ceiling so the permission gate permits it.

**Fail-closed allowlisting:** the resolved tool list is an explicit allowlist. If resolution returns no tools (for example `include` matches nothing, or `exclude`, `dangerous`, or `openWorld` filter everything out), the agent receives zero tools rather than all tools. This makes an agent policy that filters out every tool a valid deny-all configuration. Workspace permissions and sandboxing are separate, additional protections.

## User override example

A user-scoped `main` in `$XDG_CONFIG_HOME/tasksmith/agents/agents/` (normally `~/.config/tasksmith/agents/agents/`) can extend the built-in main agent without replacing its instructions. The user definition wins over the system definition for the short name `main`, while the qualified `extends` selects the system parent directly. The example skill names must exist in the visible resource scopes to resolve.

```markdown
---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: main
  description: My customised main agent.
spec:
  extends: system/Agent/main
  skills: [go, git]
---
## Additional guidance
Always prefer table-driven tests in Go.
```

The resulting persona contains the system instructions followed by the additional guidance. Skill lists are merged according to the inheritance rules above.

## Agent example

```markdown
---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: analyst
  description: Senior financial analysis agent
  displayName: Analyst
spec:
  models: [gpt-4o, claude-3-5-sonnet]
  temperature: 0.2
  thinking:
    type: effort
    effort: high
  skills: [finance, sql]
  commands: [generate-report]
  subagents: [system/Agent/writer]
  policies:
    tools:
      exclude: [write, edit, remove]
---
# Analyst
You are a senior financial analyst. Cite the data source before drawing a conclusion.
Never fabricate figures; say when data is unavailable.
```

The model identifiers and resource references are illustrative and must exist in the active configuration to resolve.
