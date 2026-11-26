# Run and create team slash commands

Turn a repeated request into a named workflow. Start with a built-in
command in the composer (or pass it as a single argument to `tasksmith exec`), then give your team its own prompt, inputs, and execution context.

## Use built-in workflow commands

| Command | Use it to… | Where it runs |
| --- | --- | --- |
| `/init` | Create or update `AGENTS.md` from repository evidence. | In the current conversation. |
| `/grill-me [topic]` | Stress-test a design in a structured, read-only interview. | In the current conversation. |
| `/review [target]` | Get findings on local changes or a particular diff. | In a clean `code-reviewer` child session. |

### Capture repository conventions

```text
/init
```

The agent inspects the repository and existing instructions before writing.
In a monorepo it can ask whether to target the root, a project, or both.
An empty or boilerplate-only directory is not enough evidence: add real
source files before running it again.

### Test an idea before implementing it

```text
/grill-me Should offline imports resume after a partial failure?
```

The interview examines the relevant code and asks you about choices the
repository can't settle. With no argument it uses a clear topic already in
the conversation, or asks for one. The outcome stays in the conversation:
it doesn't create a plan file, change mode or start implementation.

### Request an independent review

```text
/review
```

Omit the target for local changes. To review the last commit instead, run:

```sh
tasksmith exec '/review HEAD~1..HEAD'
```

In the TUI, `tasksmith exec`, or an ACP client, `/review` resolves the same
workspace command. It delegates to a read-only `code-reviewer`, embeds the
`code-review` skill and returns findings to the parent. The clients display
results differently; you do not need separate review templates for them.

## Create a custom slash command (`*.command.md`)

Suppose your team asks for the same compatibility checks before a release.
Create the project directory:

```sh
mkdir -p .agents/commands
```

Save `.agents/commands/contract-check.command.md`:

```markdown
---
apiVersion: warp/v1alpha1
kind: Command
metadata:
  name: contract-check
  displayName: Check a public contract
  description: Review a change for consumers that rely on the existing behavior.
spec:
  agent: code-reviewer
  context: clean
  argumentHint: "[revision range or scope]"
  skills: [code-review]
---
Compare the public contract before and after the change.
{{with .RawArguments}}Focus on: {{.}}{{else}}Inspect local changes.{{end}}
For each incompatible change, identify an affected caller and cite the diff.
Separate confirmed regressions from missing evidence. Do not edit files.
```

Try both paths through the template:

```text
/contract-check HEAD~1..HEAD
```

```text
/contract-check
```

`metadata.name` supplies the slash name. The body is a **prompt template**,
not shell code. The optional `argumentHint` tells users what to type; it
does not validate input. The embedded skill supplies the review procedure
on this invocation without requiring a separate `load_skill` call.

### Choose free-form or structured inputs

`{{.RawArguments}}` preserves the argument text received after the name,
trimmed at both ends. Use it for a question, topic or target description.
For fixed slots, declare named arguments instead. For example, a benchmark
command could add the following fields under `spec`:

```yaml
argumentHint: "<package_path> <function_name>"
arguments:
  - name: package_path
    required: true
  - name: function_name
    required: true
```

Use `{{.package_path}}` and `{{.function_name}}` in its body: ask the agent
to locate the function, add a `Benchmark` test, run it with `-benchmem` and
report `ns/op` and `allocs/op`. A contributor could then type:

```text
/benchmark ./internal/daemon/permissions MatchRule
```

Arguments are positional in declaration order or named (`function_name=MatchRule`).
Named values win; a missing required value without a default fails before
the workflow runs. Don't rely on shell quoting for multiword slots. `$1`,
`$name` and `${name}` are shorthand; use `$$` for a literal dollar sign.

## Choose execution context: inline, fork or clean

| `spec.context` | What the workflow sees | Choose it for |
| --- | --- | --- |
| `inline` (default) | The current conversation; no separate child by default. | A design interview that needs your answers. |
| `fork` | A child seeded with a snapshot of the parent conversation. | A second opinion that needs the discussion so far. |
| `clean` | A child without the parent's conversation history. | An independent review with an explicit target. |

`spec.agent` naming a different persona delegates even without an explicit
child context. A clean conversation is **not** an isolated Git worktree or
a grant of different filesystem permissions.

`spec.models` can temporarily select a model for an inline command; the
session model is restored afterward, including on failure or cancellation.
`spec.skills` and `spec.rules` embed instructions. `spec.tools` can activate
normally deferred tools, but cannot bypass workspace or agent tool policy.

## Override built-in commands

Choose which audience should see the replacement:

- **One project:** `.agents/commands/review.command.md`.
- **One account:** `~/.config/tasksmith/agents/commands/review.command.md`.
- **Multiple projects:** the workspace root's `.agents/commands/`.

Use the same `metadata.name` and `kind: Command`. To keep review isolated,
retain `agent: code-reviewer` and `context: clean` in the replacement.
If only the *review procedure* needs changing, override the `code-review`
skill instead; delegated reviews will benefit too.

For plugin commands, use `/<plugin-name>/<command> [args...]`. A Command
wins over a Skill with the same slash name. Avoid collisions with CLI
controls such as `/model`; give a new team workflow a distinctive name.

## Next steps

- [Package reusable instructions as skills](./skills-and-rules.md).
- [Create a custom delegated persona](./agents-and-subagents.md).
- [Check all Command fields and template variables](../reference/manifests/command.md).
- [Look up interactive slash commands](../reference/cli/slash-commands.md).
