package ui

import (
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// Suggestion describes an autocomplete hint for a slash command.
type Suggestion struct {
	Usage       string
	Description string
}

// ComposerProps defines the properties for the Composer functional component.
type ComposerProps struct {
	Disabled    bool
	Suggest     func(prefix string) []Suggestion
	SuggestArgs func(rawLine string) ([]Option, bool)
	OnSubmit    func(text string)
	OnCycleMode func()
	OnCancel    func()
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
// command autocomplete, prompt history navigation, and mode cycling.
var Composer = trail.FC("Composer", func(props ComposerProps) trail.Node {
	value, setValue := kitex.UseState("")
	history := usePromptHistory()
	auto := useAutocomplete(value(), props.Suggest, props.SuggestArgs)

	inputRef := kitex.UseRef[dom.Element](nil)
	trail.UseFocus(inputRef, trail.FocusOptions{
		AutoFocus: true,
		IsActive:  !props.Disabled,
	})

	handleKeyDown := func(e event.Event) {
		ke, ok := e.(*event.KeyEvent)
		if !ok {
			return
		}

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
			if props.OnCancel != nil {
				props.OnCancel()
			}

		case ke.MatchString("tab"):
			if nextVal, ok := auto.Tab(); ok {
				e.PreventDefault()
				e.StopPropagation()
				setValue(nextVal)
			}

		case ke.MatchString("up"):
			if auto.Up() {
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
			if prevVal, ok := history.Up(value()); ok {
				setValue(prevVal)
			}

		case ke.MatchString("down"):
			if auto.Down() {
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
				setValue(nextVal)
			}

		case ke.MatchString("shift+enter", "alt+enter"):
			e.PreventDefault()
			e.StopPropagation()
			if inputRef.Current != nil {
				setValue(inputRef.Current.TextContent())
			}

		case ke.MatchString("enter"):
			e.PreventDefault()
			e.StopPropagation()
			if subText, ok := auto.Enter(); ok {
				setValue("")
				history.Add(subText)
				if props.OnSubmit != nil {
					props.OnSubmit(subText)
				}
				return
			}

			text := strings.TrimSpace(value())
			setValue("")
			if text != "" {
				history.Add(text)
				if props.OnSubmit != nil {
					props.OnSubmit(text)
				}
			}
		}
	}

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
			Value:    value(),
			Disabled: props.Disabled,
			Style: style.S().
				Flex(1).
				Width(style.Percent(100)).
				Height(style.Content).
				MinHeight(style.Cells(1)).
				Scrollbar(style.Scrollbar{Y: style.Some(false)}),
			OnChange: func(e event.Event) {
				if ie, ok := e.(*event.InputEvent); ok {
					setValue(ie.Value)
				}
			},
			OnKeyDown: handleKeyDown,
		}),
	)

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)),
	},
		composerBox,
		Autocomplete(AutocompleteProps{
			Value:       value(),
			Suggest:     props.Suggest,
			SuggestArgs: props.SuggestArgs,
			Selected:    auto.Cursor,
		}),
	)
})
