---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: tasksmith-customizations
  displayName: TaskSmith Customizations
  description: Author and configure TaskSmith WARP workspace resources (WORKSPACE.md, AGENTS.md, Rule, Skill, Agent, Command, Decision, Tool, MCP, ModelProvider, Plugin) and daemon isolation/policy files (sandbox.json and permissions.json).
spec:
  useWhen: "Creating, editing, or troubleshooting *.rule.md, SKILL.md, *.agent.md, *.command.md, *.decision.md, *.tool.md, MCP servers, *.provider.yaml, plugins, WORKSPACE.md, AGENTS.md, model tiers (@fast/@balanced/@smart/@flagship), sandbox path/network/excludedCommands rules, or permission allow/deny policies."
  keywords: [rule, skill, agent, subagent, command, decision, tool, mcp, provider, plugin, workspace.md, agents.md, sandbox.json, permissions.json, allowwrite, denywrite, denyread, allowread, excludedcommands, "@fast", "@balanced", "@smart", "@flagship", "@inherit", modelaliases, spec.enabled, provider-aliases]
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

TaskSmith searches upward from the starting directory for `WORKSPACE.md`; without one it creates a synthetic workspace. The workspace maps projects and their `AGENTS.md` context. Resource directories are scanned recursively. In a flat workspace whose only project is `.`, the root `.agents/` belongs to the project instead of being loaded twice as workspace and project. Declared workspace plugins can also contribute definitions. A nested directory with its own `.agents/` may be loaded as the current project.

A qualified reference has the form `namespace/Kind/name`, for example `system/Agent/main`; it selects that namespace directly. `local/Kind/name` refers to the current project, with workspace fallback. Project namespaces use project slugs. Short names resolve among visible scopes according to the hierarchy, so a project can override a system skill without changing the system definition. Relative-path fallback and explicit validation have additional limitations; see `references/discovery-and-validation.md`.

Workspace built-ins are mirrored under `$XDG_DATA_HOME/tasksmith/builtin/` (normally `~/.local/share/tasksmith/builtin/`); embedded resources remain available if the mirror cannot be used. Supporting files live alongside the skill and are read relative to the directory returned by `load_skill`.

## Choosing an authoring scope

Tier order decides which definition wins; it does not decide where a new resource belongs. Project scope is committed and shared with everyone who clones the repository, user scope is private to one machine and applies to every project, and workspace scope sits between them for a multi-project workspace. When the requesting user has not named a scope, ask before writing: sharing an internal endpoint, credential reference, or personal workflow with a repository is a product decision, not a default.

Never move a credential out of another tool's store, such as a CLI keyring, into a manifest, `.env`, or any other plaintext file without explicit approval. Reference it through a `spec.auth` expression (`${env:<VAR>}` or `${file:<PATH>}`) and let the user supply the value.

## Progressive disclosure

Every token in always-on context is paid on every turn and competes for model attention. Design workspace customizations so information enters context only at the granularity a task actually needs:

| Layer | Context cost | What belongs here |
| --- | --- | --- |
| `AGENTS.md`, `WORKSPACE.md` body, `triggers.always: true` rules | Paid on **every turn** | Repository-wide commands (especially non-obvious flags/codegen), cross-package architecture & data/control flow, implicit conventions, and unbreakable invariants. Omit surface details obvious from `ls` or reading one file. |
| Conditional rules (`rules/*.rule.md` with `globs`, `tools`, `keywords`, `useWhen`) | Loaded **only when matched** | Path-, language-, or tool-specific guardrails (e.g. SQL migrations, `docs/**/*.md` portability, `api/**` codegen). Prefer conditional triggers over growing `AGENTS.md` or `triggers.always`. |
| Skills (`skills/<name>/SKILL.md` + `references/`, `scripts/`) | Metadata only until `load_skill` / `/name` | Specialized domain workflows and runbooks. Keep `SKILL.md` as a concise workflow map and split deep schemas, tables, and examples into `references/*.md` read on demand. |

When authoring or refactoring instructions, link to existing repository docs with relative Markdown links instead of duplicating prose, and ground every documented command or convention in verified repository files rather than assumptions.

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
| Custom tools | `references/tools.md` | `tools/<name>.tool.md` |
| Tools, MCP | `references/tools-and-mcp.md` | `tools/`, `mcps/` |
| Model providers | `references/providers.md` | `providers/<name>.provider.yaml` |
| Plugins | `references/workspace-and-context.md` | Repository root `PLUGIN.md` or `PLUGIN.yaml` |
| Workspace and project context | `references/workspace-and-context.md` | `WORKSPACE.md`, `AGENTS.md` |
| AGENTS.md authoring | `references/agents-md.md` | Repository root or project `AGENTS.md` |
| Sandbox & permissions configuration | `references/sandbox-and-permissions.md` | `sandbox.json`, `permissions.json`, `WORKSPACE.md` `spec.policies.tools` |

Discovery is not the same as execution: TaskSmith resolves and injects Rule resources and connects the MCP servers its definitions declare, but it does not currently evaluate Decision resources. See the references for the actual runtime behavior and schemas.
