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

	bindings := uicomponents.UseKeyMap([]uicomponents.KeyAction{
		{Keys: []string{"esc", "escape"}, Label: "Close", OnPress: func() { store.RemoveSlot(slotID) }},
		{
			Keys: []string{"up", "k"}, Label: "Scroll up",
			OnPress: func() { setScrollPos(max(0, scrollPos()-1)) },
		},
	}, true)
	return uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{Bindings: bindings})
})
```

Rules that matter here:

- **Read daemon data through the store, never a fresh client.** `store.UseApp(selector)` subscribes
  the view, so a background refresh re-renders it. `store.Client()` is for issuing calls.
- **Fetch in an effect, not the render body.** Render must stay pure.
- **Use `UseKeyMap` for view shortcuts** and feed its return value to `KeyboardHint`. Group
  aliases under `Keys`; `Hint` overrides their display. Use `Disabled` for unavailable actions
  and an inactive map when the child owns all input.
- **Escape closes the root view** with `store.RemoveSlot(slotID)`; in a nested inspector it pops the route.
- Leave a paper trail: `store.AppendCommandOutput("/skills", "Exited /skills command")` on close,
  so the scrollback records what happened.
- Reuse `Picker`/`OptionList` before hand-rolling navigation; hand-rolled key handling should still
  match the bindings shown by `KeyboardHint`.
- Keep presentation in helpers. A browser typically splits into pure functions — status icon,
  color, badge, cell text — each taking `*theme.Scheme` and returning `trail.Node`, so the
  component body stays about behavior.

## 4. Two-pane browsers

Use `flight.Stack` inside the slot. Put filters that must survive a pop above the stack.
Route structs must be comparable and ID-only: never store an API record, map, or slice in one.
Resolve details from live store data and handle records removed during a refresh.

```go
type subagentsListRoute struct{}
type subagentDetailRoute struct{ ID string }

return flight.Stack(flight.StackProps{
	InitialRoute: subagentsListRoute{},
	RenderRoute: func(route flight.Route) trail.Node {
		if detail, ok := route.(subagentDetailRoute); ok {
			return subagentDetailView(detail.ID)
		}
		return subagentsList(listProps)
	},
})
```

Call `nav := flight.UseNavigation()` **inside the routed component**, not in `RenderRoute`.
Select with `nav.Push(subagentDetailRoute{ID: opt.Value})`; in the detail adapter pass
`OnClose: nav.Pop` to the viewer. Escape returns to the list; only closing the root removes the slot.
A directly mounted viewer can accept another `OnClose` without depending on a navigator.

When a routed screen swaps between conditional branches (for example, an empty/loading state and a
`Filterable: true` `Picker`), wrap both branches in a stable outer `trail.Box` so `flight.Stack`'s
focus scope stays anchored to a mounted root element.

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
