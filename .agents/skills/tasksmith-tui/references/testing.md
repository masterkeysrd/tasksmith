# Testing rendered components

UI tests are **headless**: `testenv` runs the engine against a mock backend in memory, so there is
no real terminal, no alternate screen, and no flake from terminal size. Assert on the **rendered
text**, which is what a user sees, rather than on internal node structure.

## The render helper

Use `internal/tui/testutils` rather than defining a local render helper. Two strategies, because
components have different needs:

```go
import "github.com/masterkeysrd/tasksmith/internal/tui/testutils"

// Headless (default): testenv + kitex.Render. Fast, deterministic, enough for
// anything that only reads props and kitex state.
out := testutils.Render(t, MyComponent(props), 100, 24)

// Live runtime: a real trail instance on a mock backend. Needed for trail.UseApp,
// trail.UseInput, Static output, or anything that only exists inside a running
// instance.
app := testutils.NewApp(t, MyComponent(props), 100, 24)
out := app.Text()
```

Widths are always explicit — a test states the terminal size it assumes instead of inheriting a
default that can change under it. Widths in use: 100×40 and 100×30 predominate, 80×24 in
`internal/ui/components`, with deliberately narrow sizes (e.g. 40×12) for truncation cases.

`Render` returns the terminal dump trimmed at the ends only, which is what existing assertions are
written against: interior lines keep their right padding so column-aligned output still lines up.
That behavior is pinned by `TestRenderKeepsInteriorPadding`; use `testutils.TrimmedLinesText` or
`testutils.TrimmedLines` when trailing padding gets in the way.

`kitex.Render` is legitimate **only** inside this package. Production code mounts through
`trail.Run`.

When a test renders the same component more than once — to assert that a state change updated the
screen — reuse one environment so the tree reconciles in place:

```go
env := testutils.NewEnv(t, 100, 24)
first := env.Render(MyComponent(props))
second := env.Render(MyComponent(otherProps))
```

Live-runtime tests can wait on asynchronous work with `app.WaitFor(func(text string) bool {...})`
instead of sleeping.

## Text assertions

```go
func TestOptionListRendersGroupHeadings(t *testing.T) {
	out := testutils.Render(t, OptionList(OptionListProps{
		Options: []Option{{Value: "a", Label: "a"}},
	}), 80, 24)
	if !strings.Contains(out, "a") {
		t.Fatalf("rendered option missing from output:\n%s", out)
	}
}
```

Always dump `out` into the failure message — a rendered-text assertion is unreadable without the
actual output.

For layout, use `testutils.TrimmedLines(out)` to get lines without the terminal's right padding,
so assertions can reason about indentation and blank lines.

## Driving input

Use `env.Press`, which delivers a key and **flushes before returning**. Forgetting the flush is the
most common way to write a test that fails for the wrong reason: rendering is scheduled, so an
assertion immediately after a key press otherwise reads the pre-key frame.

```go
env := testutils.NewEnv(t, 100, 24)
env.Render(browser(props))

// Pressing "3" selects the third menu entry.
env.Press(key.Key{Code: '3', Text: "3"})
if resources != 1 || tools != 0 {
	t.Fatalf("pressing 3 fired tools=%d resources=%d, want resources only", tools, resources)
}

// Selecting an entry moves the cursor, so two ups land on the first entry.
env.Press(key.Key{Code: key.KeyUp})
env.Press(key.Key{Code: key.KeyUp})
env.Press(key.Key{Code: key.KeyEnter})
```

- Set `Text` for printable characters (`key.Key{Code: '3', Text: "3"}`), since a handler matching on
  the typed rune needs it. Named keys use `key.KeyUp`, `key.KeyEnter`, `key.KeyEscape`.
- `env.KeyPress("esc")`, `env.Type("hello")`, and `env.Press` all return the resulting text.
- `Env` embeds `*testenv.Environment`, so `env.Click(x, y)`, `env.Wheel(...)`, `env.Resize(w, h)`,
  `env.Flush()`, `env.DumpText()`, and `env.DumpANSI()` are all available directly.
- `env.QuerySelector(sel)` and `env.GetNodeByID(id)` pierce UA shadow subtrees, which is how you
  assert on the internals of a control you don't own. `env.HasFocus(el)` / `env.CurrentFocus()`
  verify focus behavior.
- For a live runtime, `app.Press(key)` delivers the key and returns the new frame's text.

## Prefer behavior over pixels

For an interactive view, the highest-value assertions are **observable effects of keys**: which
callback fired, how many times, and whether an out-of-range key stayed inert. That catches the
regressions that matter (a key bound to the wrong action, a digit wrapping around, `esc` not
dismissing) and survives restyling. Text assertions then pin the visible labels separately.

Assert counts, not just "something happened" — `if tools != 1` catches double-firing that
`if tools > 0` misses.

## Interaction test skeleton

```go
func TestBrowserKeys(t *testing.T) {
	var tools, resources, back int
	env := testutils.NewEnv(t, 100, 24)
	env.Render(browser(testProps(
		func() { tools++ },
		func() { resources++ },
		func() { back++ },
	)))

	tests := []struct {
		name  string
		press key.Key
		want  func() bool
	}{
		{"t opens tools", key.Key{Code: 't', Text: "t"}, func() bool { return tools == 1 }},
		{"esc closes", key.Key{Code: key.KeyEscape}, func() bool { return back == 1 }},
	}
	for _, tc := range tests {
		out := env.Press(tc.press)
		if !tc.want() {
			t.Errorf("%s: tools=%d resources=%d back=%d\n%s", tc.name,
				tools, resources, back, out)
		}
	}
}
```

Inject counters through props/callbacks rather than reaching into component state; it keeps the
test about the contract the view exposes.

## Effects, timers, and async

- Effects flush asynchronously. `env.Press` flushes for you; call `env.Flush()` directly when an
  effect is triggered by something other than a key, and `env.RenderFrame()` for scheduled work.
  On a live runtime, `app.WaitFor(cond)` polls frames until the condition holds instead of
  sleeping.
- Do not assert on wall-clock timing. A `UseInterval` spinner's *animation* is not worth testing;
  its rendered state labels are.
- A view that fetches on mount must not hit the network in tests — inject a stub client or seed
  the store before rendering.

## Reading a mock backend that a live loop is writing

When a test asserts against a backend an engine goroutine is still writing, read it only through the
mock's accessors — `LastFrame()`, `AllInsertedAbove()`, `InsertedAboveCount()`, `Counts()` — never its
exported fields. `testutils.ScrollbackText` / `testutils.AllText` already do this, so prefer them over
hand-rolling a buffer-to-text loop.

For an app that clears the screen and redraws, wrap the backend in `testutils.NewClearingBackend`
instead of writing your own fake. `Clear` only wipes the live viewport, so scrollback committed before
it would otherwise keep satisfying assertions against output the app no longer shows; the wrapper
reports only what was committed after the last clear.

## What to cover for any UI change

1. Happy-path render (text present, correctly ordered).
2. Empty state (`EmptyText`, or nothing rendered when that is intended).
3. Truncation / narrow width for anything derived from user or daemon data.
4. Each bound key, plus one key that must be inert.
5. Dismissal (`esc`) and the resulting store effect.

Add the test alongside the change, not afterwards. TaskSmith asserts on rendered text and does not
use golden snapshots today; `testenv` offers `env.MatchGolden(t, "name.golden")` if a change ever
needs pixel-level regression, but prefer text assertions, and never hand-edit a golden to make a
failure pass. Full gate: `go-verification-gate`.
