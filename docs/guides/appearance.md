# Personalize your terminal

Choose a color scheme and put the session information you rely on where
you can see it. These preferences belong to your account under `~/.config/tasksmith/` (or `$XDG_CONFIG_HOME/tasksmith/`), not the repository.

## Switch built-in themes with `/theme`

Open the picker, filter by name and select a theme:

```text
/theme
```

Or try a known name immediately in the running session:

```text
/theme github-dark
```

| Start with | If you want |
| --- | --- |
| `dark` or `light` | A baseline for your terminal background |
| `github-dark` or `tokyo-night` | An alternative dark scheme |
| `catppuccin`, `gruvbox` or `solarized` | One of these familiar palettes |

For a predictable startup choice, set `theme.json`. Without a recognized
selection TaskSmith falls back to `dark`.

## Create a custom theme (`theme.json`)

There are two files: **the scheme** defines colors; **the preference**
selects it at startup. Create the directory for schemes first:

```sh
mkdir -p "${XDG_CONFIG_HOME:-$HOME/.config}/tasksmith/themes"
```

Save this as `themes/harbor.json` under that configuration directory:

```json
{
  "name": "harbor",
  "extends": "dark",
  "type": "dark",
  "palette": {
    "sea": "#83caff",
    "mint": "#8ee6ba",
    "coral": "#ffa89d"
  },
  "colors": {
    "text": { "accent": "sea" },
    "border": { "focus": "sea" },
    "syntax": { "keyword": "sea", "string": "mint" },
    "diff": { "added_text": "mint", "removed_text": "coral" }
  }
}
```

`palette` names are yours to choose. Under `colors`, use **existing semantic
tokens**, not arbitrary names. Here `mint` colors both string literals and
added diff text; it doesn't change unrelated success badges. Extending
`dark` or `light` fills in every role you haven't customized. Don't create a
cycle between parent themes.

Activate your custom theme in a running session with `/theme`:

```text
/theme harbor
```

For startup defaults, `~/.config/tasksmith/theme.json` selects a **built-in**
theme name (unknown or custom names fall back to `dark`) and controls transparency:

```json
{
  "theme": "dark",
  "transparent": false
}
```

Set `transparent` to `true` to show the terminal's background through canvas,
panel, hover and active surfaces; code blocks and selections retain their
contrasting fills.

## Customize the status line (`/statusline` and `statusline.json`)

Open the live editor:

```text
/statusline
```

By default the first segment in each row sits on the left. The others are
grouped on the right. Think of the rows as two different questions:

| Row | What to look for |
| --- | --- |
| 1: How is this turn running? | Shortcut hint on the left; `[mode]  Model (provider)  effort  warnings` on the right. Warnings hide at zero. |
| 2: How much have I used? | Context window usage on the left; session cost (or subscription quota) and input/output tokens on the right. |

There is **no `git` segment**. To check the branch and working tree,
run `git status --short --branch` in the project rather than adding an
unknown segment to the JSON.

| To… | Press |
| --- | --- |
| Select a segment | `↑` / `↓` or `k` / `j` |
| Enable or disable it | `Space`, `Enter` or `t` |
| Reorder within its row | `←` / `→` or `h` / `l` |
| Move to the other row | `r` |
| Preview compact labels | `c` |
| Save the live layout / discard preview | `s` / `Esc` |

Keep `mode` visible so you can see when edits are accepted automatically.
At least one segment must remain enabled. For the default arrangement with
shorter labels, use this `statusline.json`:

```json
{
  "rows": [
    ["hint", "mode", "model", "effort", "warnings"],
    ["context", "cost", "tokens"]
  ],
  "compact": true
}
```

Direct edits take effect on the next REPL start. Unknown or duplicated
IDs and extra rows are dropped and reported; if none remain, the default
layout is used. If JSON parsing fails, correct the file instead of deleting
it. `segments` and the old `usage` ID are not replacements for `rows`,
`cost` and `tokens`.

## Check terminal rendering

- **Colors look too similar?** Check truecolor support in the terminal and
  any multiplexer before changing hex values. Compare a syntax-highlighted
  code block and a diff; a readable accent alone doesn't guarantee contrast.
- **Symbols appear as boxes?** Use a font with Unicode coverage. `↑` and `↓`
  distinguish input (including cached input) from output; `∞` denotes an
  unlimited quota, not zero usage.
- **The provider name disappeared?** Resize the terminal and let the rows
  redraw. On narrow terminals the model can lose its provider suffix;
  compact mode shortens labels without moving segments between rows.
- **A warning count persists?** Open `/warnings` for the active problem and
  suggested fix. Clearing old scrollback doesn't resolve provider or MCP health.

## Next steps

- [Look up the appearance configuration files](../reference/cli/config-files.md).
- [Use the interactive CLI](./cli.md).
- [Interpret pricing and quota windows](./models-and-providers.md).
