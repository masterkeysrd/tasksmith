# Agents

An Agent defines an actor's persona, model preferences, and available resources. TaskSmith selects an agent for a session and combines its rendered instructions with workspace instructions. Agent manifests are commonly stored in `.agents/agents/<name>.agent.md` and discovered in system, user, workspace, and current project scopes. The workspace can select a default with `spec.defaultAgent`; short and qualified names follow the resource hierarchy.

## Definition

An Agent uses WARP YAML frontmatter and a Markdown body. The body supplies `instructions` automatically; write the persona below the closing `---`, not as a frontmatter field. The body is rendered as a template for the session. The built-in main agent's template includes project context, but custom agents do not automatically receive it; see `workspace-and-context.md`.

All `spec` fields are optional:

| Field | Type | Meaning |
| --- | --- | --- |
| `extends` | string | Parent Agent short name or qualified reference such as `system/Agent/main`. |
| `triggers` | string[] | Intended invocation sources, such as `human`, `agent`, or `system`; an empty list means unrestricted in WARP. These are not natural-language trigger phrases. |
| `models` | string[] | Prioritized model identifiers; an unspecified or empty list leaves model selection to defaults. |
| `temperature` | float | Sampling temperature, conventionally `0.0`–`2.0`; when omitted it is unset rather than an explicit `0.0`. |
| `thinking` | object | Extended-reasoning configuration, for example `type: effort` and `effort: high`. |
| `skills` | string[] | Skill names, qualified references, paths, or patterns to resolve. |
| `commands` | string[] | Command names, qualified references, paths, or patterns to resolve. |
| `subagents` | string[] | Agent names, qualified references, paths, or patterns to resolve for delegation. |
| `rules` | string[] | WARP Rule references or patterns; TaskSmith does not currently activate Rule resources. |
| `policies` | object | Security and access settings, including tool restrictions under `policies.tools`. |

When a session has no explicit model, TaskSmith passes the agent's model list to model resolution and uses its first result; if nothing resolves, it falls back to the first listed identifier. An explicit session model takes precedence.

For skills, commands, subagents, and rules, an omitted list or `["*"]` selects all effective resources in WARP; an explicit `[]` selects none. Resolution also skips references that cannot be found. These are resolved-resource semantics, not a guarantee that every runtime path enforces every selector. In particular, slash-command lookup uses the workspace's command and skill lookup, rather than the resolved agent command list.

WARP filters resolved subagents to those whose `triggers` are empty or include `agent`. This is a resolution filter, not general enforcement of every invocation source by TaskSmith.

## Inheritance

`extends` recursively resolves the parent, including its own ancestors. WARP copies the resolved parent as the merge base and replaces its metadata, namespace, and directory with the child's. The merge rules are:

1. Nonempty child `models`, `thinking`, and `triggers` replace the parent's values; a specified `temperature`, including `0.0`, replaces the parent's. Empty lists or maps for these override fields do not clear inherited values.
2. Child `skills`, `commands`, `subagents`, and `rules` are appended to the parent lists. If a parent list is omitted and the child specifies one, the child list becomes the effective list; otherwise an empty child list does not clear the parent list. Referenced resources are resolved after merging.
3. Tool policy booleans `allowDangerous` and `allowOpenWorld` override their parent values when specified. `include` and `exclude` lists are combined as a set union; exclusion remains effective even if a name is included.
4. Parent instructions precede child instructions, separated by a blank line when both are present. Circular chains, including self-extension, are rejected by WARP resolution.

An inherited wildcard remains in an appended list and therefore continues to select all effective resources. Use a qualified parent when extending an agent with the same short name, to avoid resolving the child itself.

## Tool access and policies

There is no top-level Agent `spec.tools` field. WARP gathers effective tools and toolkit entries, then filters them with the merged `spec.policies.tools`: a nonempty `include` must match, `exclude` takes precedence, and `allowDangerous: false` or `allowOpenWorld: false` filters tools with the corresponding annotations. TaskSmith uses the resulting tool names when constructing the session's tool container. Execution is also subject to workspace permissions and sandbox checks.

**Current limitation:** if resolution returns no tools, the resulting list can be nil and TaskSmith treats a nil allowed-tool list as unrestricted during tool registration. Do not rely on an agent policy that filters out every tool as a deny-all security boundary. Workspace permissions and sandboxing are separate protections.

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
