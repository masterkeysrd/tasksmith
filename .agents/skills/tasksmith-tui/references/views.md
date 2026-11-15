# Interactive `/command` views

An interactive slash command does **not** own a terminal or call `trail.Run`. The app runs one
`trail.Run` for its whole lifetime (`internal/cli/cli.go`); a command instead enqueues a view into
the **slot** queue and the running app renders it above the composer. This is what keeps the
transcript intact and lets several prompts stack.

```
handler → store.EnqueueSlot(SlotItem{Kind, Data})
             → <Slot/> dispatch (internal/cli/components/slot.go)
                → child component rendered with active.ID
                   → user acts → store.RemoveSlot(id)
```

## 1. Register the command

```go
func (r *Registry) registerSkillCommands() {
	r.Register(Command{
		Name:        "skills",
		Usage:       "/skills",
		Description: "List, inspect, and reload workspace skills",
		Handler:     handleSkills,
	})
}

func handleSkills(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("/skills does not accept arguments (did you mean to invoke /%s?)",
			strings.TrimPrefix(args[0], "/"))
	}
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: SkillsBrowser,
	})
	return nil
}
```

Reject unexpected arguments in the handler with a message that suggests the likely intent.
Return an `error` rather than printing; the caller renders it.

## 2. Pick the slot kind

| Kind | `Data` accepted | Composer hidden | Use for |
| --- | --- | --- | --- |
| `SlotKindPicker` | `PickerProps`, `SlotComponent`, `func(slotID string) trail.Node`, `trail.Node` | yes | listings, browsers, inspectors |
| `SlotKindPermission` | `PermissionPromptProps` or `api.PermissionRequest` | yes | tool approval |
| `SlotKindQuestion` | `QuestionPromptProps` or `api.QuestionRequest` | yes | structured user questions |
| `SlotKindAutocomplete` | autocomplete state | no | inline completion |

`Picker` **auto-removes the slot** on select and cancel by wrapping `OnSelect`/`OnCancel`, so a
plain picker never touches `RemoveSlot`. A custom view is responsible for its own dismissal.

`store.EnqueueSlot` assigns `slot-<n>` when `ID` is empty. Pass an explicit `ID` to make the entry
idempotent: enqueueing the same `ID` **replaces** that queue entry instead of stacking a duplicate,
which is what you want for a view that can be re-rendered by a background update.

## 3. The view component

```go
// SkillsBrowser renders an interactive skills list and markdown inspector inside <Slot/>.
var SkillsBrowser = trail.FC("SkillsBrowser", func(slotID string) trail.Node {
	skillsPtr := store.UseApp(func(s store.AppState) *[]api.Skill { return s.Skills })
	selectedSkill, setSelectedSkill := kitex.UseState[*api.Skill](nil)
	scrollPos, setScrollPos := kitex.UseState(0)
	isReloading, setIsReloading := kitex.UseState(false)

	var skills []api.Skill
	if skillsPtr != nil {
		skills = *skillsPtr
	}

	kitex.UseEffect(func() {
		if skillsPtr == nil {
			if c := store.Client(); c != nil {
				go func() {
					if sks, err := c.ListSkills(context.Background()); err == nil {
						store.SetWorkspaceSkills(sks)
					}
				}()
			}
		}
	}, []any{}) // []any{}, not nil — see the deps table in SKILL.md

	trail.UseInput(func(_ string, k event.KeyEvent) {
		switch {
		case k.MatchString("esc", "escape"):
			store.RemoveSlot(slotID)
		case k.MatchString("up", "k"):
			if cur := scrollPos(); cur > 0 {
				setScrollPos(cur - 1)
			}
		}
	})
	// ...
})
```

Rules that matter here:

- **Read daemon data through the store, never a fresh client.** `store.UseApp(selector)` subscribes
  the view, so a background refresh re-renders it. `store.Client()` is for issuing calls.
- **Fetch in an effect, not the render body.** Render must stay pure.
- **Keys go through `trail.UseInput` + `k.MatchString(...)`.** `MatchString` accepts the aliases a
  user may actually press (`"esc", "escape"`, `"pgdown", "space", "f", "ctrl+d"`). Match on
  semantics, not raw codes.
- **Every view dismisses itself on `esc`** and calls `store.RemoveSlot(slotID)`.
- Leave a paper trail: `store.AppendCommandOutput("/skills", "Exited /skills command")` on close,
  so the scrollback records what happened.
- Reuse `Picker`/`OptionList` before hand-rolling navigation; hand-rolled key handling should still
  match the bindings shown by `KeyboardHint`.
- Keep presentation in helpers. A browser typically splits into pure functions — status icon,
  color, badge, cell text — each taking `*theme.Scheme` and returning `trail.Node`, so the
  component body stays about behavior.

## 4. Two-pane browsers

The established shape is: `Picker` for the list, and selecting or pressing `i` swaps the slot
contents to an inspector view (scrollable via `up`/`k`, `down`/`j`, `pgup`/`b`/`ctrl+u`,
`pgdown`/`space`/`f`/`ctrl+d`, `home`/`g`, `end`/`G`). Scrolling uses a `kitex.UseRef[dom.Element]`
plus `kitex.UseLayoutEffect` calling `el.ScrollTo(0, pos)` with `[]any{pos}` deps — layout effects
run before paint, so the scroll doesn't visibly jump.

## 5. Async and long operations

Daemon calls are issued from goroutines started in effects or key handlers, and results are pushed
into a store (`store.SetWorkspaceSkills`, `EnqueueSlot`, `AppendCommandOutput`), which re-renders
subscribers. Never block a render or an input handler on a network call.

For an operation that can take a while, set a local `isReloading` state to render a `Spinner`, and
clear it when the call returns. Surface failure with `store.AppendCommandOutput` or a notice store
rather than swallowing it.

## 6. Checklist for a new interactive command

1. `Command` registered with `Name`/`Usage`/`Description` and argument validation.
2. Handler enqueues a slot; view is a `trail.FC` in the same `internal/cli/command/` package.
3. `PickerProps.Columns`, `Bindings`, and `Keys` set; every key has a binding.
4. `esc` dismisses; closing appends command output.
5. Data read via `store.UseApp`, fetched in an effect with `[]any{}` deps.
6. Colors from `theme.UseTheme()`; text truncated; numbers via `internal/ui/format`.
7. Test renders the view and drives it with `env.SendKey` — see `references/testing.md`.
8. `docs/reference/cli.md` updated with the command, its keys, and any flags; `docs/guides/cli.md`
   if the workflow is new. This is required by `AGENTS.md`, not optional.
