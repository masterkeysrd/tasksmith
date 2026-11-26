# Resource Manifest Reference

TaskSmith loads declarative WARP resources from Markdown frontmatter or YAML. Use the kind and discovery filename below to find its fields, defaults, examples and runtime behavior.

## Resource references

Paths under `.agents/` are relative to a workspace or project. The same resource layout can be used under the user resource directory.

| Kind | Conventional location | Reference |
| --- | --- | --- |
| `Workspace` | `WORKSPACE.md` | [Projects, defaults, plugin imports and policies](./manifests/workspace.md) |
| `Context` | `<project>/AGENTS.md` | [Project identity and instructions](./manifests/workspace.md#project-context-manifest-agentsmd) |
| `Agent` | `.agents/agents/*.agent.md` | [Personas, model selection and delegation](./manifests/agent.md) |
| `Skill` | `.agents/skills/*.skill.md` or `.agents/skills/<name>/SKILL.md` | [On-demand instructions and arguments](./manifests/skill.md) |
| `Rule` | `.agents/rules/*.rule.md` | [Triggers, priority and rule injection](./manifests/rule.md) |
| `Command` | `.agents/commands/*.command.md` | [Slash commands and argument templates](./manifests/command.md) |
| `Decision` | `.agents/decisions/*.decision.md` | [Predicates, choices and scoring rubrics](./manifests/decision.md) |
| `Tool` | `.agents/tools/*.tool.md` | [Executable tools, schemas and argument delivery](./manifests/tool.md) |
| `MCP` | `.agents/mcps/*.mcp.yaml` | [Server transports, authentication and filters](./manifests/mcp.md) |
| `ModelProvider` | `.agents/providers/*.provider.yaml` | [Providers, models, pricing and aliases](./providers.md) |
| `Plugin` | `<plugin-root>/PLUGIN.md` or `PLUGIN.yaml` | [Exports, imports and `warp.lock`](./manifests/plugin.md) |

## Common metadata envelope

| Field | Type | Meaning |
| --- | --- | --- |
| `apiVersion` | string | `warp/v1alpha1`. |
| `kind` | string | Resource kind from the routing table, such as `Agent`. |
| `metadata` | object | Identity shared by all kinds. |
| `metadata.name` | string | Required identifier, unique within the resource kind and namespace. |
| `metadata.displayName` | string | Optional human-readable title displayed in interactive pickers. |
| `metadata.description` | string | Optional short summary of what the resource does. |
| `metadata.labels` | map of strings to strings | Optional key-value pairs for categorization. |
| `spec` | object | Kind-specific settings. For instruction-bearing Markdown resources the body supplies `spec.instructions`. |

```yaml
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: reviewer                 # Unique resource identifier within the namespace (required)
  displayName: Code Reviewer     # Human-readable title for UI and pickers
  description: Pull request reviewer for Go and TypeScript
  labels:
    tier: production
    category: engineering
```

| File form | Where the envelope goes |
| --- | --- |
| Markdown | Between opening and closing `---` lines. The body below the closing delimiter is instruction text, not YAML. |
| YAML | At the document root; no Markdown body or frontmatter delimiters are needed. |
| Special root files | `WORKSPACE.md`, `AGENTS.md`, and `PLUGIN.md` can omit frontmatter. Use explicit metadata when a stable identity is important. |

## Discovery paths and override precedence

TaskSmith looks upward from the starting directory for `WORKSPACE.md`. If none is valid, it creates a synthetic workspace at the starting directory. The four standard tiers resolve short names in this order; plugin namespaces are a fallback.

| Priority (highest first) | Scope | Discovery path |
| --- | --- | --- |
| 1 | Current project | `<project>/AGENTS.md` and `<project>/.agents/`. The project slug is its namespace; `local` selects the active project. |
| 2 | Workspace | `<workspace-root>/WORKSPACE.md` and `<workspace-root>/.agents/`. |
| 3 | User | `$XDG_CONFIG_HOME/tasksmith/agents/` (normally `~/.config/tasksmith/agents/`) and `~/.agents/`. The XDG directory wins for the same kind and name. |
| 4 | System | Embedded in the binary. Built-ins are mirrored under `$XDG_DATA_HOME/tasksmith/builtin/`. |
| 5 | Plugins | Exported resources selected by `WORKSPACE.md` imports, under each plugin's namespace. |

| Lookup | Result |
| --- | --- |
| Short name, e.g. `reviewer` | Highest visible definition of that kind and name; a project override does not modify the system definition. |
| `namespace/Kind/name` | Explicit namespace, e.g. `system/Agent/main`. Use this when extending a resource with the same short name. |
| `local/Kind/name` | Active project with workspace fallback. |
| Single project `.` | The root `.agents/` belongs to the project and is not loaded a second time as workspace-global. |
| Resource directory | Scanned recursively. A nested working directory with its own `.agents/` can also become the active project. |

A higher-priority manifest changes which definition is selected, not what permissions it has. For example, a registered built-in tool still wins over a same-named custom executable.

## Model provider settings

| Setting | Reference and constraints |
| --- | --- |
| Availability and aliases | [Provider reference](./providers.md) covers `spec.enabled` and 1-to-1 `@alias` editing. Workspace `spec.modelAliases` takes precedence over per-model `aliases`. |
| Usage estimates | Model `cost` and tiers (`label`, `tier.type`, `tier.size`) override built-in pricing for LLM-call estimates shown by `/usage` and the status line. Without configured or profile pricing a model is recorded as unpriced. |
| [Codex](./providers/codex.md) | Use `codex` for provider name and type, with `spec.auth.type: oauth2`. |
| [Claude Code](./providers/claudecode.md) and [GitHub Copilot](./providers/github-copilot.md) | Use `claudecode` or `github-copilot` for both `metadata.name` and `spec.type`. Use `spec.auth.type: oauth2` alone; omit `spec.endpoint`, `spec.headers`, and `spec.config`. Configure completion models, token limits and aliases here, not vendor CLI settings, credentials or per-token prices. |

## Related references

| Topic | Reference |
| --- | --- |
| Workspace setup | [Workspaces guide](../guides/workspaces.md) |
| Execution restrictions | [Permissions guide](../guides/permissions.md) |
| Tool and MCP configuration | [Tools and MCP guide](../guides/tools-and-mcp.md) |
| <span id="5-rule-manifest-rulemd"></span>Rule attachment | [Rule manifest](./manifests/rule.md) |
| <span id="9-tool-manifest-toolmd"></span>Custom tool contract | [Tool manifest](./manifests/tool.md) |
| <span id="command-templating"></span>Command templating | [Template variables and argument delivery](./manifests/tool.md#command-templating) |
| <span id="10-mcp-server-manifest-mcpyaml"></span>MCP server contract | [MCP manifest](./manifests/mcp.md) |
