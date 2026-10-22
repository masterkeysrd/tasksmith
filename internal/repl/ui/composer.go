package ui

import (
	"fmt"
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

// ComposerProps defines the properties for the input composer component.
type ComposerProps struct {
	Disabled    bool
	Suggest     func(prefix string) []Suggestion
	OnSubmit    func(text string)
	OnCycleMode func()
	OnCancel    func()
}

// Composer is a trail functional component managing user prompt input, history navigation,
// slash command autocomplete suggestions, and key shortcuts.
var Composer = trail.FC("Composer", func(props ComposerProps) trail.Node {
	value, setValue := kitex.UseState("")
	history, setHistory := kitex.UseState([]string(nil))
	historyIdx, setHistoryIdx := kitex.UseState(0)
	draft, setDraft := kitex.UseState("")

	inputRef := kitex.UseRef[dom.Element](nil)

	kitex.UseLayoutEffect(func() {
		if !props.Disabled && inputRef.Current != nil {
			if doc := inputRef.Current.OwnerDocument(); doc != nil {
				doc.Focus(inputRef.Current)
			}
		}
	}, []any{props.Disabled})

	val := value()
	suggestions := kitex.UseMemo(func() []Suggestion {
		trimmed := strings.TrimSpace(val)
		if strings.HasPrefix(trimmed, "/") && props.Suggest != nil {
			return props.Suggest(trimmed)
		}
		return nil
	}, []any{val})

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
			if len(suggestions) > 0 {
				e.PreventDefault()
				e.StopPropagation()
				parts := strings.Fields(suggestions[0].Usage)
				if len(parts) > 0 {
					nextVal := parts[0] + " "
					if txa, ok := inputRef.Current.(*element.TextAreaElement); ok {
						txa.SetValue(nextVal)
					}
					setValue(nextVal)
				}
			}

		case ke.MatchString("up"):
			if txa, ok := inputRef.Current.(*element.TextAreaElement); ok {
				if txa.CursorState().Y > 0 {
					return
				}
			}
			e.PreventDefault()
			e.StopPropagation()
			hist := history()
			if len(hist) > 0 {
				hIdx := historyIdx()
				if hIdx >= len(hist) {
					setDraft(value())
					newIdx := len(hist) - 1
					setHistoryIdx(newIdx)
					if txa, ok := inputRef.Current.(*element.TextAreaElement); ok {
						txa.SetValue(hist[newIdx])
					}
					setValue(hist[newIdx])
				} else if hIdx > 0 {
					newIdx := hIdx - 1
					setHistoryIdx(newIdx)
					if txa, ok := inputRef.Current.(*element.TextAreaElement); ok {
						txa.SetValue(hist[newIdx])
					}
					setValue(hist[newIdx])
				}
			}

		case ke.MatchString("down"):
			if txa, ok := inputRef.Current.(*element.TextAreaElement); ok {
				lineCount := strings.Count(txa.TextContent(), "\n") + 1
				if txa.CursorState().Y < lineCount-1 {
					return
				}
			}
			e.PreventDefault()
			e.StopPropagation()
			hist := history()
			if len(hist) > 0 {
				hIdx := historyIdx()
				if hIdx < len(hist)-1 {
					newIdx := hIdx + 1
					setHistoryIdx(newIdx)
					if txa, ok := inputRef.Current.(*element.TextAreaElement); ok {
						txa.SetValue(hist[newIdx])
					}
					setValue(hist[newIdx])
				} else if hIdx == len(hist)-1 {
					setHistoryIdx(len(hist))
					d := draft()
					if txa, ok := inputRef.Current.(*element.TextAreaElement); ok {
						txa.SetValue(d)
					}
					setValue(d)
				}
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
			text := strings.TrimSpace(value())
			if txa, ok := inputRef.Current.(*element.TextAreaElement); ok {
				txa.SetValue("")
			}
			setValue("")
			if text != "" {
				hist := append(history(), text)
				setHistory(hist)
				setHistoryIdx(len(hist))
				setDraft("")
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
		kitex.If(len(suggestions) > 0, func() trail.Node {
			return trail.Box(trail.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Width(style.Percent(100)),
			},
				kitex.Map(suggestions, func(s Suggestion, _ int) trail.Node {
					return trail.Box(trail.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow),
					},
						trail.Span(trail.SpanProps{
							Style: theme.StatusBarStyle,
						}, trail.Text(fmt.Sprintf("  %-38s %s", s.Usage, s.Description))),
					)
				}),
			)
		}),
	)
})
