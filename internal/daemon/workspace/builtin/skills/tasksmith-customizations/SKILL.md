---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: tasksmith-customizations
  displayName: TaskSmith Customizations
  description: Author, configure, and maintain TaskSmith workspace resources including Rules, Skills, Agents, Commands, Decisions, Tools, MCP servers, Model Providers, Plugins, and WORKSPACE.md / AGENTS.md files.
spec:
  useWhen: "Creating, editing, or configuring TaskSmith rules (*.rule.md), skills (SKILL.md), agents (*.agent.md), commands (*.command.md), decisions (*.decision.md), custom tools (*.tool.md), MCP servers, model providers (*.provider.yaml), plugins (PLUGIN.md or PLUGIN.yaml), WORKSPACE.md, or AGENTS.md."
  keywords: [rule, rules, skill, agent, subagent, command, decision, tool, mcp, provider, plugin, workspace.md, agents.md]
---

# TaskSmith Customizations

TaskSmith discovers WARP resources that define agents, skills, commands, tools, providers, and other customization data. Their kind, name, and scope determine which definition is visible. Markdown resources use YAML frontmatter followed by an instruction body; other resources can be YAML manifests. `WORKSPACE.md` and project `AGENTS.md` have special discovery roles rather than being ordinary `.agents/` resources.

## Hierarchy and discovery

For a short name shared by the same kind, the higher visible tier wins:

| Priority | Scope | Location |
| --- | --- | --- |
| 1 (highest) | Current project | `<project>/.agents/` |
| 2 | Workspace | `<workspace-root>/.agents/` |
| 3 | User | `$XDG_CONFIG_HOME/tasksmith/agents/` (normally `~/.config/tasksmith/agents/`) |
| 4 | System | Embedded built-ins |
| 5 (lowest) | Plugin namespaces | Resources exported by declared plugins |

TaskSmith searches upward from the starting directory for `WORKSPACE.md`; without one it creates a synthetic workspace. The workspace maps projects and their `AGENTS.md` context. Resource directories are scanned recursively. In a flat workspace whose only project is `.`, the root `.agents/` belongs to the project instead of being loaded twice as workspace and project. Declared workspace plugins and resources explicitly listed by a Context can also contribute definitions. A nested directory with its own `.agents/` may be loaded as the current project.

A qualified reference has the form `namespace/Kind/name`, for example `system/Agent/main`; it selects that namespace directly. `local/Kind/name` refers to the current project, with workspace fallback. Project namespaces use project slugs. Short names resolve among visible scopes according to the hierarchy, so a project can override a system skill without changing the system definition. Relative-path fallback and explicit validation have additional limitations; see `references/discovery-and-validation.md`.

Workspace built-ins are mirrored under `$XDG_DATA_HOME/tasksmith/builtin/` (normally `~/.local/share/tasksmith/builtin/`); embedded resources remain available if the mirror cannot be used. Supporting files live alongside the skill and are read relative to the directory returned by `load_skill`.

## Resource references

| Resource | Reference | Common location beneath a resource directory |
| --- | --- | --- |
| Discovery and validation | `references/discovery-and-validation.md` | Workspace and resource directories |
| Instruction templates | `references/templating.md` | Markdown resource bodies |
| Rules | `references/rules.md` | `rules/<name>.rule.md` |
| Skills | `references/skills.md` | `skills/<name>/SKILL.md` |
| Agents | `references/agents.md` | `agents/<name>.agent.md` |
| Commands | `references/commands.md` | `commands/<name>.command.md` |
| Decisions | `references/decisions.md` | `decisions/<name>.decision.md` |
| Tools, toolkits, MCP | `references/tools-and-mcp.md` | `tools/`, `mcps/` |
| Model providers | `references/providers.md` | `providers/<name>.provider.yaml` |
| Plugins | `references/workspace-and-context.md` | Repository root `PLUGIN.md` or `PLUGIN.yaml` |
| Workspace and project context | `references/workspace-and-context.md` | `WORKSPACE.md`, `AGENTS.md` |

Discovery is not the same as execution: WARP can load Decision, Rule, and MCP definitions, but TaskSmith does not currently evaluate Decision resources, activate Rule resources, or connect MCP servers from those definitions. See the references for the actual runtime behavior and schemas.
