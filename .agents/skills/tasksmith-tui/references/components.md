# Shared components

Reuse before authoring. Every component below is already themed, truncated, and tested; a new
one that duplicates any of that is a regression in consistency. If an existing component is close
but missing something, **add an optional field to its props** — do not fork it.

Two packages:

- `internal/ui/components` — generic, presentation-only, no daemon knowledge.
- `internal/cli/components` — TaskSmith-specific; reads `internal/cli/store` and `api` types.

Put new generic widgets in `ui/components`; anything that knows about sessions, tools,
transcripts, or the daemon belongs in `cli/components`.

## `internal/ui/components`

| Component | Kind | Props that matter |
| --- | --- | --- |
| `OptionList` | `trail.FC` | `Options []Option`, `Selected int`, `MaxVisible`, `Columns []Column`, `EmptyText`, `Styles *OptionListStyles`, `GroupLabel func(Option) string` |
| `Autocomplete` | `trail.FC` | `State AutocompleteState`, `Views map[string]AutocompleteView`, `Bindings []KeyBinding` |
| `KeyboardHint` | `trail.FC` | `Bindings []KeyBinding`; renders `nil` when empty |
| `Markdown` | `kitex.FC` | `Source`, `Style`, `PlainCodeBlocks`, `OnAttachmentClick func(refType, rawValue string)`, `IsDirectory func(path string) bool` |
| `CodeBlock` | `kitex.FC` | `Code`, `Lang`, `Style`, `HideHeader`, `ShowLineNumbers`, `StartLine`, `Compact`, `Wrap`, `GutterMinWidth` |
| `Button` | `kitex.FCC` | `Key`, `Variant`, `Color`, `Disabled`, `Active`, `OnClick` |

Supporting types:

```go
type Column struct {
	Width int             // 0 = flexible / remaining space, >0 = fixed width in cells
	Align style.TextAlign // style.TextAlignLeft (default), Center, Right
	Style style.Style     // optional override for unselected rows
}

type Option struct {
	Value       string
	Label       string
	Description string
	Cells       []string      // one entry per Column
	CellNodes   []trail.Node  // per-cell node override; nil entry falls back to Cells[i]
	Current     bool          // marks the active item (" • current")
	Icon        trail.Node    // status glyph in the cursor gutter, costs no column
}

type KeyBinding struct {
	Key   string // "↑/↓", "enter", "esc"
	Label string // "Select"
}
```

Notes that save time:

- `Option.Icon` and `OptionListProps.GroupLabel` exist so a listing can show status and grouping
  **without spending a column**. Prefer them over adding narrow columns. `CellNodes` lets one
  cell carry styled nodes (badge, colored value) while the rest stay plain strings.
- `MaxVisible` enables sliding-window pagination; `DefaultPickerMaxVisible` is 8 and
  `DefaultAutocompleteMaxVisible` is 5.
- `GroupLabel` renders a muted heading whenever the value changes between adjacent options, so
  **sort by group first** or headings repeat.
- With no options, `OptionList` renders **nothing** unless `EmptyText` is set — so pass
  `EmptyText` (e.g. `"  No matching options."`) instead of guarding at the call site.
- `CellNodes` only wins for a column if the index is in range and non-nil, and for column 0 a
  non-nil `Icon` takes precedence over `CellNodes[0]`.
- `Markdown` renders `@file:` / `@sym:` / `@skill:` pills; they become static badges unless
  `OnAttachmentClick` is set.

## `internal/cli/components`

Composed screen regions, mostly driven by store state:

- **Chat surface**: `Message`, `MessageText`, `MessageThought`, `MessageTool`, `Transcript`,
  `ActiveStream`, `StreamingHint`, `Spinner`, `Header`, `Logo`, `ContextUsage`, `DaemonAlert`,
  `StatusLine`. Compaction is not its own component — it surfaces as a transcript timeline event
  and as `Spinner`'s `StatusCompacting` state.
- **Input**: `Composer` (`Disabled`, `OnSubmit`, `OnCycleMode`, `OnCancel`, `Autocomplete`,
  `CompletionViews`), `Picker`, `Slot`.
- **Prompts**: `PermissionPrompt`, `QuestionPrompt`, `PermissionEditor`.
- **Per-tool widgets**: `ViewToolWidget`, `LsToolWidget`, `GlobToolWidget`, `GrepToolWidget`,
  `WriteToolWidget`, `EditToolWidget`, `RemoveToolWidget`, `BashToolWidget`, `WebFetchToolWidget`,
  `DownloadToolWidget`, `TasksToolWidget`, `TodosToolWidget`, `ToolDefaultWidget`, plus matching
  `<Tool>Renderer(tool store.ToolPart) trail.Node` functions.
