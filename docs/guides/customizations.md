# Choose how to customize TaskSmith

Start with what you want to change: what the agent knows, what it can do,
which model it uses, or what you see in the terminal. Then choose who should
receive that change: just you, everyone in a workspace, or one project.

You don't need a plugin or a workspace manifest to teach the agent about a
single repository — run `/init` to create or update its `AGENTS.md` from evidence in the project.

## Compare extension types

Paths below are conventional locations relative to the project root unless
marked otherwise. Markdown resources have YAML frontmatter and an instruction
body; `AGENTS.md` can be plain Markdown.

| Choose this | When you want to… | Where it lives |
| --- | --- | --- |
| Project context | Give every session the same build command or architectural invariant. | `AGENTS.md` |
| Rule | Apply a constraint when the agent touches particular paths or tools. | `.agents/rules/*.rule.md` |
| Skill | Teach a procedure the agent can load only when needed. | `.agents/skills/<name>/SKILL.md` or `.agents/skills/*.skill.md` |
| Slash command | Give people a short, repeatable entry point to a workflow. | `.agents/commands/*.command.md` |
| Agent | Delegate to a persona with its own instructions, models and allowed tools. | `.agents/agents/*.agent.md` |
| Decision | Describe a classifier's rubric, such as the auto-mode permission reviewer. | `.agents/decisions/*.decision.md` |
| Custom tool | Let the agent call an existing executable with validated inputs. | `.agents/tools/*.tool.md` |
| MCP server | Connect a service that publishes tools, prompts or resources. | `.agents/mcps/*.mcp.yaml` |
| Model provider | Connect an account or inference endpoint and name its models. | `.agents/providers/*.provider.yaml` |
| Plugin | Distribute related resources together across workspaces. | `PLUGIN.md` or `PLUGIN.yaml` in the plugin; declare it in `WORKSPACE.md` |
| Appearance | Change colors or arrange your terminal status line. | `theme.json`, `themes/`, `statusline.json` under `$XDG_CONFIG_HOME/tasksmith/` |

**For example:** “Use our documented test command” belongs in `AGENTS.md`.
“Check the downgrade path before changing a migration” belongs in a rule.
“How to rehearse a migration” belongs in a skill. If contributors need a
one-line way to start that rehearsal, add a command that embeds the skill.

A Decision can ask a **predicate** (true/false), a **choice** among options,
or a **score** against ordered levels. A PR risk rubric might distinguish a
removed public field from an additive change. Declaring that rubric does not
make TaskSmith run it on every PR or activate a Rule trigger.

The shipped `permission-reviewer` is a specific runtime consumer: in auto
mode it uses a `verdict` choice and a confidence threshold. See the
[Decision reference](../reference/manifests/decision.md) before overriding it.

## Where customizations live

TaskSmith looks upward for `WORKSPACE.md`. Without one, it supplies a
synthetic workspace. Pick the scope by audience, not by file format:

| Audience | Resource directory | Example |
| --- | --- | --- |
| Only you, across repositories | `$XDG_CONFIG_HOME/tasksmith/agents/` (usually `~/.config/tasksmith/agents/`) | A personal review command |
| Every project in a workspace | `<workspace-root>/.agents/` | A shared provider or release playbook |
| One project | `<project>/.agents/` | A migration rule specific to one service |

For the same kind and short name, project wins over workspace, then user,
then system, then plugin resources. In a single-project workspace rooted at
`.`, the root `.agents/` is the project directory; it is not loaded twice.
Directories are scanned recursively, so `.agents/defs/` can hold agents too.

Use a short name like `code-review` to pick up local overrides. Use a
qualified name such as `system/Agent/main` when you specifically mean the
shipped definition. `local/Kind/name` looks in the current project with
workspace fallback.

Keep secrets out of shared manifests. Point at the credential instead, for
example `${env:TEAM_API_KEY}` where the provider expects a token. Keep
personal terminal preferences outside `.agents/`; they configure the client,
not the workspace the daemon serves.

## Scaffold customizations with built-in skills

Ask the active agent to load the appropriate skill. Say who the change is
for, what should trigger it, and what evidence would show that it works.
There is no need to switch agents.

**For workspace resources**, try this in the composer:

```text
Use tasksmith-customizations to add a project rule for database migrations.
Match migrations/**. Require a rollback plan; don't make it always-on.
```

**For the terminal**, ask for the setting or interactive control you need:

```text
Use cli-customizations to show me how to keep the mode badge visible
but shorten the context and token counters in my terminal.
```

The built-in skills are mirrored under `$XDG_DATA_HOME/tasksmith/builtin/`
(normally `~/.local/share/tasksmith/builtin/`). If the mirror can't be
written, embedded definitions still work, but supporting files may be
unavailable. Put overrides in your own scope rather than editing the mirror.

## Next steps

- [Connect a provider and route work to models](./models-and-providers.md).
- [Delegate work or create a custom agent](./agents-and-subagents.md).
- [Teach project conventions with rules and skills](./skills-and-rules.md).
- [Run or create a slash command](./commands.md).
- [Add a tool or connect an MCP server](./tools-and-mcp.md).
- [Personalize the theme and status line](./appearance.md).
- [Look up manifest formats, scopes and discovery](../reference/manifests.md).
