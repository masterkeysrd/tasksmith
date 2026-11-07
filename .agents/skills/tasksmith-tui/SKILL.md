---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: tasksmith-tui
  description: House patterns for building TaskSmith's terminal UI on the trail/kitex/kites stack - components, theming, state stores, interactive slash-command views, and component tests.
  displayName: TaskSmith Terminal UI
spec:
  useWhen: Writing or reviewing code under internal/ui/, internal/tui/, internal/cli/components/, internal/cli/command/, or internal/cli/store/ that renders terminal UI, adds a component, wires an interactive /command view, or tests a rendered component.
  keywords:
    - ui
    - tui
    - kitex
    - trail
    - kites
    - component
    - picker
    - slot
    - theme
    - terminal
    - view
    - render
  argumentHint: component or view to build
---

# TaskSmith Terminal UI

TaskSmith's UI is a **reactive, inline** terminal app. It does not use a fullscreen
alternate-screen buffer and it does not use Bubble Tea. Rendering is a component tree that
re-renders from state, closer to Ink/React than to a widget toolkit.

Three layers sit under `github.com/masterkeysrd/kite`. Use the one that matches the job:

| Layer | Package | Owns | Reach for it when |
| --- | --- | --- | --- |
| App runtime | `kite/extras/trail` | Inline lifecycle, input, focus, window size, `Static` scrollback | Declaring a component, handling keys, exiting |
| Reactive primitives | `kite/extras/kitex` | Hooks (`UseState`, `UseRef`, `UseEffect`, `UseMemo`, `UseInterval`), `Box`/`Span`/`Table` | Component state and effects |
| External state | `kite/extras/kites` | `Store[T]` + selector subscriptions | App-wide state outside one component |

`trail.Node` is a **type alias** for `kitex.Node`
(`kite/extras/trail/components.go`), so the two are interchangeable in signatures. This repo's
convention is to write `trail.Node` as the component return type.

## The layer rules

- **Components are `trail.FC` / `trail.FCC` / `trail.SimpleFC`.** Return `trail.Node`.
- **State hooks come from `kitex`.** Every `UseState` in this repo is `kitex.UseState`
  (`trail` re-exports these; do not use those aliases). Same for `UseRef`, `UseMemo`,
  `UseEffect`, `UseEffectCleanup`, `UseLayoutEffect`, `UseInterval`, `UseTimeout`.
- **Input and lifecycle hooks come from `trail`.** `trail.UseInput`, `trail.UseApp`,
  `trail.UseFocus`, `trail.UseFocusManager`, `trail.UseWindowSize`, `trail.UseStdout`.
- **Elements are `trail.Box` / `trail.Span` / `trail.Text` / `trail.Static` / `trail.Spacer` /
  `trail.Newline`.** `kitex.Button`, `kitex.Input`, `kitex.Table`/`TR`/`TD` are also used where
  a table or control is needed.
- **Never call `kitex.Render` in application code.** The app mounts once via `trail.Run`
  (`internal/cli/cli.go`). The only allowed call site is `internal/tui/testutils`; everything else
  renders through it.
- **Never import `kite/internal/**`.** It is not a public API.

## House component shape

```go
// SpinnerProps defines the properties for the animated thinking spinner.
type SpinnerProps struct {
	Text   string
	Status api.SessionStatus
}

// Spinner is a trail functional component rendering an animated braille spinner.
var Spinner = trail.FC("Spinner", func(props SpinnerProps) trail.Node {
	scheme := theme.UseTheme()
	frame, setFrame := kitex.UseState(0)

	kitex.UseInterval(func() {
		setFrame((frame() + 1) % len(SpinnerFrames))
	}, 80*time.Millisecond, nil)

	return trail.Span(trail.SpanProps{
		Style: style.S().Foreground(scheme.Color.Text.Accent),
	}, trail.Text(SpinnerFrames[frame()]+" "+props.Text))
})
```

Conventions encoded above, all of them load-bearing:

1. Exported component is a `var` named `PascalCase`; its props are a `<Name>Props` struct
   declared directly above it with a doc comment on both.
2. `theme.UseTheme()` is the first line of the render body when anything is colored.
   **Colors and borders come from the scheme, never literals.** Grep for a scheme field before
   inventing one.
3. State is read through the **getter at render time** (`frame()`), never through a value
   captured before the render. `setFrame` replaces the whole value.
4. `style.S()` builds a style; `style.S().Foreground(...)`, `.Padding(...)`,
   `.FlexDirection(style.FlexRow)`, `.WhiteSpace(style.WhiteSpacePre)`.
5. Use `trail.SimpleFC` when there are no props, `trail.FCC` when the component takes children —
   and for `FCC` the props struct must have a field literally named `Children []trail.Node`.

### Hook rules

- Hooks run **unconditionally**, in the **same order**, every render. No hook inside `if`, a
  loop, or a closure that may be skipped. Put the condition *inside* the hook body instead.
