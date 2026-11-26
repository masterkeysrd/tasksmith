# Command Manifest (`*.command.md`)

Defines reusable command prompt templates invokable via `/name` or agent delegation. Placed under `.agents/commands/<name>.command.md`.

## Example

`/refactor` requires `target_file`; omitting `focus` renders the prompt with `readability`. The `inline` mode keeps the current conversation history.

```markdown
---
apiVersion: warp/v1alpha1
kind: Command
metadata:
  name: refactor
  displayName: Refactor Module
  description: Refactors a module for improved readability and modularity
spec:
  agent: main
  context: inline
  models:
    - openai/gpt-5.5
  skills:
    - go-refactoring
  rules:
    - coding-standards
  tools:
    - view
    - edit
    - write
    - bash
  argumentHint: "Usage: /refactor <target_file> [focus]"
  arguments:
    - name: target_file
      description: Source file to refactor
      required: true
    - name: focus
      description: Refactoring focus area (e.g. modularity, concurrency)
      required: false
      default: "readability"
---

Please analyze the file {{.target_file}} focusing on {{.focus}}:
1. Identify code duplication, high cyclomatic complexity, or violation of single-responsibility.
2. Formulate a refactoring plan and present a unified diff using `edit`.
3. Verify that all tests pass using `bash(go test ./...)`.
```

## Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `agent` | string | Preferred or required agent persona to execute this command. Spawns a delegated subagent session when different from the active session agent. |
| `context` | string | Conversational execution mode: `inline` (default, runs in the current session), `clean` (runs in a subagent session with empty history), or `fork` (runs in a subagent session with a snapshot of the current history). |
| `models` | array of strings | Prioritized model identifiers or aliases for executing this command. Overrides the active model for the duration of the command turn and restores the previous model afterward. |
| `skills` | array of strings | Skill references or globs eagerly embedded into context when executing this command. |
| `rules` | array of strings | Rule references or globs eagerly loaded (`always: true`) when executing this command. |
| `tools` | array of strings | Tool references or globs required by this command. Deferred tools in this list (and in `spec.skills`) are automatically activated into context, subject to workspace and agent tool policy ceilings. |
| `argumentHint` | string | Usage signature hint displayed during autocomplete and validation. |
| `arguments` | array of objects | Structured parameter declarations accepted by this command. |

## Argument Object Reference

Every parameter declared in `spec.arguments` supports:

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `name` | string | **Yes** | Identifier used for template variable substitution (`{{.name}}` or `$name`). |
| `description` | string | No | Guidance describing the purpose and expected format of the argument. |
| `required` | boolean | No | When `true`, invocation fails validation if the argument is omitted and has no default. |
| `default` | string | No | Fallback value automatically injected by the renderer when omitted. |

*The Markdown body below the frontmatter contains the execution instructions. In a slash invocation, `{{.RawArguments}}` holds the argument text as received by the daemon after the command name and surrounding whitespace are removed. Use it for free-form input instead of a structured argument definition.*

## Built-in System Commands

TaskSmith embeds system-tier command manifests directly within the binary. These commands are universally available across all workspaces unless shadowed by a higher-priority definition:

- **`system/Command/init` (`/init`)**: Built-in command that inspects the workspace codebase (directory layout, existing agent/editor rule files, build/CI configs, and representative source/test files) and interactively generates or updates `AGENTS.md`. Employs `inline` context, activates the `tasksmith-customizations` skill, and requests safe read/inspection tools (`ls`, `view`, `glob`, `grep`), interactive input (`ask_question`), and file modification tools (`write`, `edit`).
- **`system/Command/review` (`/review [target]`)**: Runs `code-reviewer` with clean history and embeds the `code-review` skill. Assign `@code-reviewer` to set its preferred model. See [Code reviews](../../guides/commands.md#request-an-independent-review).
- **`system/Command/grill-me` (`/grill-me [idea, plan, or problem]`)**: Inline design interview that uses repository inspection and `ask_question` rounds to settle choices without writing files. See [Design Interviews](../../guides/commands.md#test-an-idea-before-implementing-it).

## See Also

- [Commands](../../guides/commands.md)
- [Manifest hierarchy and metadata](../manifests.md)
