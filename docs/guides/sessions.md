# Continue a session and manage its work

Pick up a conversation where you left off, try a different approach without changing its history,
and inspect the files, processes, and usage associated with the work. The daemon persists session
journals across terminal restarts automatically.

## Resume a previous session

For the workspace's most recently updated **top-level** session (including forks), run:

```bash
tasksmith -c
```

`--continue` is the long form. If there isn't a previous session, this opens a fresh one.
It works with `exec` and `-p` too:

```bash
tasksmith -p --continue "Summarize what remains to be tested."
```

To resume a *particular* session, use the command from the goodbye block, for example:

```bash
tasksmith --session 018f3a52-9b24-7800-842a-594247e62a1c
```

Use your own ID. The flag is `--session` (no `-s` shorthand); it takes precedence over `-c`.
Inside the terminal UI, `/sessions` lets you find a conversation without knowing the ID:

| Key | Action |
| --- | --- |
| `Enter` | Open the selected session. |
| `F2` | Rename the session so it's easier to find next time. |
| `F3` | Fork the selected session. |
| `F4` | Delete the selected session. Don't delete one you want to resume. |

`/sessions` also accepts an ID. To start with no previous conversation instead, use
`/new` (optionally naming a model) or `/clear`.

## Branch work with `/fork`

Fork when you want to reuse the investigation but explore a different solution.
Wait for the current turn to finish, then enter:

```text
/fork Try a streaming parser
```

You switch to a new top-level session with the source conversation. Subsequent turns
in the fork don't append to the original. Omit the title for the derived fork name.
A conversation fork is not a Git branch or a separate working directory.

From the shell, resolve the source **before** forking:

```bash
tasksmith -c --fork --title 'Streaming parser'
```

`--fork` without a resolvable source is a usage error. You can also use `--session`
with `exec --fork`; a bidirectional exec stream follows a `/fork` to the new session.

## Inspect file changes and revert

Type `/changes` to list tracked files created, modified, or deleted during the session.
Start typing a filename to filter the list, then:

1. Select a path and press `Enter` for its colored unified diff.
2. Check the additions and deletions against the change you meant to make.
3. Press `Esc` to return. To restore just that file, press `r` and confirm the path.

The baseline is the file state before the session's tracked changes, **not necessarily Git HEAD**.
Prefer the browser if you want a confirmation step. The direct command acts on the scope you specify:

| Command in the composer | What it restores |
| --- | --- |
| `/revert README.md` | That file's baseline snapshot. |
| `/revert README.md ENTRY_ID` | The file snapshot at that tracked change entry; substitute the actual entry ID. |
| `/revert` | The session's tracked file changes across the workspace. |

Restoring files does **not** rewind the conversation. Tell the agent which change you
reverted before continuing. Start a new conversation instead if you want it to approach the task afresh.

## Manage context with `/compact`

Use `/context` to see the token grid and which categories occupy the context window.
When a long investigation crowds out the implementation discussion, compact with a focus:

```text
/compact Keep the parser requirements, rejected alternatives, and failing test names.
```

Without a focus, `/compact` requests a general summary. This frees space for the next
turn; it doesn't revert file edits. Resolve pending permissions and questions first:
commands and background notifications remain queued while an interaction is outstanding.
If compaction fails, the existing conversation is preserved rather than replaced with a partial summary.

## Monitor background tasks (`/tasks`)

A badge near the composer means the daemon is still running work such as a dev server.
Open `/tasks` to find out what is running before starting a second copy.

| Key | In the task monitor |
| --- | --- |
| `Enter` | Follow the selected task's live, auto-scrolling output (last 50 lines). |
| `a` | Toggle running tasks versus all session tasks, including completed and canceled ones. |
| `x` | Stop the selected running or pending task. |
| `Esc` | Leave the view without stopping the process. |

A delegated **agent** is a child session, not a shell process. The activity bar shows
its persona, task title, resolved model (not the requested alias), activity, and elapsed time.
For example, `Reading files` can change to `Generating` without interrupting your draft.

Open `/subagents` (or `/subagent`), toggle completed children with `a`, and press `Enter`
to inspect a live transcript. Scroll with arrows, `j`/`k`, `Ctrl+P`/`Ctrl+N`, page keys,
`Ctrl+U`/`Ctrl+D` or `Ctrl+B`/`Ctrl+F`; `g`/`G` jumps to either end. `x` stops the child.
A waiting-child alert offers `Ctrl+G` to inspect and answer, or `Ctrl+Y` to allow once.

## Track token usage, cost, and speed

### Read a turn summary

While the spinner runs, `↑` counts tokens sent and `↓` counts tokens received across
**all model calls in this turn**. Counts start at zero, updating during a call if the
provider reports live usage and otherwise at completion. Context size is a different number.
Elapsed time includes tools, compaction, and time spent waiting for approval.

A delegated command with clean or forked context, or a different agent, names the child
in the spinner. `Esc` cancels that command. Autonomous subagents and background tasks
keep running unless you stop them in their own browsers.

```text
Smithed for 42s · ↑12.4k ↓1.8k · $0.03 · done 2:15 PM
```

The verb varies. Completed turns under five seconds omit the summary; failures and
cancellations say `Failed after` or `Stopped after`. Stored summaries return when you reopen the session.

### Compare spending and quotas

`/usage` covers chat, compaction, titles, failed and canceled calls, tools, and tracked file changes.
Use `1`–`4` or the horizontal arrows for Session, Workspace, Global, and Quotas.
Open a particular tab directly, for example:

```text
/usage quotas
```

Workspace and Global can group by model (`m`), provider (`p`), agent (`a`), purpose (`u`),
pricing tier (`i`), or day (`d`); `t` cycles 24 hours, 7 days, 30 days, and all time.
Look at cache misses, API versus wall duration, and tool errors, denials, cancellations,
slowest calls, and lines changed. Insights appear only when the underlying data warrants one.

- **Metered models:** uncached input, cache reads, and cache writes don't overlap. Prices
  are fixed at call time; `≥` indicates a lower bound because some calls lack pricing.
- **Subscriptions:** shared quota buckets group models by provider and account, mark the current
  model, and show remaining capacity, reset information and usage counts when available.
  Quota calls aren't counted as unpriced. Usage records survive session deletion.

### Measure model speed

`/stats` counts chat calls only, excluding title generation and compaction. Compare uncached
prompt tokens per processing second with output tokens per generation second; also check
average and worst time to first token and end-to-end latency. `—` means not measured.

`~` marks inferred speeds: OpenAI-compatible timings can use first-token time and the
remaining duration as estimates; Ollama reports the split directly. Averages include only
calls with timing data. The session tab refreshes every 15 seconds; `r` rescans Workspace
or Global. `t` cycles today (local midnight), 7 days, 30 days, and all time.

When you exit, the goodbye block pairs the resume command with changed lines and files,
elapsed time, token totals (including cache), and cost or quota. `—` means no pricing;
subscription models show remaining quota and reset when known. Piped output and `NO_COLOR` stay plain.

## Next steps

- [Plan the next change](./modes-and-planning.md) before editing.
- [Drive a session from a script](./headless-exec.md).
- [Look up session commands and usage views](../reference/cli/slash-commands.md).