- Hooks are legal only directly inside a component render body.
- `UseEffect` / `UseMemo` / `UseCallback` / `UseInterval` take an explicit `deps []any` slice,
  compared with `reflect.DeepEqual` (length and order matter). **`nil` is not "no dependencies",
  and it does not mean the same thing across hooks:**

  | Hook | `deps = nil` | `deps = []any{}` |
  | --- | --- | --- |
  | `UseEffect`, `UseEffectCleanup`, `UseLayoutEffect`, `UseLayoutEffectCleanup` | `deps == nil` forces `changed` true → **re-runs (after cleaning up) on every render** | `depsEqual` on two empty slices → **runs once on mount** |
  | `UseInterval`, `UseTimeout` | safe: they forward `append([]any{interval}, deps...)`, so the slice is never nil → **runs once**, restarting only if the delay changes | same |
  | `UseMemo` | `depsEqual(nil, nil)` is equal → **computes once, never again** | same |
  | `UseCallback` | substitutes a fresh sequence number per render → **no memoization at all**; callback identity changes every render | memoized properly |

  So: for effect hooks, write "once on mount" as `[]any{}`, never `nil` — a `nil`-deps effect that
  subscribes or allocates tears it down and rebuilds it every render. For `UseMemo`, `nil` is a
  legitimate "constant". Never pass `nil` to `UseCallback`.
- `kitex.UseRef[T](nil)` holds a mutable value (typically a `dom.Element`) without triggering a
  re-render. Reading `.Current` does not make the UI reactive.
- Subscriptions, timers, and listeners must be released: prefer `kitex.UseEffectCleanup`, whose
  body returns the cleanup func.

## App-wide state (`kites`)

One `Store[T]` per concern, created as a package-level `var`, read through a selector hook so
only subscribing components re-render:

```go
var transcript = kites.Create(TranscriptState{})

// UseTranscript subscribes the calling component to a slice of transcript state.
func UseTranscript[U comparable](selector func(TranscriptState) U) U {
	return kites.Use(transcript, selector)
}

// GetTranscript reads current state without subscribing.
func GetTranscript() TranscriptState { return transcript.Get() }
```

- Mutations happen in store package functions, never from a component body during render.
- Inside a component use the `Use*` selector form. `Get*` is for non-reactive reads
  (event handlers, daemon callbacks).
- `store.UseApp(func(s store.AppState) *[]api.Skill { return s.Skills })` is how a view reads
  daemon-backed app data.

## Rendering rules

- **Text width is a real constraint.** Truncate or ellipsize anything derived from user or
  daemon data before it reaches `trail.Text`. Never pad by assuming a fixed terminal width;
  read `trail.UseWindowSize()` when layout depends on it.
- `trail.Static` writes finished rows into native scrollback **once** and never re-renders them.
  Anything in `Static` must be immutable; changing it produces duplicated output, not an update.
- Layout is flexbox: `style.FlexRow`/`FlexColumn`, `FlexGrow`, `MinWidth(style.Cells(0))`.
  `MinWidth(style.Cells(0))` is what lets a row shrink below its content.
- `trail.Box`/`Span` accept `trail.SpanProps{Style: ...}` / `trail.BoxProps{Style: ...}`.
- Prefer `kitex.Fragment`, `kitex.If`, `kitex.Map` over building `[]trail.Node` by hand.
  `nil` children are ignored, so a conditional branch can return `nil`.
- **`kitex.IfElse` evaluates both branches eagerly.** Use `kitex.If` with a closure whenever the
  skipped branch is expensive or has side effects.
- Keys: any list rendered with `kitex.Map` into components that hold state needs stable identity,
  otherwise state follows the index instead of the item.

## When to read a reference

| Doing this | Read |
| --- | --- |
| Picking an existing component instead of writing a new one; what `Option`/`Column`/`KeyBinding` accept | `references/components.md` |
| Adding or changing an interactive `/command` (picker, browser, inspector, slot lifecycle) | `references/views.md` |
| Writing a test that renders a component | `references/testing.md` |

Reuse before authoring. `internal/ui/components` and `internal/cli/components` already cover
lists, pickers, markdown, code blocks, keyboard hints, autocomplete, tool widgets, permission and
question prompts. Extending an existing component's props (a new optional field) is strongly
preferred over a parallel component that re-implements its styling.

## Verification

A UI change is not done until `go test -count=1 ./...` passes **and** the change has a test that
renders it. Do not regenerate or hand-edit a golden/snapshot file to make a failure disappear.
Full gate: `go-verification-gate`.

If `kite`'s API is unclear, read the code that actually compiles rather than guessing. Check `go.mod`
first: a `replace github.com/masterkeysrd/kite => <path>` line means the sibling checkout at that path
is what builds, so read it directly. Without a `replace`, the `require` pseudo-version is authoritative
and the compiled code lives at `$(go env GOMODCACHE)/github.com/masterkeysrd/kite@<version>/`.
A `replace` is a local convenience, not a pin — never treat green tests as proof the `require` version
is sound while one is in effect. Cite the package you actually read, not a remembered API.
