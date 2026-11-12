---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: cli-customizations
  displayName: CLI Customizations
  description: Configure TaskSmith's interactive CLI settings such as the REPL status line composition and the color theme by editing their XDG config files or driving the /statusline view.
spec:
  useWhen: "The user asks to change, hide, reorder, or configure the status line, status bar segments, compact mode, colors, or theme, or asks how the interactive CLI is customized."
  keywords: [statusline, status line, status bar, segment, compact, theme, colors, cli settings, appearance]
---

# CLI Customizations

TaskSmith's interactive CLI keeps user-facing appearance settings in small JSON files under
`$XDG_CONFIG_HOME/tasksmith/`. Two surfaces exist today: the REPL status line and the color
theme. Prefer the in-app view when the user is inside the REPL; edit the JSON file directly when
working non-interactively or applying a workspace-wide default.

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

## Etiquette

- These files are user preferences, not project files: do not create them inside a repository or
  commit them to a workspace unless the user explicitly wants a checked-in example.
- After editing either file, tell the user a REPL restart (or `/statusline` save) is needed for
  the change to show.
- If `statusline.json` fails to parse, the CLI keeps the defaults and reports the problem in the
  status area; fix the JSON rather than deleting the file.
