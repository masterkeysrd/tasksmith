# Teach TaskSmith your project conventions

Put repository facts where every agent sees them, apply rules when a task
needs a guardrail, and package longer procedures as skills that load on demand.
Put shared conventions in `.agents/`, or personal ones in `~/.config/tasksmith/agents/`.

## Choose between `AGENTS.md`, rules and skills

| Use | When the agent needs it | Good example |
| --- | --- | --- |
| `AGENTS.md` | On every turn in this project. | The build requires generated clients to be refreshed before tests. |
| Conditional Rule | When working with specific files, tools or topics. | A schema change must preserve a supported downgrade path. |
| Skill | When following a procedure that spans several steps. | Rehearse an upgrade, exercise it with sample data, then verify rollback. |

If removing the instruction would break almost every task, keep it in
`AGENTS.md`. If most tasks don't need it, use a trigger or an on-demand
playbook. Include non-obvious commands and invariants rather than a file
listing the agent can discover for itself.

To draft or refresh project context from the repository:

```text
/init
```

A behavioral rule isn't an access control. Use a workspace policy if an
operation must actually be blocked, not merely discouraged in instructions.

## Write ambient rules (`*.rule.md`)

Create the conventional project directory:

```sh
mkdir -p .agents/rules
```

Save `.agents/rules/migration-safety.rule.md`:

```markdown
---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: migration-safety
  description: Protects existing readers and writers during schema changes.
spec:
  priority: 10
  triggers:
    globs:
      - "migrations/**"
      - "!migrations/testdata/**"
    keywords: [migration, rollback]
    useWhen: Writing or reviewing a production migration or its downgrade path.
---
Show how old and new application versions can coexist during rollout.
Identify any step that locks a populated table or destroys data.
State what rollback restores and what it cannot restore.
```

Choose triggers by how certain you want activation to be:

| Trigger | What happens |
| --- | --- |
| `globs` | A tool touching a matching workspace-relative path loads the body. Use `!` to exclude fixtures. |
| `tools` | A matching tool call loads it, even without a matching path. For a tool-specific rule, try `tools: [write, edit]`. |
| `keywords` | A matching word or phrase in the user's prompt suggests the rule; it does not force a load. |
| `useWhen` | Describes applicability to the agent so it can request `load_rule` before acting. |
| `always: true` | Puts the body in every in-scope agent's context. Reserve this for non-negotiable common constraints. |

A tool trigger is not a qualifier on a path trigger. Adding `tools: [edit]`
to the example would make an unrelated edit sufficient to load it.
Higher `priority` orders rules first; ties break by name. Priority isn't
a way to override permissions or disable another rule.

### Check that a rule loads

1. Open `/rules`. Find the rule and inspect its scope, triggers, estimated
   tokens and status: `[Always]`, `[Loaded]` or `[Standby]`.
2. Press `i` for the full body; press `r` after an edit to hot-reload.
3. Ask the agent to inspect a file matching the glob. Look for a single
   `Loaded Rule` entry in the transcript, not the entire body.

To skip waiting for a trigger, mention the rule in the composer:

```text
@rule:migration-safety Check the proposed downgrade before implementation.
```

On an Agent manifest, `rules: ["*"]` includes conditional rules for normal
triggering. Explicitly naming a rule attaches it to that agent as always-on.
An agent can't opt out of a workspace-wide always-on rule.

## Create a skill with progressive disclosure (`<name>/SKILL.md`)

Use a single `.agents/skills/<name>.skill.md` for a short, self-contained
procedure. Use a package when readers need checklists, scripts or templates:

```sh
mkdir -p .agents/skills/migration-rehearsal/references
mkdir -p .agents/skills/migration-rehearsal/scripts
```

Save `.agents/skills/migration-rehearsal/SKILL.md`:

```markdown
---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: migration-rehearsal
  description: Rehearses a schema upgrade and rollback against a disposable database.
spec:
  useWhen: A contributor asks to test a migration before rollout.
  keywords: [migration, postgres]
  argumentHint: "[table]"
  arguments:
    - name: table
      description: Table affected by the change
      default: users
---
Identify the migration affecting {{.table}} and its matching downgrade.
Read references/rollback.md before choosing the checks to run.
Use only a disposable test database. Report the before/after row counts
and any data the downgrade cannot reconstruct.
```

`description` says **what the skill provides**; `useWhen` says **which user
intent calls for it**. Both appear in discovery. Don't depend on `keywords`
alone to explain the skill, or put the entire runbook into its description.

Add `references/rollback.md` in that package with your project's actual
checks. For instance:

```markdown
Compare the table shape and row count before upgrade and after downgrade.
Check index creation separately: PostgreSQL CREATE INDEX CONCURRENTLY
must not run inside a transaction block. Report any lock-sensitive steps.
```

Put repeatable test helpers in `scripts/` and templates in `resources/`.
Tell the agent which file to read or script to run **at the step that needs
it**. Supporting files are relative to the skill directory returned on load;
they aren't automatically injected or executed.

### Run the playbook

A contributor can invoke it directly:

```text
/migration-rehearsal accounts
```

An agent can explicitly call `load_skill` with the equivalent inputs:

```json
{
  "skill": "migration-rehearsal",
  "arguments": { "table": "accounts" }
}
```

Test with and without the argument. The bare invocation should use `users`;
a request for `accounts` shouldn't silently operate on the default table.

## Override built-in skills

Keep the same `kind: Skill` and `metadata.name`. Put the replacement in
`.agents/skills/<name>/SKILL.md` for the project or workspace; use
`~/.config/tasksmith/agents/skills/<name>/SKILL.md` for your user account.
The higher visible scope wins; it doesn't append to the shipped body.

| Override | When it pays off |
| --- | --- |
| `code-review` | Add your team's evidence or compatibility checks to both delegated reviews and `/review`. |
| `tasksmith-customizations` | Teach the agent your workspace's resource-authoring conventions. |
| `cli-customizations` | Provide account-specific guidance for interactive CLI workflows. |

Keep any still-needed supporting files beside the replacement. Don't edit
the materialized built-in copy: another workspace would see the change and
a later materialization can replace it. Exercise the override by asking for
the skill in the intended project, not merely by checking the filename.

## Next steps

- [Expose a skill through a team command](./commands.md).
- [Look up Rule activation and priority](../reference/manifests/rule.md).
- [Check Skill inputs and package layout](../reference/manifests/skill.md).
- [Place instructions in the right workspace scope](../reference/manifests/workspace.md).
