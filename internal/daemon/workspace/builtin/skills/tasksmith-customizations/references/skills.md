# Skills

A Skill packages specialized instructions and optional supporting files. Its metadata tells an agent what expertise is available; its body is loaded when the skill is invoked, rather than all skill bodies being placed in the prompt up front.

## Discovery and use

Skills are normally stored at `.agents/skills/<name>/SKILL.md` in a workspace or project, or under `$XDG_CONFIG_HOME/tasksmith/agents/skills/` for a user. They follow the resource hierarchy and can also be provided by the system or workspace plugins. Supporting references and scripts live alongside `SKILL.md`; they are not automatically included in the instructions.

The main agent receives metadata for its resolved skills. `useWhen` and `keywords` help the model decide when a skill is relevant; they are not a deterministic activation engine. The `load_skill` tool accepts an accessible skill name and arguments, renders the instructions, and returns both the text and the skill directory for reading supporting files. It rejects skills outside the agent's resolved set.

A user can also invoke a skill with `/name` and arguments. A matching Command is resolved first, so it takes precedence over a Skill with the same slash name. The rendered skill instructions are then included in the user message. Slash invocation uses workspace lookup, whereas `load_skill` checks the agent's resolved skill set.

## Definition

A Skill has YAML frontmatter and a Markdown body. The body becomes `instructions` automatically and is rendered when invoked. A `spec` block is optional: a skill can consist of metadata and instructions alone.

| Field | Type | Meaning |
| --- | --- | --- |
| `metadata.name`, `description` | string | Identity and short description used for discovery. |
| `spec.useWhen` | string | Guidance to the model about when the skill is relevant. |
| `spec.keywords` | string[] | Semantic tags for discovery and prompting. |
| `spec.tools` | string[] | Tool or Toolkit references associated with the skill. Declaring them does not itself grant tool access; agent policies and permissions still apply. |
| `spec.argumentHint` | string | Single-line usage hint shown by the CLI. |
| `spec.arguments` | object[] | Structured parameters available to the instruction template. |

### Arguments and rendering

| Argument field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Parameter identifier used in the instructions. |
| `description` | string | Guidance about the parameter. |
| `required` | bool | A nonempty value is required after applying the default. |
| `default` | string | Fallback when no nonempty value is supplied. |

A named argument such as `table` is available as `$table`, `${table}`, or `{{.table}}` in the body. Positional values can also be referenced as `$1`, `$2`, and so on. The renderer interprets these as template expressions; use `$$` for a literal dollar sign. Required arguments without a value or default cause rendering to fail; the CLI also validates required slash arguments before sending them.

Slash arguments can be positional in declaration order or named as `table=users` or `--table=users`. Named values take precedence over the corresponding positional value. The daemon splits slash input on whitespace, so do not assume shell-style quoting preserves multiword values. The `load_skill` tool instead accepts an argument map keyed by name.

## Example

```markdown
---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: database-migrations
  description: Safe database migration practices
spec:
  useWhen: Creating or reviewing database migrations
  keywords: [database, migration]
  argumentHint: table name
  arguments:
    - name: table
      description: Target table
      default: users
---
# Database migrations
Inspect existing migrations for `$table` and test upgrade and rollback paths.
```