- **Diff**: `DiffView`.

Adding a tool widget means adding both the `trail.FC` and its `Renderer`, and wiring it into the
renderer dispatch — the default widget exists so an unmapped tool still renders sanely.

## `Picker` — the interactive-list default

Almost every `/command` browser is a `Picker`, built through `NewPicker` so defaults are shared:

```go
pickerProps := components.NewPicker(title, opts, func(opt uicomponents.Option) {
	// OnSelect
})
pickerProps.Columns = []uicomponents.Column{
	{Width: 30, Align: style.TextAlignLeft},
	{Width: 10, Align: style.TextAlignLeft},
	{Width: 8, Align: style.TextAlignRight},
	{Width: 0, Align: style.TextAlignLeft}, // flexible
}
pickerProps.Bindings = []uicomponents.KeyBinding{
	{Key: "↑/↓", Label: "Select"},
	{Key: "r", Label: "Reload workspace"},
	{Key: "esc", Label: "Close"},
}
pickerProps.Keys = map[string]func(uicomponents.Option){
	"r": func(_ uicomponents.Option) { /* reload */ },
}
pickerProps.GroupLabel = func(opt uicomponents.Option) string { return scopeOf(opt) }
```

`PickerProps` also accepts `Subtitle`, `Header`, `MaxVisible`, `Filterable`, `OnOpen`,
`OnCancel`, `ConfirmActions map[string]ConfirmAction`, and `InputActions map[string]InputAction`
(a key that opens an inline text prompt with `Prompt`, `InitialValue`, `OnSubmit`).

Declare `Bindings` for every entry of `Keys` — the hint bar is generated from `Bindings`, so a
key without a binding is invisible and undiscoverable. It must also match
`docs/reference/cli.md`.

## Theming

`theme.UseTheme()` returns a reactive `*theme.Scheme`; it subscribes the component, so a theme
switch re-renders it. Access is nested — there is no flat `scheme.Accent`:

```go
scheme := theme.UseTheme()
style.S().Foreground(scheme.Color.Text.Accent)     // emphasis
style.S().Foreground(scheme.Color.Text.Muted)      // secondary, counts, metadata
style.S().Foreground(scheme.Color.Text.Success)    // ok / connected
style.S().Foreground(scheme.Color.Text.Warning)
style.S().Foreground(scheme.Color.Text.Error)
style.S().Foreground(scheme.Color.Agent.Thinking)  // agent activity
```

Groups under `scheme.Color`: `Text`, `Surface`, `Tint`, `Border`, `Icon`, `Agent`, `Mode`,
`Diff`, `Transcript`, `Meter`, `Syntax`. Grep `type .*Color struct` in `internal/ui/theme/theme.go`
before adding a color; if a genuinely new semantic role is needed, add it to the scheme and all
built-in themes rather than inlining a hex value.

- Inside a component: `theme.UseTheme()`. Outside a render (a plain helper, a daemon callback):
  `theme.Get()`.
- Prefer passing `*theme.Scheme` into helper funcs over calling `theme.Get()` inside them, which
  keeps helpers pure and testable.
- `internal/ui/format` owns human-readable values — `format.Tokens`, `format.Duration`,
  `format.DurationMilis`, `format.FormatCostNano`, `format.CompactSingleLine(s, maxRunes)`.
  Never hand-roll token/duration/cost formatting.
- `internal/core/strutils` owns truncation: `TruncateBytes`, `TruncateLine(s, maxBytes, suffix)`,
  `TruncateHead`. Anything user- or daemon-sourced that can exceed a column width must go
  through one of these.
- `internal/ui/components/icon` provides file/LSP kind glyphs (`icon.LspKindIcon(kind, scheme)`).

## Style notes

- `style.S()` starts an empty style; chain setters (`.Foreground()`, `.Bold()`, `.Padding()`,
  `.FlexDirection()`, `.Flex(0, 0)`, `.MinWidth(style.Cells(0))`, `.WhiteSpace(style.WhiteSpacePre)`).
- `style.WhiteSpace(style.WhiteSpacePre)` is required for any string with meaningful leading
  spaces or alignment padding, otherwise the layout collapses it.
- `style.Cells(n)` is the cell unit for widths and min/max constraints.
- `Flex(0, 0)` pins a gutter or glyph so it is never shrunk; `MinWidth(style.Cells(0))` on the
  flexible sibling is what allows it to shrink below content width.
