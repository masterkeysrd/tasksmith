# Status Line Configuration

File: `$XDG_CONFIG_HOME/tasksmith/statusline.json` (created by `/statusline` save; optional).

```json
{
  "segments": ["hint", "mode", "context", "usage", "model"],
  "compact": false
}
```

## Schema

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `segments` | ordered array of segment IDs | all five, in the order above | Enabled segments. The first renders on the left of the status line, the remaining ones on the right, separated by ` · `. |
| `compact` | boolean | `false` | Shorter renderings for `context` (percentage only) and `model` (model name only). |

## Segment IDs

| ID | Renders | Example |
| --- | --- | --- |
| `hint` | keyboard hint or the Ctrl+C cancel prompt | `shift+tab cycle mode • /help commands` |
| `mode` | permission mode badge | `plan` |
| `context` | context window usage | `109.5k/400.0k (27%)`, compact: `27%` |
| `usage` | session cost with input/output tokens | `$0.42 · ↑128.4k ↓8.3k` |
| `model` | model and thinking effort | `gpt-5 · high`, compact: `gpt-5` |

IDs are matched case-insensitively after trimming.

## Validation rules

Applied when the file is loaded and when it is saved:

- Unknown IDs are dropped and reported; known IDs are kept in the given order.
- Duplicates collapse to their first occurrence.
- An empty or invalid `segments` list falls back to the built-in default order.
- Malformed JSON leaves the running configuration untouched and surfaces a parse error.

## Responsive hiding ladder

When the terminal is too narrow for the configured composition, segments are shrunk or hidden in
a fixed order regardless of their position in `segments`:

1. `model` switches to compact text, then hides.
2. `usage` switches to compact text, then hides.
3. `hint` is ellipsized to the available room, then hides.
4. `context` switches to compact text, then hides.

The `mode` badge is protected and is never hidden — it is safety-relevant. Removing it from
`segments` is the only way to hide it, and only the user should decide that.

## Applying changes

- `/statusline` in the REPL edits the composition interactively with a live preview; `s` saves
  (writing this file), `esc` restores the previous configuration.
- Editing the file directly takes effect the next time the REPL starts.
