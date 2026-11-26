# Skill Manifest (`*.skill.md` or `SKILL.md`)

Bundles domain expertise, procedural instructions, and reference patterns. Placed under `.agents/skills/<name>.skill.md` or `.agents/skills/<name>/SKILL.md`.

## Example

The migration skill renders the index statement for the supplied `table_name`, or for `users` when omitted. Its tool list requests tools for this workflow without granting permission to run them.

```markdown
---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: postgres-migrations
  displayName: PostgreSQL Migrations
  description: Guidelines for authoring zero-downtime database migrations
spec:
  useWhen: "Authoring or modifying database schemas, table definitions, or SQL migrations."
  keywords:
    - postgres
    - sql
    - migration
    - schema
  argumentHint: "Usage: /postgres-migrations [table_name]"
  arguments:
    - name: table_name
      description: Target database table being modified
      required: false
      default: "users"
  tools:
    - view
    - bash
    - grep
---

# PostgreSQL Migration Rules

1. Always add new columns as `NULL` or provide a default with `DEFAULT ... NOT NULL` on PostgreSQL 11+.
2. Create indexes concurrently: `CREATE INDEX CONCURRENTLY idx_{{.table_name}}_email ON {{.table_name}}(email);`.
3. Never drop columns in the same release that removes the application code reading them.
```

## Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `useWhen` | string | Natural language condition explaining when agents should load this skill. |
| `keywords` | array of strings | List of keywords that trigger skill discovery. |
| `argumentHint` | string | Usage signature hint displayed during autocomplete and validation. |
| `arguments` | array of objects | Structured parameter declarations accepted by this skill. |
| `tools` | array of strings | Tool references or glob patterns required or used by this skill. Deferred tools in this list are automatically activated into context when the skill is loaded, subject to workspace and agent tool policy ceilings. |

*The Markdown body below the frontmatter contains the domain instructions.*

## Argument fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `name` | string | **Yes** | Identifier used for template variable substitution (`{{.name}}` or `$name`). |
| `description` | string | No | Guidance describing the purpose and expected format of the argument. |
| `required` | boolean | No | When `true`, invocation fails validation if the argument is omitted and has no default. |
| `default` | string | No | Fallback value automatically injected by the renderer when omitted. |

## See Also

- [Skills and rules](../../guides/skills-and-rules.md)
- [Manifest hierarchy and metadata](../manifests.md)
