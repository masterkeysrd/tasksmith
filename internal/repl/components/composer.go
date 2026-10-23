package components

import (
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
)

// ComposerProps defines the properties for the Composer functional component.
type ComposerProps struct {
	Disabled        bool
	Autocomplete    *autocomplete.Engine
	CompletionViews map[string]uicomponents.AutocompleteView
	OnSubmit        func(text string)
	OnCycleMode     func()
	OnCancel        func()
}

// PromptHistory holds state and operations for traversing input prompt history.
type PromptHistory struct {
	History    []string
	HistoryIdx int
	Draft      string
	Up         func(currentValue string) (string, bool)
	Down       func() (string, bool)
	Add        func(text string)
}

// usePromptHistory manages input prompt history navigation and draft preservation.
func usePromptHistory() PromptHistory {
	history, setHistory := kitex.UseState([]string(nil))
	historyIdx, setHistoryIdx := kitex.UseState(0)
	draft, setDraft := kitex.UseState("")

	up := func(currentValue string) (string, bool) {
		hist := history()
		if len(hist) == 0 {
			return "", false
		}
		hIdx := historyIdx()
		if hIdx >= len(hist) {
			setDraft(currentValue)
			newIdx := len(hist) - 1
			setHistoryIdx(newIdx)
			return hist[newIdx], true
		} else if hIdx > 0 {
			newIdx := hIdx - 1
			setHistoryIdx(newIdx)
			return hist[newIdx], true
		}
		return "", false
	}

	down := func() (string, bool) {
		hist := history()
		if len(hist) == 0 {
			return "", false
		}
		hIdx := historyIdx()
		if hIdx < len(hist)-1 {
			newIdx := hIdx + 1
			setHistoryIdx(newIdx)
			return hist[newIdx], true
		} else if hIdx == len(hist)-1 {
			setHistoryIdx(len(hist))
			return draft(), true
		}
		return "", false
	}

	add := func(text string) {
		hist := append(history(), text)
		setHistory(hist)
		setHistoryIdx(len(hist))
		setDraft("")
	}

	return PromptHistory{
		History:    history(),
		HistoryIdx: historyIdx(),
		Draft:      draft(),
		Up:         up,
		Down:       down,
		Add:        add,
	}
}

