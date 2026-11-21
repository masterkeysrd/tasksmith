# Status Line & Theme Appearance

## Status line (`statusline.json` & `/statusline`)

File: `$XDG_CONFIG_HOME/tasksmith/statusline.json` (created by `/statusline` save; optional).

```json
{
  "rows": [
    ["hint", "mode", "model", "effort", "warnings"],
    ["context", "cost", "tokens"]
  ],
  "compact": false
}
```

This is the default layout. The top row places the shortcut hint on the left and the other segments on the right. The bottom row places Context on the left and groups Cost or Quota with Tokens on the right, separated by `·`. Each row places its first configured segment on the left, and rows with no visible segments are omitted.

### Schema

| Field | Type | Meaning |
| --- | --- | --- |
| `rows` | array of up to two ordered arrays of segment IDs | Each inner array lists the enabled segments for that row. |
| `compact` | boolean (default: `false`) | Uses shorter text where available without changing row assignments. |

| ID | Default text | Compact text |
| --- | --- | --- |
| `hint` | `Shift+Tab mode · /help`, or the Ctrl+C cancel prompt. | Unchanged. |
| `mode` | Permission mode badge. Do not remove unless the user explicitly asks. | `[edits]` for accept edits; other modes are unchanged. |
| `model` | Active model. | Unchanged. |
| `effort` | Thinking effort. | Unchanged. |
| `warnings` | Active workspace and MCP warning count (`⚠ N warnings`); hidden at zero. | `⚠ N` |
| `context` | Context window usage, e.g. `Context 109.5k/400.0k (27%)`. | `27%` |
| `cost` | Session cost, or remaining quota with its reset countdown for subscription-billed models, when available. Do not invent a dollar cost or reset time for a subscription. | Amount or remaining quota, without the label or countdown. |
| `tokens` | Session token totals, with `↑` for input (including cached tokens) and `↓` for output. | Same totals without the `Tokens` label. |

IDs are trimmed and matched case-insensitively. Unknown IDs, duplicate occurrences after the first, and extra rows are dropped and reported. If no valid segments remain, the default rows are used. The old `segments` field and `usage` ID are not supported; configure `cost` and `tokens` separately in `rows`. Malformed JSON surfaces a parse error without replacing the current configuration.

### Interactive `/statusline` keys

- `↑` / `↓` (`k` / `j`): select a segment.
- `Space` / `Enter` / `t`: toggle the selected segment (at least one must remain enabled).
- `←` / `→` (`h` / `l`): reorder the selected enabled segment within its row.
- `r`: move the selected enabled segment to the other row.
- `c`: toggle `compact` mode in the live preview.
- `s`: save to `statusline.json`; `Esc`: revert the preview and exit without saving.
- Direct edits to `statusline.json` take effect the next time the REPL starts.

### Inspecting active warnings (`/warnings`)

Type `/warnings` in the composer and press `Enter`, or click the warning count if the terminal supports mouse input and no other interactive view is open. Use `↑` / `↓`, `k` / `j`, or `Ctrl+P` / `Ctrl+N` to select a warning or scroll its details. The list shows an explanation and a `Fix:` line when guidance is available (for example, `/mcp auth <name>` for a server that needs authorization). `Enter` opens the full warning. `Esc` goes back to the list or closes it.

- Routine workspace, provider, model alias, and MCP health warnings appear in this view and its status-line count instead of the banner or transcript. Passive MCP refresh adds no transcript announcements.
- Sandbox-specific workspace warnings still appear in the header and are also included in the view and count. Operation-specific failures, security notices, and explicit `/mcp` outcomes still appear inline as before.
- Previously printed scrollback is not rewritten; the view shows what is active now, not historical notices.

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
