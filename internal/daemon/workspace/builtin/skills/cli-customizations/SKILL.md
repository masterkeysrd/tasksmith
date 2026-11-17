---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: cli-customizations
  displayName: CLI Customizations
  description: Configure TaskSmith's interactive CLI settings such as the REPL status line composition and the color theme by editing their XDG config files or driving the /statusline view, drive the interactive /sandbox and /permissions editors, --sandbox startup flag, and ! shell mode, explain the /model picker columns, quota windows, and the /usage Quotas tab for subscription-billed providers, and document headless execution with tasksmith exec or -p including NDJSON framing, stdin control frames, slash commands without the TUI, and exit codes.
spec:
  useWhen: "The user asks to change, hide, reorder, or configure the status line, status bar segments, compact mode, colors, or theme; asks how the interactive CLI is customized; wants to inspect or change sandboxing, isolation posture, excluded commands, or permission allow/deny rules from inside the REPL via /sandbox, /permissions, --sandbox, or ! commands; asks about quota windows, subscription-billed models, the /usage Quotas tab, or what the /model picker's context, alias, and pricing/quota columns mean; or asks about running TaskSmith headlessly or in CI via tasksmith exec, -p/--print, --input-format/--output-format jsonl or json, NDJSON control frames, headless slash commands, or exec exit codes."
  keywords: [statusline, status line, status bar, segment, compact, theme, colors, cli settings, appearance, sandbox, /sandbox, permissions, /permissions, --sandbox, shell mode, ! command, excluded commands, quota, quotas, /usage, /model, subscription, rate limit, pricing, billing, exec, headless, --print, -p, one-shot, ci, automation, scripting, ndjson, jsonl, control frame, exit code, sdk, embedding]
---

# CLI Customizations

TaskSmith's interactive CLI keeps user-facing appearance settings in small JSON files under
`$XDG_CONFIG_HOME/tasksmith/` and exposes safety-relevant posture through dedicated in-app
views. Surfaces today: the REPL status line, the color theme, the `/sandbox` editor, and the
`/permissions` rule browser, plus the `/model` picker and `/usage` browser for model billing,
and the headless `exec` / `-p` runner for scripts and CI.
Prefer the in-app view when the user is inside the REPL; edit the JSON file directly when working
non-interactively or applying a workspace-wide default.

## Status line

The status line composition is configured in `$XDG_CONFIG_HOME/tasksmith/statusline.json`.
Full schema, segment IDs, ordering semantics, and the responsive hiding ladder live in
`references/statusline.md` — read it before editing the file.

Quick rules:

- `segments` is an ordered list of enabled segment IDs; unknown, duplicated, or empty values are
  dropped on load and the built-in default composition is used instead.
- The first enabled segment renders on the left, the rest on the right.
- `"compact": true` switches context/model segments to short forms.
- The permission mode badge is never hidden by responsive shrinking; never remove it from a
  user's config unless they explicitly ask.
- Inside the REPL, `/statusline` opens an interactive editor with a live preview; it saves to the
  same file. Changes made by editing the file directly take effect after restarting the REPL.

## Theme

Colors come from `$XDG_CONFIG_HOME/tasksmith/theme.json`. It is optional; when absent, the
built-in theme applies. Only edit it when the user explicitly asks about colors or theming, and
never invent color keys — mirror the structure of an existing theme file.

## Model billing: /model picker and quota windows

The `/model` picker groups models by provider and shows three columns: the label, a capacity
cell (`"400.0k ctx · @fast, @default"` — context window and typed aliases), and a billing cell
(`"quota"` for subscription-billed providers, `"$3/$15 /M"` for per-token pricing, blank when
unknown). A muted footer under the list describes the highlighted model, and filtering matches
labels, aliases, ids, provider names, and billing cells.

For subscription-billed models the status line's `usage` segment switches to a quota badge
(`94% (↻ 3h12m) · 12.3k tok` — see `references/statusline.md`), turn and exit summaries omit
dollar cost, and `/usage` gains a fourth tab, `4 Quotas`, listing one bar per quota window with
percent remaining (gauged red under 20%, yellow under 50%, green from 50% up), a reset countdown,
and shared-pool grouping. Quota readings update live from
daemon events; `/usage quotas` also accepts the tab as an argument. Never claim a dollar cost for
a quota-billed call, and never invent a reset time the provider did not report.

## Sandbox & permissions

`/sandbox` opens a three-tab editor over the workspace's isolation posture (`1 Policy`,
`2 Exclusions`, `3 Effective`); `/permissions` (aliases `/perm`, `/perms`) browses and edits the
project and global allow/deny rule lists. `--sandbox native|local` selects the kernel backend for
sessions the client creates, and `!command` on an empty composer line runs a shell command
directly through the daemon. Full keybindings, scope semantics, and when to prefer the view over
editing `sandbox.json` by hand live in `references/sandbox-and-permissions.md` — read it before
driving either view.

Quick rules:

- Sandbox edits save per scope (Project → `$XDG_DATA_HOME/tasksmith/workspaces/<hash>/sandbox.json`,
  Global → `~/.config/tasksmith/sandbox.json`). Saving through `/sandbox` live-refreshes open
  sessions' runtime sandbox policies (filesystem paths, network posture and proxy domain lists,
  `allowUnsandboxed`, `autoAllowBash`, and `excludedCommands`), whereas hand-edits on disk need
  `/workspace reload`. Only the `mode` backend (`native` vs `local`) is fixed at session creation
  and applies to sessions created afterwards.
- Never edit `sandbox.json` or `permissions.json` with file tools: both stores are write-denied
  to tools by design. Drive the views or hand the user the exact JSON snippet instead.
- The permission mode badge in the status line is safety-relevant; pair posture changes with it
  when explaining what will and won't prompt.

## Headless execution (`exec` / `-p`)

`tasksmith exec "prompt"` (or the root shorthand `tasksmith -p`) runs one headless turn and
exits; `--input-format jsonl` turns stdin into a stream of NDJSON control frames for
scripted bidirectional sessions, and `--output-format json|jsonl` frames results for
machines. Full flag table, control-frame schema, headless slash commands, and exit codes
live in `references/headless-exec.md` — read it before writing an automation snippet.

Quick rules:

- Never run headless from an interactive TTY without a prompt: that is a usage error (exit `2`).
- A pending permission or question cannot be answered in `text` input mode; the run exits `4`
  (`needs_input`) rather than hanging. Recommend `--input-format jsonl` for anything that may
  touch tools under `--mode ask`.
- With `--output-format jsonl`, stdout carries frames only; never interleave prose with it.

## Etiquette

- These files are user preferences, not project files: do not create them inside a repository or
  commit them to a workspace unless the user explicitly wants a checked-in example.
- After editing either file, tell the user a REPL restart (or `/statusline` save) is needed for
  the change to show.
- If `statusline.json` fails to parse, the CLI keeps the defaults and reports the problem in the
  status area; fix the JSON rather than deleting the file.