// Composer is a trail functional component managing multi-line user prompt drafting,
// autocomplete, prompt history navigation, and mode cycling. It requires an
// enclosing wind.Provider.
var Composer = trail.FC("Composer", func(props ComposerProps) trail.Node {
	draft, setDraft := kitex.UseState(uicomponents.AutocompleteEdit{})
	history := usePromptHistory()
	auto := uicomponents.UseAutocomplete(uicomponents.AutocompleteOptions{
		Engine: props.Autocomplete, Value: draft().Value, Cursor: draft().Cursor, Disabled: props.Disabled,
	})

	inputRef := kitex.UseRef[dom.Element](nil)
	trail.UseFocus(inputRef, trail.FocusOptions{
		AutoFocus: true,
		IsActive:  !props.Disabled,
	})

	readDraft := func() uicomponents.AutocompleteEdit {
		if input, ok := inputRef.Current.(*element.TextAreaElement); ok {
			return uicomponents.AutocompleteEdit{Value: input.TextContent(), Cursor: input.Buffer().ByteOffset()}
		}
		return draft()
	}
	syncDraft := func() {
		if next := readDraft(); next != draft() {
			setDraft(next)
		}
	}
	replaceDraft := func(next uicomponents.AutocompleteEdit) {
		if input, ok := inputRef.Current.(*element.TextAreaElement); ok {
			input.SetValue(next.Value)
			input.SetSelectionRange(next.Cursor, next.Cursor)
		}
		setDraft(next)
	}
	handleKeyDown := func(e event.Event) {
		ke, ok := e.(*event.KeyEvent)
		if !ok || props.Disabled {
			return
		}
		kitex.PostMacro(syncDraft)
		current := readDraft()
		isCurrent := current.Value == auto.Result.Input && current.Cursor == auto.Result.Cursor

		switch {
		case ke.MatchString("shift+tab"):
			e.PreventDefault()
			e.StopPropagation()
			if props.OnCycleMode != nil {
				props.OnCycleMode()
			}

		case ke.MatchString("esc", "escape"):
			e.PreventDefault()
			e.StopPropagation()
			if auto.Dismiss() {
				return
			}
			if props.OnCancel != nil {
				props.OnCancel()
			}

		case ke.MatchString("tab"):
			if isCurrent && auto.IsOpen {
				e.PreventDefault()
				e.StopPropagation()
				if !auto.IsLoading && auto.Error == nil && len(auto.Result.Candidates) > 0 {
					if edit, err := auto.Accept(); err == nil {
						replaceDraft(edit)
					}
				}
			}

		case ke.MatchString("up"):
			if isCurrent && auto.Up() {
				e.PreventDefault()
				e.StopPropagation()
				return
			}
			if txa, ok := inputRef.Current.(*element.TextAreaElement); ok {
				if txa.CursorState().Y > 0 {
					return
				}
			}
			e.PreventDefault()
			e.StopPropagation()
			if prevVal, ok := history.Up(current.Value); ok {
				replaceDraft(uicomponents.AutocompleteEdit{Value: prevVal, Cursor: len(prevVal)})
			}

		case ke.MatchString("down"):
			if isCurrent && auto.Down() {
				e.PreventDefault()
				e.StopPropagation()
				return
			}
			if txa, ok := inputRef.Current.(*element.TextAreaElement); ok {
				lineCount := strings.Count(txa.TextContent(), "\n") + 1
				if txa.CursorState().Y < lineCount-1 {
					return
				}
			}
			e.PreventDefault()
			e.StopPropagation()
			if nextVal, ok := history.Down(); ok {
				replaceDraft(uicomponents.AutocompleteEdit{Value: nextVal, Cursor: len(nextVal)})
			}

		case ke.MatchString("shift+enter", "alt+enter"):
			e.PreventDefault()
			e.StopPropagation()
			start, end := current.Cursor, current.Cursor
			if input, ok := inputRef.Current.(*element.TextAreaElement); ok && input.SelectedText() != "" {
				start, end = input.SelectionRange()
			}
			replaceDraft(uicomponents.AutocompleteEdit{
				Value: current.Value[:start] + "\n" + current.Value[end:], Cursor: start + 1,
			})

		case ke.MatchString("enter"):
			e.PreventDefault()
			e.StopPropagation()
			if isCurrent && auto.IsOpen {
				if auto.IsLoading || auto.Error != nil || len(auto.Result.Candidates) == 0 {
					return
				}
				if !selectionMatchesQuery(auto) {
					if edit, err := auto.Accept(); err == nil {
						replaceDraft(edit)
					}
					return
				}
				// The typed token already is the selected candidate (for example
				// "/mode" or "/mode plan"), so accepting would change nothing:
				// submit instead.
			}

			text := strings.TrimSpace(current.Value)
			replaceDraft(uicomponents.AutocompleteEdit{})
			if text != "" {
				history.Add(text)
				if props.OnSubmit != nil {
					props.OnSubmit(text)
				}
			}
		}
	}

	keyHandler := kitex.UseRef[event.Listener](nil)
	keyHandler.Current = handleKeyDown
	kitex.UseEffectCleanup(func() func() {
		if input := inputRef.Current; input != nil {
			selection := input.AddEventListener(event.EventSelectionChange, func(event.Event) { syncDraft() })
			keys := input.AddEventListener(event.EventKeyDown, func(e event.Event) { keyHandler.Current(e) }, event.Capture())
			mouse := input.AddEventListener(event.EventMouseDown, func(event.Event) { kitex.PostMacro(syncDraft) })
			drag := input.AddEventListener(event.EventDrag, func(event.Event) { kitex.PostMacro(syncDraft) })
			return func() {
				selection.Cancel()
				keys.Cancel()
				mouse.Cancel()
				drag.Cancel()
			}
		}
		return nil
	}, []any{})

	composerBox := trail.Box(trail.BoxProps{
		Style: theme.ComposerBorderStyle.
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignStart).
			Width(style.Percent(100)),
	},
		trail.Span(trail.SpanProps{
			Style: theme.UserPromptStyle.Margin(0, 1, 0, 0),
		}, trail.Text(">")),
		kitex.TextArea(kitex.TextAreaProps{
			Ref:      inputRef,
			Value:    draft().Value,
			Disabled: props.Disabled,
			Style: style.S().
				Flex(1).
				Width(style.Percent(100)).
				Height(style.Content).
				MinHeight(style.Cells(1)).
				Scrollbar(style.Scrollbar{Y: style.Some(false)}),
			OnChange: func(event.Event) { syncDraft() },
		}),
	)

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)),
	},
		composerBox,
		uicomponents.Autocomplete(uicomponents.AutocompleteProps{
			State: auto,
			Views: props.CompletionViews,
		}),
	)
})

// selectionMatchesQuery reports whether the token being completed already
// equals the selected candidate's value or one of its aliases. An empty query
// (for example right after "/mode ") never matches, so Enter keeps completing.
func selectionMatchesQuery(auto uicomponents.AutocompleteState) bool {
	candidates := auto.Result.Candidates
	if auto.Selected < 0 || auto.Selected >= len(candidates) || auto.Result.Query == "" {
		return false
	}
	candidate := candidates[auto.Selected]
	if strings.EqualFold(auto.Result.Query, candidate.Value) {
		return true
	}
	for _, alias := range candidate.Aliases {
		if strings.EqualFold(auto.Result.Query, alias) {
			return true
		}
	}
	return false
}
