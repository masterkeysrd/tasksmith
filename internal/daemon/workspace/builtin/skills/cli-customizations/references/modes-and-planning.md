# Session Modes & Planning in the CLI

TaskSmith separates **permission gates** (what auto-runs vs. prompts) from **behavioral posture**
(whether the agent acts, answers questions, or drafts a plan).

## Modes (`default`, `accept_edits`, `plan`, `ask`, `auto`)

| Mode | Permission Gate | Behavioral Posture |
| --- | --- | --- |
| `default` | Safe read-only tools (`view`, `ls`, `glob`, `grep`, `ask_question`, `todos`, read-only `bash`) auto-run; writes, mutating commands, network fetches, and outside reads prompt. | Act normally. |
| `accept_edits` | Same read baseline as `default`, plus safe `write`/`edit`/`remove` operations auto-run; shell and network still prompt. | Act normally. |
| `plan` | Same permission gate as `default`. Writes, edits, and removals inside `<session-dir>/plans/` always auto-allow. | Investigate and clarify ambiguities via `ask_question`, ask where to store the plan (`<session-dir>/plans/` vs. workspace), write the plan, and submit via `exit_plan_mode`. |
| `ask` | Same permission gate as `default`. | Answer the user's questions without modifying code or files. |
| `auto` | All allowed tools auto-run except explicit `deny` rules, workspace/agent policy ceilings, `bypass_sandbox` retries, and unlisted proxy hosts. | Act normally. |

In **restricted (untrusted)** repositories, sessions start in `default` mode while the daemon
enforces read-only protection on the repository (mutating tools are denied and the kernel sandbox is
read-only), keeping `<session-dir>/plans/` writable until the workspace is trusted.

## Switching modes & `/plan [prompt]`

- **`Shift+Tab`** (empty composer): cycles `default → accept_edits → plan → ask → auto → default`.
- **`/mode [mode]`**: opens the interactive mode picker (with no argument) or switches directly to
  `default`, `accept_edits`, `plan`, `ask`, or `auto`.
- **`/plan [prompt]`**: switches to `plan` mode immediately; when `<prompt>` is provided, also
  submits `<prompt>` as a planning turn in `plan` mode. (Supported in both the REPL and
  `tasksmith exec`.)
- **`e`** (**Yes, and switch to accept_edits**): approves the selected write and switches the current
  session to `accept_edits` without creating a rule. Available in the main session's permission
  prompt for writes not classified as unsafe, excluding explicit sandbox-bypass retries. Hidden in
  `accept_edits` and `auto` and in subagent prompts.
- **`/permissions`** (`/perm`, `/perms`): manage persistent project and global allow/deny rules.

Widening the mode rechecks pending approvals in the session and active subagents. Only pending requests
that the new mode allows and that are not classified as unsafe are approved once; no requests are
rejected automatically and no rules are created. Pending explicit sandbox-bypass retries and
unlisted proxy hosts still need approval.

## Design interviews (`/grill-me [topic]`)

Use `/grill-me` to stress-test a design before `/plan`. The interview verifies repository facts, asks dependency-ordered rounds of up to five independent questions, then asks for confirmation of the decision summary. Omit the topic to use a clear subject from the conversation or be prompted for one. The interview does not write files, change modes, or implement anything; `/plan` is the separate step for drafting a plan. In `tasksmith exec`, use `--input-format jsonl` to answer the questions.

## Plan storage & `exit_plan_mode` review

- **Safe plans directory (`<session-dir>/plans/`)**: session-scoped and automatically writable in
  every mode (including restricted workspaces) without approval prompts. Writing a plan file inside
  the repository workspace uses normal permission prompts.
- **`enter_plan_mode` & `exit_plan_mode`**: top-level sessions can also enter and exit plan mode via
  tool calls. `exit_plan_mode(path)` pauses the turn and opens the compact **Plan review** prompt
  (press `p` to open and scroll the rendered Markdown plan viewer, and `p` or `Esc` to return to
  the review options) with four choices:
  1. `Approve & auto-accept edits (accept_edits)`
  2. `Approve with standard prompts (default)`
  3. `Approve autonomous (auto)`
  4. `Keep planning` (press `4` or `r` for inline revision feedback; `Esc` cancels and stays in `plan` mode)
