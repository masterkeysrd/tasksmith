# Commands

A Command packages reusable instructions for a discrete operation. Its Markdown body is a prompt template, not a shell script; invoking it does not itself execute an operating-system command.

## Discovery and invocation

Command manifests are commonly stored at `.agents/commands/<name>.command.md` in a workspace or project, or under `$XDG_CONFIG_HOME/tasksmith/agents/commands/` for a user. They follow the resource hierarchy and can also come from system resources or workspace plugins. The declared `metadata.name`, not the filename, is the slash-command name.

TaskSmith exposes workspace Commands as `/name` entries in the CLI. For a slash input, the daemon looks up a Command before a Skill with the same name, validates it against the active agent, renders its instructions with the arguments, and runs the result either in the current conversation (inline) or in a spawned subagent session (`context: clean`/`fork`, or a `spec.agent` override). Slash lookup uses the workspace, not the selected agent's resolved command list. CLI built-in slash commands are separate; registering a resource with the same name can replace a CLI registry entry, so avoid name collisions.

## Definition

A Markdown manifest has WARP YAML frontmatter followed by the instruction body. The body automatically becomes `instructions`; `spec` is optional.

| Field | Type | WARP meaning |
| --- | --- | --- |
| `spec.agent` | string | Preferred or required Agent reference. Spawns a delegated subagent session when it names a different agent than the active session. |
| `spec.context` | string | Conversation mode: `inline` (default), `fork`, or `clean`. |
| `spec.models` | string[] | Prioritized model identifiers or aliases that override the active model for the duration of the command turn. |
| `spec.skills` | string[] | Skill references or globs eagerly embedded into context when the command runs. |
| `spec.rules` | string[] | Rule references or globs eagerly loaded (`always: true`) when the command runs. |
| `spec.tools` | string[] | Tool references or globs required by the command; deferred tools are activated into context. |
| `spec.argumentHint` | string | Single-line usage hint shown by the CLI. |
| `spec.arguments` | object[] | Structured argument definitions. |

**TaskSmith runtime:** Slash-command execution validates the command against the active agent's `spec.commands` (unless `spec.agent` names a different agent, which bypasses the enablement check) and resolves its `spec.skills`, `spec.rules`, and `spec.tools` against the workspace and agent tool policy ceilings.

- **Inline execution** (`context: inline`, or an empty context with no `spec.agent` override) runs the command in the current session. When `spec.models` resolves to a model that differs from the session's active model, the turn temporarily uses that model and restores the previous model when the turn finishes, including on error or cancellation.
- **Subagent execution** (`context: clean`, `context: fork`, or a `spec.agent` targeting a different agent) spawns a child subagent session to run the command. `clean` starts with an empty conversation history; `fork` seeds the child with a snapshot of the parent's current conversation messages before starting the turn. The child notifies the parent on completion via a `<subagent_completed ...>` message.
- **Eager embedding and tool activation:** declared `spec.skills` are rendered and embedded as `<embedded_skill ...>` blocks, and declared `spec.rules` are rendered and embedded as `<embedded_rule ...>` blocks, both prepended to the command's prompt text. Deferred tools from `spec.tools` and from the declared skills' `spec.tools` are activated into context via tool-reference blocks, bounded by `workspace.spec.policies.tools` and `agent.spec.policies.tools`.

The body is a Go template. It can reference a named argument as `$ticker`, `${ticker}`, or `{{.ticker}}`, and positional values as `$1`, `$2`, and so on. Command execution also exposes `{{.Agent.*}}` (the executing agent's `Name`, `Description`, `Dir`, `Path`) and `{{.Command.*}}` (the command's `Name`, `Dir`, `Path`, `Tools`, `Skills`, `Rules`, `Context`, `Agent`) alongside `{{.Workspace.*}}` and `{{.Project.*}}`. WARP interprets dollar shorthand; use `$$` for a literal dollar sign.

## Arguments and templates

Each argument has a `name`, optional `description`, boolean `required`, and string `default`. A nonempty default can satisfy a required argument; otherwise a missing required value causes rendering to fail. The CLI also validates required slash arguments before sending them.

Arguments are positional in declaration order or named, for example `ticker=EXAMPLE` or `--ticker=EXAMPLE`; a named value takes precedence over the corresponding positional value. Slash input is split on whitespace by the daemon, so shell-style quoting does not reliably preserve multiword values.

The body can reference a named argument as `$ticker`, `${ticker}`, or `{{.ticker}}`, and positional values as `$1`, `$2`, and so on. WARP renders the body as a Go template and interprets dollar shorthand; use `$$` for a literal dollar sign. Workspace and project template context is also available.

## Example

```markdown
---
apiVersion: warp/v1alpha1
kind: Command
metadata:
  name: generate-report
  description: Produce a structured research report
  displayName: Generate Report
spec:
  argumentHint: <ticker> [year]
  arguments:
    - name: ticker
      description: Stock ticker symbol
      required: true
    - name: year
      description: Reporting year
      default: "2026"
---
# Generate Report
Prepare a research report for $ticker covering $year. Cite sources, distinguish facts from estimates, and note missing data.
```

For this definition, `/generate-report EXAMPLE year=2026` supplies the ticker and year to the template.
