# Run TaskSmith in scripts and CI

Use `tasksmith exec` for a single answer, a machine-readable review, or a conversation driven over standard input.
Your script uses the same daemon, workspace, sessions, permissions, and sandbox as the interactive terminal.

## Before you start

Install TaskSmith on the runner and configure a provider the daemon can use without an
interactive login. On a development machine, follow the [quickstart](./quickstart.md) first.
Run from the intended project or select it with `-w`. The client starts the daemon if needed,
including when invoked through SSH or inside a container. The extraction examples below use `jq`.

Keep credentials outside the checkout. For PRs from untrusted contributors, don't expose
secrets or inherit permission grants meant for trusted project scripts.

## Run a one-shot prompt

Use `exec` for explicit automation. Root `-p` (`--print`) is its one-shot shorthand:

```bash
tasksmith exec "List the top-level components and their responsibilities."
tasksmith -p "Summarize this project in two sentences."
```

With a prompt argument, piped stdin is **context prepended to that prompt**:

```bash
git diff | tasksmith -p "Review this diff for bugs; cite changed lines."
```

Without an argument, piped stdin **is the prompt**:

```bash
printf '%s\n' 'Explain the build system.' | tasksmith exec
```

File, rule, skill, and MCP mentions are expanded in prompt arguments, not in piped text.
This keeps a literal `@name` in logs or diffs from turning into an attachment. On an interactive
terminal with neither prompt nor pipe, text input is a usage error (exit `2`), not a waiting chat.

## Choose an output format

| Format | Choose it when you want… | What arrives on stdout |
| --- | --- | --- |
| `text` (default) | A human-readable answer | Streamed assistant text or `!command` output. |
| `json` | One result to parse after exit | A single object with session ID, status, text, and available usage/changes. |
| `jsonl` | Live events and tool progress | Newline-delimited JSON (NDJSON), one complete frame per line. |

Diagnostics go to stderr. Don't merge stderr into stdout if a parser is reading the result.
Changing **output** format does not make stdin interactive.

To watch just text deltas on a terminal with Bash and `jq`:

```bash
set -o pipefail
tasksmith exec --output-format jsonl 'Explain @README.md' |
  jq --unbuffered -rj 'select(.type=="chunk") | .payload.chunk.content[]? | select(.type=="text") | .text'
```

Use the terminal `result` frame rather than reconstructing a final answer from chunks.
`entry_added` carries persisted entries; tool activity and child sessions can interleave with text.

## Handle interactive prompts safely in CI

With `text` input, nobody can answer a permission request, structured question, or plan review.
Instead of hanging, TaskSmith cancels the in-flight turn, returns `needs_input`, and exits `4`.
Handle this separately from a provider or usage error. This shell fragment also works under `set -e`:

```bash
rc=0
tasksmith exec --output-format json 'Review the README for stale examples.' > result.json || rc=$?
case "$rc" in
  0) jq -r '.text' result.json ;;
  4) printf '%s\n' 'Human input required; inspect the run before retrying.' >&2; exit 4 ;;
  *) printf 'TaskSmith exited %s\n' "$rc" >&2; exit "$rc" ;;
esac
```

For a repeatable job, pre-allow only the specific operations the job needs. Select `--mode`
according to the task: `ask` for an explanation, or `accept_edits` for routine workspace file
edits. Neither guarantees a prompt-free run. Don't blanket-approve unknown commands to avoid code `4`.
If a human or policy service must answer, keep stdin open as described below.

## Drive multi-turn runs with bidirectional NDJSON

Set **both** formats to `jsonl`. Send one JSON object followed by a newline for each action.
For a smoke test with no tool approvals, send two client-local queries and close:

```bash
tasksmith exec --input-format jsonl --output-format jsonl <<'FRAMES'
{"type":"send","content":"/model"}
{"type":"send","content":"/mode"}
{"type":"close"}
FRAMES
```

Look for a `session` header, `command_result` frames for the queries, and a final `result`.
To run a model turn, send a normal prompt. A live driver should:

1. Start the subprocess with separate stdin, stdout, and stderr. Save the `session_id`
   from its header. Keep stdin open and flush after each frame.
2. Send the first prompt; read stdout continuously. Events include the root session
   **and its children**. Use their `session_id` to keep conversations separate.
3. On `permission_requested`, present the action to the decision maker. On `question_requested`
   or `plan_review_requested`, collect the answer or plan decision instead.
4. Copy the request's `id` into `request_id` and its `tool_call_id` into the matching reply.
   With these identifiers, exec routes replies to the owning session automatically.
5. After `completed`, send a follow-up prompt or `close` and drain stdout to the terminal result.
   A closed input stream can't answer a later request; a fixed here-document isn't a live driver.

For example, these are **separate writes** in a live exchange, not a batch of unconditional decisions:

```jsonl
{"type":"send","content":"Compare the parser implementations without editing files."}
{"type":"permission","permission":{"request_id":"REQUEST_ID","tool_call_id":"TOOL_CALL_ID","kind":"reject","feedback":"Use only the files already available."}}
{"type":"send","content":"Which implementation has the smaller memory footprint?"}
{"type":"close"}
```

Only send the second line if a matching request actually arrives, substituting its real IDs.
Use `config` to change session settings between turns. `cancel` interrupts the addressed turn
without closing the session; it does not stop independent subagents. Cap unattended conversations
with `--max-turns N`. For a design interview such as `/grill-me`, answer each question frame.

Local commands include model and agent selection, thinking controls, usage, and `/help`.
`/plan` can submit a turn; `/compact` requests daemon compaction. Workspace commands, skills,
MCP prompts, and `!` shell input go to the daemon. An unknown bare `/name` fails instead of
silently becoming prose. Use `tasksmith auth login` outside exec for interactive sign-in.

## Practical CI recipes

### Review a pull request without posting comments

Fetch the base and head revisions in the job before running the review. In this example,
`origin/main` is the PR's target branch and `HEAD` is its checked-out head:

```bash
tasksmith exec --ephemeral --output-format json '/review origin/main...HEAD' > review.json
```

The reviewer starts with clean history, inspects the comparison read-only, and returns
verified findings with line citations. A successful exit means the review ran, **not**
that it found no bugs. Use `/review HEAD~1..HEAD` for a single commit, or `/review PR 42`
with network access and an authorized `gh` for a private PR.

### Extract a report from the result

```bash
jq -r '.text' review.json
jq '{session_id, status, tokens: .usage.totals.tokens, changes}' review.json
```

Usage may be absent. If you need costs, `cost_nano` is nano-USD, not dollars.
Keep provider diagnostics separate from artifacts intended for a JSON consumer.

### Keep or discard the session deliberately

```bash
tasksmith -p --ephemeral --mode ask 'Explain how tests are organized.'
```

`--ephemeral` deletes the session after the run settles; it does **not** undo edits to the
working tree. Leave it off when you want a follow-up: pass the saved ID to `--session`,
add `--fork` to preserve the original conversation, or use `-c` in the same workspace.

Building a client instead? Use Go's public `client.ConnectOrStart` for HTTP + SSE or drive
the subprocess using `api.NewExecFrameReader` and `api.WriteNDJSON`. Both use the same daemon.

## Next steps

- [Look up all exec flags, frames, events, and exit codes](../reference/exec.md).
- [Configure permission rules for your job](./permissions.md).
- [Continue or fork a saved session](./sessions.md).
- [Use the daemon API directly](../reference/daemon-api.md).
