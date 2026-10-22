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

// ComposerProps defines the properties for the input composer component.
type ComposerProps struct {
	Disabled    bool
	Suggest     func(prefix string) []Suggestion
	SuggestArgs func(rawLine string) ([]Option, bool)
	OnSubmit    func(text string)
	OnCycleMode func()
	OnCancel    func()
}

type argResult struct {
	cmdName string
	opts    []Option
	ok      bool
}

// Composer is a trail functional component managing user prompt input, history navigation,
// slash command autocomplete suggestions, inline argument options, and key shortcuts.
var Composer = trail.FC("Composer", func(props ComposerProps) trail.Node {
	value, setValue := kitex.UseState("")
	history, setHistory := kitex.UseState([]string(nil))
	historyIdx, setHistoryIdx := kitex.UseState(0)
	draft, setDraft := kitex.UseState("")
	argCursor, setArgCursor := kitex.UseState(0)

	inputRef := kitex.UseRef[dom.Element](nil)

	kitex.UseLayoutEffect(func() {
		if !props.Disabled && inputRef.Current != nil {
			if doc := inputRef.Current.OwnerDocument(); doc != nil {
				doc.Focus(inputRef.Current)
			}
		}
	}, []any{props.Disabled})

	val := value()
	trimmedLeft := strings.TrimLeft(val, " \t")

	argRes := kitex.UseMemo(func() argResult {
		if strings.HasPrefix(trimmedLeft, "/") && strings.Contains(trimmedLeft, " ") && props.SuggestArgs != nil {
			cmdPart, _, _ := strings.Cut(trimmedLeft, " ")
			opts, ok := props.SuggestArgs(val)
			if ok {
				return argResult{
					cmdName: cmdPart,
					opts:    opts,
					ok:      true,
				}
			}
		}
		return argResult{}
	}, []any{val})

	suggestions := kitex.UseMemo(func() []Suggestion {
		if strings.HasPrefix(trimmedLeft, "/") && !strings.Contains(trimmedLeft, " ") && props.Suggest != nil {
			return props.Suggest(trimmedLeft)
		}
		return nil
	}, []any{val})

	kitex.UseEffect(func() {
		if argRes.ok && len(argRes.opts) > 0 {
			_, argPart, _ := strings.Cut(trimmedLeft, " ")
			if strings.TrimSpace(argPart) == "" {
				for i, opt := range argRes.opts {
					if opt.Current {
						setArgCursor(i)
						return
					}
				}
				setArgCursor(0)
			} else {
				if cur := argCursor(); cur >= len(argRes.opts) {
					setArgCursor(max(0, len(argRes.opts)-1))
				}
			}
		}
	}, []any{val, len(argRes.opts)})

	curArg := argCursor()
	if len(argRes.opts) > 0 {
		if curArg >= len(argRes.opts) {
			curArg = len(argRes.opts) - 1
		}
		if curArg < 0 {
			curArg = 0
		}
	} else {
		curArg = 0
	}

	setNativeValue := func(next string) {
		if inputRef.Current != nil {
			if txa, ok := inputRef.Current.(*element.TextAreaElement); ok {
				txa.SetValue(next)
			} else if inp, ok := inputRef.Current.(*element.InputElement); ok {
				inp.SetValue(next)
			}
		}
	}

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
			if argRes.ok && len(argRes.opts) > 0 {
				e.PreventDefault()
				e.StopPropagation()
				selectedOpt := argRes.opts[curArg]
				nextVal := argRes.cmdName + " " + selectedOpt.Value + " "
				setNativeValue(nextVal)
				setValue(nextVal)
				return
			}
			if len(suggestions) > 0 {
				e.PreventDefault()
				e.StopPropagation()
				parts := strings.Fields(suggestions[0].Usage)
				if len(parts) > 0 {
					nextVal := parts[0] + " "
					setNativeValue(nextVal)
					setValue(nextVal)
				}
			}

		case ke.MatchString("up"):
			if argRes.ok && len(argRes.opts) > 0 {
				e.PreventDefault()
				e.StopPropagation()
				if curArg > 0 {
					setArgCursor(curArg - 1)
				}
				return
			}
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
					setNativeValue(hist[newIdx])
					setValue(hist[newIdx])
				} else if hIdx > 0 {
					newIdx := hIdx - 1
					setHistoryIdx(newIdx)
					setNativeValue(hist[newIdx])
					setValue(hist[newIdx])
				}
			}

		case ke.MatchString("down"):
			if argRes.ok && len(argRes.opts) > 0 {
				e.PreventDefault()
				e.StopPropagation()
				if curArg < len(argRes.opts)-1 {
					setArgCursor(curArg + 1)
				}
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
			hist := history()
			if len(hist) > 0 {
				hIdx := historyIdx()
				if hIdx < len(hist)-1 {
					newIdx := hIdx + 1
					setHistoryIdx(newIdx)
					setNativeValue(hist[newIdx])
					setValue(hist[newIdx])
				} else if hIdx == len(hist)-1 {
					setHistoryIdx(len(hist))
					d := draft()
					setNativeValue(d)
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
			if argRes.ok && len(argRes.opts) > 0 {
				selectedOpt := argRes.opts[curArg]
				subText := argRes.cmdName + " " + selectedOpt.Value
				setNativeValue("")
				setValue("")
				hist := append(history(), subText)
				setHistory(hist)
				setHistoryIdx(len(hist))
				setDraft("")
				if props.OnSubmit != nil {
					props.OnSubmit(subText)
				}
				return
			}

			text := strings.TrimSpace(value())
			setNativeValue("")
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
		kitex.If(argRes.ok && len(argRes.opts) > 0, func() trail.Node {
			var trs []trail.Node
			for i, opt := range argRes.opts {
				rowStyle := theme.PickerOptionStyle
				prefix := "  "
				if i == curArg {
					rowStyle = theme.PickerSelectedStyle
					prefix = "❯ "
				}
				desc := opt.Description
				if opt.Current {
					if desc != "" {
						desc += " • current"
					} else {
						desc = "current"
					}
				}

				td1 := kitex.TD(kitex.TDProps{
					Style: style.S().Padding(0, 2, 0, 0).WhiteSpace(style.WhiteSpacePre),
				}, trail.Span(trail.SpanProps{
					Style: rowStyle.WhiteSpace(style.WhiteSpacePre),
				}, trail.Text(prefix+opt.Label)))

				td2 := kitex.TD(kitex.TDProps{
					Style: style.S(),
				}, trail.Span(trail.SpanProps{
					Style: theme.StatusBarStyle,
				}, trail.Text(desc)))

				trs = append(trs, kitex.TR(kitex.TRProps{}, td1, td2))
			}

			return kitex.Table(kitex.TableProps{
				Style: style.S().Width(style.Percent(100)),
			}, kitex.TBody(kitex.TBodyProps{}, trs...))
		}),
		kitex.If(!argRes.ok && len(suggestions) > 0, func() trail.Node {
			var trs []trail.Node
			for _, s := range suggestions {
				td1 := kitex.TD(kitex.TDProps{
					Style: style.S().Padding(0, 2, 0, 0).WhiteSpace(style.WhiteSpacePre),
				}, trail.Span(trail.SpanProps{
					Style: theme.StatusBarStyle.WhiteSpace(style.WhiteSpacePre),
				}, trail.Text("  "+s.Usage)))

				td2 := kitex.TD(kitex.TDProps{
					Style: style.S(),
				}, trail.Span(trail.SpanProps{
					Style: theme.StatusBarStyle,
				}, trail.Text(s.Description)))

				trs = append(trs, kitex.TR(kitex.TRProps{}, td1, td2))
			}

			return kitex.Table(kitex.TableProps{
				Style: style.S().Width(style.Percent(100)),
			}, kitex.TBody(kitex.TBodyProps{}, trs...))
		}),
	)
})
