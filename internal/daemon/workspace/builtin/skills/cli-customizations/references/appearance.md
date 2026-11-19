# Status Line & Theme Appearance

## Status line (`statusline.json` & `/statusline`)

File: `$XDG_CONFIG_HOME/tasksmith/statusline.json` (created by `/statusline` save; optional).

```json
{
  "segments": ["hint", "mode", "context", "usage", "model"],
  "compact": false
}
```

### Schema

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `segments` | ordered array of segment IDs | all five, in the order above | Enabled segments. The first renders on the left of the status line, the remaining ones on the right, separated by ` · `. |
| `compact` | boolean | `false` | Shorter renderings for `context` (percentage only) and `model` (model name only). |

### Segment IDs

| ID | Renders | Example |
| --- | --- | --- |
| `hint` | keyboard hint or the Ctrl+C cancel prompt | `shift+tab cycle mode • /help commands` |
| `mode` | permission mode badge | `plan` |
| `context` | context window usage | `109.5k/400.0k (27%)`, compact: `27%` |
| `usage` | session cost with input/output tokens; for subscription-billed (quota) models, remaining capacity with a reset countdown instead of a cost | `$0.42 · ↑128.4k ↓8.3k` or `94% (↻ 3h40m) · ↑2.0M ↓4.9k` |
| `model` | model and thinking effort | `gpt-5 · high`, compact: `gpt-5` |

IDs are matched case-insensitively after trimming.

### Quota-billed models

When the active model is billed against a provider quota window rather than per token, the `usage`
segment shows remaining capacity instead of a dollar figure: `94% (↻ 3h40m)` (or `∞` when unlimited).
The countdown ticks locally between daemon pushes, and the badge is colored amber under 20%
remaining and red at empty. Until the first reading arrives, only the token counters render.

### Validation & responsive hiding ladder

- Unknown IDs are dropped and reported; known IDs keep their given order; duplicates collapse to their first occurrence.
- An empty or invalid `segments` list falls back to the built-in default order; malformed JSON leaves the running configuration untouched and surfaces a parse error.
- When the terminal is too narrow, segments shrink or hide in a fixed priority regardless of their position in `segments`:
  1. `model` switches to compact text, then hides.
  2. `usage` switches to compact text, then hides.
  3. `hint` is ellipsized to available width, then hides.
  4. `context` switches to compact text, then hides.
- The `mode` badge is safety-relevant and is **never** hidden by responsive shrinking — only removing `"mode"` from `segments` hides it, and only the user should choose that.

### Interactive `/statusline` keys

- `↑` / `↓` (`j` / `k`): select segment.
- `Space` / `Enter` / `t`: toggle segment on or off (at least one segment must remain enabled).
- `←` / `→` (`h` / `l`): reorder enabled segments left or right with live preview.
- `c`: toggle `compact` mode live.
- `s`: save to `statusline.json`; `Esc`: revert live preview and exit without saving.
- Direct edits to `statusline.json` on disk take effect the next time the REPL starts.

---

## Color themes (`theme.json`, `themes/<name>.json`, & `/theme`)

### Switching themes & startup preference (`theme.json`)

- **Interactive `/theme [name]`**: opens a filterable picker over built-in and custom themes (or switches immediately to `<name>` for the running REPL session).
- **Built-in themes**: `dark` (default), `light`, `catppuccin`, `github-dark`, `gruvbox`, `solarized`, `tokyo-night`.
- **Startup selection (`$XDG_CONFIG_HOME/tasksmith/theme.json`)**: optional JSON file read at CLI startup:
  ```json
  {
    "theme": "catppuccin",
    "transparent": false
  }
  ```
  - `"theme"`: built-in or custom theme name loaded on startup (falls back to `"dark"` if missing or unknown).
  - `"transparent"`: when `true`, clears canvas/panel/hover/active surface fills so the terminal emulator's native background shows through while preserving code/elevated/selected/tint surfaces.

### Authoring custom themes (`$XDG_CONFIG_HOME/tasksmith/themes/<name>.json`)

Custom color schemes live in `$XDG_CONFIG_HOME/tasksmith/themes/<name>.json` and appear automatically in `/theme`. Prefer extending `"dark"` or `"light"` via `"extends"` so you only override the tokens you want to customize rather than defining every token from scratch:

```json
{
  "name": "my-theme",
  "extends": "dark",
  "type": "dark",
  "palette": {
    "bg": "#0d1117",
    "fg": "#c9d1d9",
    "accent": "#58a6ff"
  },
  "colors": {
    "text": { "primary": "fg", "accent": "accent" },
    "surface": { "canvas": "bg" },
    "icon": { "primary": "accent" }
  }
}
```

- **Fields**: `name`, optional `extends` (parent theme name; cycles are rejected), `type` (`"dark"` or `"light"`), optional `transparent` (`bool`), optional `palette` (`name → #RGB | #RGBA | #RRGGBB | #RRGGBBAA`), `colors` (token values referencing either a `palette` key or a `#` hex color), and optional `clients` (`"cli"`, `"tui"`, `"desktop"` per-client overrides).
- **Token groups under `colors`** (never invent group or token names outside these):
  - `text`: `primary`, `secondary`, `muted`, `disabled`, `inverse`, `accent`, `info`, `success`, `warning`, `error`
  - `surface`: `canvas`, `panel`, `elevated`, `code`, `hover`, `active`, `selected`, `disabled`
  - `tint`: `accent`, `info`, `success`, `warning`, `error`
  - `border`: `default`, `subtle`, `focus`, `accent`, `info`, `success`, `warning`, `error`
  - `icon`: `primary`, `secondary`, `muted`, `accent`, `info`, `success`, `warning`, `error`
  - `agent`: `brand`, `thinking`, `generating`, `executing`, `permission`, `subagents` (`red`, `blue`, `green`, `yellow`, `purple`, `orange`, `pink`, `cyan`)
  - `mode`: `default`, `plan`, `accept_edits`, `auto`, `ask`, `bash`
  - `diff`: `added_bg`, `added_text`, `added_word`, `added_dimmed`, `removed_bg`, `removed_text`, `removed_word`, `removed_dimmed`, `hunk_bg`, `hunk_text`
  - `transcript`: `user_bg`, `user_hover_bg`, `bash_bg`, `system_text`, `queued_text`
  - `meter`: `fill`, `warn`, `danger`, `empty`
  - `syntax`: `keyword`, `function`, `string`, `number`, `type`, `comment`, `operator`, `variable`, `punctuation`
