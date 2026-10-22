package ui

import (
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// DefaultPickerStatusBar is the default navigation hint displayed at the bottom of a picker.
const DefaultPickerStatusBar = "↑/↓ or j/k to select • enter to confirm • esc to cancel"

// PickerCanceledMsg indicates that a picker interaction was dismissed without making a selection.
type PickerCanceledMsg struct{}

// Option represents a selectable item in an interactive command picker.
type Option struct {
	Value       string
	Label       string
	Description string
	Columns     []string
	Current     bool
}

// ConfirmAction defines an inline confirmation prompt triggered by a custom key.
type ConfirmAction struct {
	Prompt    func(opt Option) string
	OnConfirm func(opt Option) any
}

// InputAction defines an inline text input prompt triggered by a custom key.
type InputAction struct {
	Prompt       string
	InitialValue func(opt Option) string
	OnSubmit     func(opt Option, value string) any
}

type pickerMode int

const (
	pickerModeBrowse pickerMode = iota
	pickerModeConfirm
	pickerModeInput
)

// PickerProps defines the properties for the interactive Picker component.
type PickerProps struct {
	Title          string
	Options        []Option
	StatusBar      string
	Filterable     bool
	OnSelect       func(opt Option) any
	Keys           map[string]func(opt Option) any
	ConfirmActions map[string]ConfirmAction
	InputActions   map[string]InputAction
	OnAction       func(action any, closePicker bool)
	OnCancel       func()
}

// NewPicker constructs an initialized PickerProps with default status bar and cursor at current.
func NewPicker(title string, opts []Option, onSelect func(opt Option) any) PickerProps {
	return PickerProps{
		Title:     title,
		Options:   opts,
		StatusBar: DefaultPickerStatusBar,
		OnSelect:  onSelect,
	}
}

// Select invokes OnSelect on the currently selected or first option.
func (p PickerProps) Select() any {
	if p.OnSelect == nil {
		return nil
	}
	opt, ok := p.SelectedOption()
	if !ok {
		return nil
	}
	return p.OnSelect(opt)
}

// SelectedOption returns the currently active option, or the first option if none is marked Current.
func (p PickerProps) SelectedOption() (Option, bool) {
	if len(p.Options) == 0 {
		return Option{}, false
	}
	for _, opt := range p.Options {
		if opt.Current {
			return opt, true
		}
	}
	return p.Options[0], true
}

func initialCursor(opts []Option) int {
	for i, opt := range opts {
		if opt.Current {
			return i
		}
	}
	return 0
}

// Picker is a trail functional component managing interactive item selection,
// filtering, custom actions (e.g. rename, delete), and keyboard navigation.
var Picker = trail.FC("Picker", func(props PickerProps) trail.Node {
	cursor, setCursor := kitex.UseState(initialCursor(props.Options))
	filter, setFilter := kitex.UseState("")
	mode, setMode := kitex.UseState(pickerModeBrowse)
	targetOpt, setTargetOpt := kitex.UseState(Option{})
	activeConfirm, setActiveConfirm := kitex.UseState(ConfirmAction{})
	activeInput, setActiveInput := kitex.UseState(InputAction{})
	inputVal, setInputVal := kitex.UseState("")

	filterInputRef := kitex.UseRef[dom.Element](nil)
	actionInputRef := kitex.UseRef[dom.Element](nil)

	kitex.UseLayoutEffect(func() {
		if mode() == pickerModeBrowse && props.Filterable && filterInputRef.Current != nil {
			if doc := filterInputRef.Current.OwnerDocument(); doc != nil {
				doc.Focus(filterInputRef.Current)
			}
		} else if mode() == pickerModeInput && actionInputRef.Current != nil {
			if doc := actionInputRef.Current.OwnerDocument(); doc != nil {
				doc.Focus(actionInputRef.Current)
			}
		}
	}, []any{mode(), props.Filterable})

	filtered := kitex.UseMemo(func() []Option {
		q := strings.TrimSpace(strings.ToLower(filter()))
		if q == "" {
			return props.Options
		}
		var res []Option
		for _, opt := range props.Options {
			if strings.Contains(strings.ToLower(opt.Label), q) ||
				strings.Contains(strings.ToLower(opt.Description), q) ||
				strings.Contains(strings.ToLower(opt.Value), q) {
				res = append(res, opt)
				continue
			}
			match := false
			for _, col := range opt.Columns {
				if strings.Contains(strings.ToLower(col), q) {
					match = true
					break
				}
			}
			if match {
				res = append(res, opt)
			}
		}
		return res
	}, []any{props.Options, filter()})

	cur := cursor()
	if len(filtered) > 0 {
		if cur >= len(filtered) {
			cur = len(filtered) - 1
		}
		if cur < 0 {
			cur = 0
		}
	} else {
		cur = 0
	}

	handleBrowseKey := func(ke event.KeyEvent) bool {
		switch {
		case ke.MatchString("up", "k"):
			if cur > 0 {
				setCursor(cur - 1)
			}
			return true

		case ke.MatchString("down", "j"):
			if cur < len(filtered)-1 {
				setCursor(cur + 1)
			}
			return true

		case ke.MatchString("enter"):
			if len(filtered) > 0 && props.OnSelect != nil {
				act := props.OnSelect(filtered[cur])
				if props.OnAction != nil {
					props.OnAction(act, true)
				}
			}
			return true

		case ke.MatchString("esc", "escape"):
			if props.OnCancel != nil {
				props.OnCancel()
			}
			return true
		}

		// Check confirm actions
		for k, ca := range props.ConfirmActions {
			if ke.MatchString(k) && len(filtered) > 0 {
				setTargetOpt(filtered[cur])
				setActiveConfirm(ca)
				setMode(pickerModeConfirm)
				return true
			}
		}

		// Check input actions
		for k, ia := range props.InputActions {
			if ke.MatchString(k) && len(filtered) > 0 {
				setTargetOpt(filtered[cur])
				setActiveInput(ia)
				initVal := ""
				if ia.InitialValue != nil {
					initVal = ia.InitialValue(filtered[cur])
				}
				setInputVal(initVal)
				setMode(pickerModeInput)
				return true
			}
		}

		// Check custom keys
		for k, fn := range props.Keys {
			if ke.MatchString(k) && len(filtered) > 0 {
				act := fn(filtered[cur])
				if props.OnAction != nil {
					props.OnAction(act, false)
				}
				return true
			}
		}

		return false
	}

	// trail.UseInput for non-input navigation or confirm mode
	trail.UseInput(func(input string, k event.KeyEvent) {
		m := mode()
		if m == pickerModeConfirm {
			if input == "y" || input == "Y" || k.MatchString("y", "enter") {
				ca := activeConfirm()
				if ca.OnConfirm != nil {
					act := ca.OnConfirm(targetOpt())
					if props.OnAction != nil {
						props.OnAction(act, false)
					}
				}
				setMode(pickerModeBrowse)
				return
			}
			if input == "n" || input == "N" || k.MatchString("n", "esc", "escape") {
				setMode(pickerModeBrowse)
				return
			}
			return
		}

		if m == pickerModeBrowse && !props.Filterable {
			handleBrowseKey(k)
		}
	})

	var rows []trail.Node

	// Title
	if props.Title != "" {
		rows = append(rows, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{
			Style: theme.PickerTitleStyle,
		}, trail.Text(props.Title))))
	}

	// Live filter input row
	if props.Filterable && mode() == pickerModeBrowse {
		rows = append(rows, trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Width(style.Percent(100)),
		},
			trail.Span(trail.SpanProps{
				Style: theme.PickerTitleStyle.Margin(0, 1, 0, 0),
			}, trail.Text("Filter:")),
			kitex.Input(kitex.InputProps{
				Ref:   filterInputRef,
				Value: filter(),
				Style: style.S().Flex(1).Width(style.Percent(100)),
				OnChange: func(e event.Event) {
					if ie, ok := e.(*event.InputEvent); ok {
						setFilter(ie.Value)
						setCursor(0)
					}
				},
				OnKeyDown: func(e event.Event) {
					if ke, ok := e.(*event.KeyEvent); ok {
						if handleBrowseKey(*ke) {
							e.PreventDefault()
							e.StopPropagation()
						}
					}
				},
			}),
		))
	}

	// Options rendering
	if len(filtered) == 0 {
		rows = append(rows, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{
			Style: theme.PickerOptionStyle,
		}, trail.Text("  No matching options."))))
	} else if len(filtered[0].Columns) > 0 {
		var trs []trail.Node
		for i, opt := range filtered {
			rowStyle := theme.PickerOptionStyle
			prefix := "  "
			if i == cur {
				rowStyle = theme.PickerSelectedStyle
				prefix = "❯ "
			}
			cols := make([]string, len(opt.Columns))
			copy(cols, opt.Columns)
			if opt.Current && len(cols) > 0 {
				last := len(cols) - 1
				if cols[last] != "" {
					cols[last] += " • current"
				} else {
					cols[last] = "current"
				}
			}

			var tds []trail.Node
			tds = append(tds, kitex.TD(kitex.TDProps{
				Style: style.S().Padding(0, 1, 0, 0),
			}, trail.Span(trail.SpanProps{Style: rowStyle}, trail.Text(prefix+cols[0]))))

			for _, col := range cols[1:] {
				tds = append(tds, kitex.TD(kitex.TDProps{
					Style: style.S().Padding(0, 1, 0, 1),
				}, trail.Span(trail.SpanProps{Style: rowStyle}, trail.Text(col))))
			}

			trs = append(trs, kitex.TR(kitex.TRProps{}, tds...))
		}

		rows = append(rows, kitex.Table(kitex.TableProps{
			Style: style.S().Width(style.Percent(100)),
		}, kitex.TBody(kitex.TBodyProps{}, trs...)))
	} else {
		rows = append(rows, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexColumn).Width(style.Percent(100)),
		},
			kitex.Map(filtered, func(opt Option, i int) trail.Node {
				labelStyle := theme.PickerOptionStyle
				prefix := "  "
				if i == cur {
					labelStyle = theme.PickerSelectedStyle
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
				var spans []trail.Node
				spans = append(spans, trail.Span(trail.SpanProps{
					Style: labelStyle,
				}, trail.Text(prefix+opt.Label)))
				if desc != "" {
					spans = append(spans, trail.Span(trail.SpanProps{
						Style: theme.StatusBarStyle,
					}, trail.Text("  "+desc)))
				}
				return trail.Box(trail.BoxProps{
					Style: style.S().FlexDirection(style.FlexRow),
				}, spans...)
			}),
		))
	}

	// Inline prompt mode (confirm or input)
	switch mode() {
	case pickerModeConfirm:
		promptText := ""
		if activeConfirm().Prompt != nil {
			promptText = activeConfirm().Prompt(targetOpt())
		}
		rows = append(rows, trail.Newline(1), trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		},
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(theme.ColorYellow).Bold(true),
			}, trail.Text("? "+promptText+" ")),
			trail.Span(trail.SpanProps{
				Style: theme.StatusBarStyle,
			}, trail.Text("(y/N)")),
		))

	case pickerModeInput:
		rows = append(rows, trail.Newline(1), trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Width(style.Percent(100)),
		},
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(theme.ColorCyan).Bold(true).Margin(0, 1, 0, 0),
			}, trail.Text(strings.TrimRight(activeInput().Prompt, " "))),
			kitex.Input(kitex.InputProps{
				Ref:   actionInputRef,
				Value: inputVal(),
				Style: style.S().Flex(1).Width(style.Percent(100)),
				OnChange: func(e event.Event) {
					if ie, ok := e.(*event.InputEvent); ok {
						setInputVal(ie.Value)
					}
				},
				OnKeyDown: func(e event.Event) {
					if ke, ok := e.(*event.KeyEvent); ok {
						switch {
						case ke.MatchString("enter"):
							e.PreventDefault()
							e.StopPropagation()
							ia := activeInput()
							if ia.OnSubmit != nil {
								act := ia.OnSubmit(targetOpt(), inputVal())
								if props.OnAction != nil {
									props.OnAction(act, false)
								}
							}
							setMode(pickerModeBrowse)

						case ke.MatchString("esc", "escape"):
							e.PreventDefault()
							e.StopPropagation()
							setMode(pickerModeBrowse)
						}
					}
				},
			}),
			trail.Span(trail.SpanProps{
				Style: theme.StatusBarStyle,
			}, trail.Text(" (enter to submit, esc to cancel)")),
		))
	}

	// Trailing blank line before global status bar
	rows = append(rows, trail.Newline(1))

	return trail.Box(trail.BoxProps{
		Style: theme.PickerTopBorderStyle.
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	}, rows...)
})
