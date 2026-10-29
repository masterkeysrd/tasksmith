# Commands

A Command packages reusable instructions for a discrete operation. Its Markdown body is a prompt template, not a shell script; invoking it does not itself execute an operating-system command.

## Discovery and invocation

Command manifests are commonly stored at `.agents/commands/<name>.command.md` in a workspace or project, or under `$XDG_CONFIG_HOME/tasksmith/agents/commands/` for a user. They follow the resource hierarchy and can also come from system resources or workspace plugins. The declared `metadata.name`, not the filename, is the slash-command name.

TaskSmith exposes workspace Commands as `/name` entries in the CLI. For a slash input, the daemon looks up a Command before a Skill with the same name, renders its instructions with the arguments, and sends the result as a user message in the current conversation. Slash lookup uses the workspace, not the selected agent's resolved command list. CLI built-in slash commands are separate; registering a resource with the same name can replace a CLI registry entry, so avoid name collisions.

## Definition

A Markdown manifest has WARP YAML frontmatter followed by the instruction body. The body automatically becomes `instructions`; `spec` is optional.

| Field | Type | WARP meaning |
| --- | --- | --- |
| `spec.agent` | string | Preferred or required Agent reference. |
| `spec.context` | string | Conversation mode: `inline` (default), `fork`, or `clean`. |
| `spec.models` | string[] | Preferred model identifiers intended to override agent defaults. |
| `spec.tools` | string[] | Tool references intended to restrict execution. |
| `spec.argumentHint` | string | Single-line usage hint shown by the CLI. |
| `spec.arguments` | object[] | Structured argument definitions. |

**TaskSmith runtime:** Slash-command execution currently renders the body in the existing conversation. It does not switch agents or models, create a forked or clean context, or apply command-specific tool restrictions from `agent`, `context`, `models`, or `tools`. These fields are manifest metadata, not security or execution guarantees in this path. Existing session tool policies, permissions, and sandbox checks still apply.

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
