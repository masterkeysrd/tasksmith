# Instruction templates

WARP renders supported resource instructions with Go `text/template`, after expanding dollar shorthand. TaskSmith renders workspace and agent instructions for session prompts, and renders skills and commands when invoked. It can render Decision instructions through its workspace API, but does not currently evaluate Decision resources. A resource being discovered does not mean its instructions are automatically rendered or used.

## Syntax and arguments

| Syntax | Meaning |
| --- | --- |
| `$Name`, `${Name}`, `{{.Name}}` | Named value in the render context. |
| `$1`, `$2` | Positional arguments when supplied by the caller. |
| `$ticker` or `{{.ticker}}` | Named Skill, Command, or Decision argument. |
| `$$` | Literal dollar sign. |
| `{{range .Agent.Skills}}...{{end}}` | Go template control flow over available values. |

Use named arguments declared in `spec.arguments` for predictable Skill and Command invocation; defaults and required checks are described in [skills](skills.md) and [commands](commands.md). Available variables depend on the resource type and caller, and absent values should not be assumed to exist. Template parsing or execution can fail.

## Context values

Common metadata is available as `Name`, `DisplayName` (falling back to `Name`), `Description`, and `Labels`. Type-specific fields such as `Models`, `Skills`, `Commands`, `Subagents`, `Rules`, `Triggers`, `Tools`, `Arguments`, and `ArgumentHint` are exposed where the renderer supplies them; not every field exists for every resource.

| Object or shorthand | Available information when supplied |
| --- | --- |
| `.Workspace`, `$WorkspaceDir`, `$WorkspacePath` | Workspace directory and manifest path. |
| `.Project`, `$ProjectDir` | Project name, display name, and directory. |
| `.Context`, `$ContextPath` | Project context path and raw instructions when a context exists. |
| `.Agent` | Agent name, description, directory, path, and potentially resolved skills, tools, commands, subagents, and rules. |
| `.Skill`, `.Command`, `.Decision`, `.Rule` | Resource-specific metadata, directory, path, and relevant argument or routing fields. |

TaskSmith passes the active workspace and project to its Agent, Skill, and Command renderers. Only agent rendering supplies a resolved agent; do not assume a skill or command receives the invoking agent's resolved capabilities. Context exists only when the project has one. Template `Path` values for resource objects may be constructed from a resource name and directory rather than the original manifest filename; use the skill directory returned by `load_skill` when accessing supporting files. Workspace rendering receives workspace context but not a project from TaskSmith.

A caller can supply additional globals through render options, but they are not universally available. For example, a custom agent can explicitly include project context and list resolved skills:

```markdown
{{if .Context}}{{.Context.Instructions}}{{end}}
{{range .Agent.Skills}}
- {{.Name}}: {{.Description}}
{{end}}
```

These templates are instructions, not a mechanism to grant tool permissions or activate unsupported resource kinds.
