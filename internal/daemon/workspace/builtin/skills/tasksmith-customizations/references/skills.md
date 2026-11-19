# Skills

A Skill packages specialized instructions and optional supporting files. Its metadata tells an agent what expertise is available; its body is loaded when the skill is invoked, rather than all skill bodies being placed in the prompt up front.

## Discovery and use

Skills are normally stored at `.agents/skills/<name>/SKILL.md` in a workspace or project, or under `$XDG_CONFIG_HOME/tasksmith/agents/skills/` for a user. They follow the resource hierarchy and can also be provided by the system or workspace plugins. Supporting references and scripts live alongside `SKILL.md`; they are not automatically included in the instructions.

The main agent receives `<available_skills>` metadata (`name`, `description`, `useWhen`, and argument declarations) for its resolved skills; `keywords` is kept for catalog/UI metadata and is not rendered into the model prompt. `description` and `useWhen` help the model decide when a skill is relevant; they are not a deterministic activation engine. The `load_skill` tool accepts an accessible skill name and arguments, renders the instructions, and returns both the text and the skill directory for reading supporting files. It rejects skills outside the agent's resolved set.

A user can also invoke a skill with `/name` and arguments. A matching Command is resolved first, so it takes precedence over a Skill with the same slash name. The rendered skill instructions are then included in the user message. Slash invocation uses workspace lookup, whereas `load_skill` checks the agent's resolved skill set.

## Structuring skills for progressive disclosure

Skills give workspaces a two-stage progressive-disclosure mechanism:

1. **Before invocation (`description` + `useWhen` in `<available_skills>`)**: Only `metadata.name`, `metadata.description`, `spec.useWhen`, and argument metadata are rendered into the system prompt on every turn. Keep both fields concise (1–2 sentences each) with **complementary, non-duplicative roles** — neither vague nor a 150-word flag dump:
   - **`metadata.description`** (*what it covers*): Name the concrete domain, files, or surfaces inside the skill once so both `/skills` and the model see its scope (e.g., *"Author PostgreSQL schema migrations, concurrent indexes, and safe column rollouts."*).
   - **`spec.useWhen`** (*when to load it*): State the user intents, tasks, or file patterns that require calling `load_skill`, without repeating `description` word-for-word (e.g., *"Creating, reviewing, or troubleshooting SQL migration files or table schema changes."*).
2. **After invocation (`SKILL.md` front page → `references/*.md`)**: Treat `SKILL.md` as a compact front page (~40–80 lines) holding only core mental models, non-negotiable guardrails, and a routing table to sibling `references/*.md` files (or `scripts/`). Place detailed schemas, flag/keybinding tables, protocol frames, and lengthy examples inside `references/*.md` so the agent reads only the specific reference file its current task needs.

## Definition

A Skill has YAML frontmatter and a Markdown body. The body becomes `instructions` automatically and is rendered when invoked. A `spec` block is optional: a skill can consist of metadata and instructions alone.

| Field | Type | Meaning |
| --- | --- | --- |
| `metadata.name`, `description` | string | Identity and short description used for discovery. |
| `spec.useWhen` | string | Guidance to the model about when the skill is relevant. |
| `spec.keywords` | string[] | Semantic tags for discovery and prompting. |
| `spec.tools` | string[] | Tool references or glob patterns required or used by this skill. When the skill is loaded via `load_skill` or `/name`, deferred tools in this list are automatically activated into the model's context via tool references without a separate `tool_search` call; already-eager tools need no activation. Declaring a tool does not bypass policy: `workspace.spec.policies.tools` and `agent.spec.policies.tools` remain the strict security ceiling, so a tool denied by policy is never activated. |
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

Beyond declared arguments, the invoking context is available to the template: `{{.Agent.Name}}` and `{{.Agent.Description}}` describe the active agent, and `{{.Workspace.Dir}}` and `{{.Project.Dir}}` expose the workspace and project root directories.

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
