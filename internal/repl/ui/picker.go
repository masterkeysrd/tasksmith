package ui

import (
	"maps"
	"slices"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// DefaultPickerFooter is the default navigation hint displayed at the bottom of a picker.
const DefaultPickerFooter = "↑/↓ or j/k to select • enter to confirm • esc to cancel"

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

type pickerModalState struct {
	mode          pickerMode
	targetOpt     Option
	activeConfirm ConfirmAction
	activeInput   InputAction
	inputVal      string
}

// PickerProps defines the properties for the interactive Picker component.
type PickerProps struct {
	Title          string
	Options        []Option
	Footer         string // Optional navigation hint displayed in the picker footer
	Filterable     bool
	OnOpen         func()
	OnSelect       func(opt Option) any
	Keys           map[string]func(opt Option) any
	ConfirmActions map[string]ConfirmAction
	InputActions   map[string]InputAction
	OnAction       func(action any, closePicker bool)
	OnCancel       func()
}

// NewPicker constructs an initialized PickerProps with default footer and cursor at current.
func NewPicker(title string, opts []Option, onSelect func(opt Option) any) PickerProps {
	return PickerProps{
		Title:    title,
		Options:  opts,
		Footer:   DefaultPickerFooter,
		OnSelect: onSelect,
	}
}

func initialCursor(opts []Option) int {
	for i, opt := range opts {
		if opt.Current {
			return i
		}
	}
	return 0
}

// withCurrentSuffix appends " • current" (or returns "current" if text is empty) when current is true.
func withCurrentSuffix(text string, current bool) string {
	if !current {
		return text
	}
	if text != "" {
		return text + " • current"
	}
	return "current"
}

// filterOptions filters opts against query by checking Label, Description, Value,
// and Columns case-insensitively. It returns opts directly if query is empty.
func filterOptions(opts []Option, query string) []Option {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return opts
	}
	var res []Option
	for _, opt := range opts {
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
}

// renderOptionsTable renders the option rows as a table or empty message.
func renderOptionsTable(filtered []Option, cur int) trail.Node {
	if len(filtered) == 0 {
		return trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{
			Style: theme.PickerOptionStyle,
		}, trail.Text("  No matching options.")))
	}

	hasColumns := len(filtered[0].Columns) > 0
	trs := make([]trail.Node, 0, len(filtered))

	for i, opt := range filtered {
		rowStyle := theme.PickerOptionStyle
		prefix := "  "
		if i == cur {
			rowStyle = theme.PickerSelectedStyle
			prefix = "❯ "
		}

		var tds []trail.Node
		if hasColumns && len(opt.Columns) > 0 {
			cols := slices.Clone(opt.Columns)
			last := len(cols) - 1
			cols[last] = withCurrentSuffix(cols[last], opt.Current)

			tds = append(tds, kitex.TD(kitex.TDProps{
				Style: style.S().Padding(0, 2, 0, 0).WhiteSpace(style.WhiteSpacePre),
			}, trail.Span(trail.SpanProps{
				Style: rowStyle.WhiteSpace(style.WhiteSpacePre),
			}, trail.Text(prefix+cols[0]))))

			for _, col := range cols[1:] {
				tds = append(tds, kitex.TD(kitex.TDProps{
					Style: style.S().Padding(0, 1, 0, 1),
				}, trail.Span(trail.SpanProps{Style: rowStyle}, trail.Text(col))))
			}
		} else {
			tds = []trail.Node{
				kitex.TD(kitex.TDProps{
					Style: style.S().Padding(0, 2, 0, 0).WhiteSpace(style.WhiteSpacePre),
				}, trail.Span(trail.SpanProps{
					Style: rowStyle.WhiteSpace(style.WhiteSpacePre),
				}, trail.Text(prefix+opt.Label))),
				kitex.TD(kitex.TDProps{
					Style: style.S(),
				}, trail.Span(trail.SpanProps{
					Style: theme.StatusBarStyle,
				}, trail.Text(withCurrentSuffix(opt.Description, opt.Current)))),
			}
		}

		trs = append(trs, kitex.TR(kitex.TRProps{}, tds...))
	}

	return kitex.Table(kitex.TableProps{
		Style: style.S().Width(style.Percent(100)),
	}, kitex.TBody(kitex.TBodyProps{}, trs...))
}

// Picker is a trail functional component managing interactive item selection,
// filtering, custom actions (e.g. rename, delete), and keyboard navigation.
var Picker = trail.FC("Picker", func(props PickerProps) trail.Node {
	cursor, setCursor := kitex.UseState(initialCursor(props.Options))
	filter, setFilter := kitex.UseState("")
	modal, setModal := kitex.UseState(pickerModalState{mode: pickerModeBrowse})

	filterInputRef := kitex.UseRef[dom.Element](nil)
	actionInputRef := kitex.UseRef[dom.Element](nil)

	kitex.UseEffect(func() {
		if props.OnOpen != nil {
			props.OnOpen()
		}
	}, nil)

	kitex.UseLayoutEffect(func() {
		st := modal()
		if st.mode == pickerModeBrowse && props.Filterable && filterInputRef.Current != nil {
			if doc := filterInputRef.Current.OwnerDocument(); doc != nil {
				doc.Focus(filterInputRef.Current)
			}
		} else if st.mode == pickerModeInput && actionInputRef.Current != nil {
			if doc := actionInputRef.Current.OwnerDocument(); doc != nil {
				doc.Focus(actionInputRef.Current)
			}
		}
	}, []any{modal().mode, props.Filterable})

	filtered := kitex.UseMemo(func() []Option {
		return filterOptions(props.Options, filter())
	}, []any{props.Options, filter()})

	// Cursor reconciliation when filtered list changes or shrinks
	kitex.UseEffect(func() {
		cur := cursor()
		if len(filtered) == 0 {
			if cur != 0 {
				setCursor(0)
			}
		} else if cur >= len(filtered) {
			setCursor(len(filtered) - 1)
		}
	}, []any{len(filtered)})

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

		// Check confirm actions (sorted for deterministic evaluation)
		for _, k := range slices.Sorted(maps.Keys(props.ConfirmActions)) {
			if ke.MatchString(k) && len(filtered) > 0 {
				setModal(pickerModalState{
					mode:          pickerModeConfirm,
					targetOpt:     filtered[cur],
					activeConfirm: props.ConfirmActions[k],
				})
				return true
			}
		}

		// Check input actions (sorted for deterministic evaluation)
		for _, k := range slices.Sorted(maps.Keys(props.InputActions)) {
			if ke.MatchString(k) && len(filtered) > 0 {
				ia := props.InputActions[k]
				initVal := ""
				if ia.InitialValue != nil {
					initVal = ia.InitialValue(filtered[cur])
				}
				setModal(pickerModalState{
					mode:        pickerModeInput,
					targetOpt:   filtered[cur],
					activeInput: ia,
					inputVal:    initVal,
				})
				return true
			}
		}

		// Check custom keys (sorted for deterministic evaluation)
		for _, k := range slices.Sorted(maps.Keys(props.Keys)) {
			if ke.MatchString(k) && len(filtered) > 0 {
				act := props.Keys[k](filtered[cur])
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
		st := modal()
		if st.mode == pickerModeConfirm {
			if input == "y" || input == "Y" || k.MatchString("y", "enter") {
				if st.activeConfirm.OnConfirm != nil {
					act := st.activeConfirm.OnConfirm(st.targetOpt)
					if props.OnAction != nil {
						props.OnAction(act, false)
					}
				}
				setModal(pickerModalState{mode: pickerModeBrowse})
				return
			}
			if input == "n" || input == "N" || k.MatchString("n", "esc", "escape") {
				setModal(pickerModalState{mode: pickerModeBrowse})
				return
			}
			return
		}

		if st.mode == pickerModeBrowse && !props.Filterable {
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
	st := modal()
	if props.Filterable && st.mode == pickerModeBrowse {
		rows = append(rows, trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Width(style.Percent(100)),
		},
			trail.Span(trail.SpanProps{
				Style: theme.PickerTitleStyle.Margin(0, 1, 0, 0).Flex(0, 0).WhiteSpace(style.WhiteSpacePre),
			}, trail.Text("Filter:")),
			kitex.Input(kitex.InputProps{
				Ref:   filterInputRef,
				Value: filter(),
				Style: style.S().Flex(1),
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
	rows = append(rows, renderOptionsTable(filtered, cur))

	// Inline prompt mode (confirm or input)
	switch st.mode {
	case pickerModeConfirm:
		promptText := ""
		if st.activeConfirm.Prompt != nil {
			promptText = st.activeConfirm.Prompt(st.targetOpt)
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
				Style: style.S().Foreground(theme.ColorCyan).Bold(true).Margin(0, 1, 0, 0).Flex(0, 0).WhiteSpace(style.WhiteSpacePre),
			}, trail.Text(st.activeInput.Prompt)),
			kitex.Input(kitex.InputProps{
				Ref:   actionInputRef,
				Value: st.inputVal,
				Style: style.S().Flex(1),
				OnChange: func(e event.Event) {
					if ie, ok := e.(*event.InputEvent); ok {
						current := modal()
						current.inputVal = ie.Value
						setModal(current)
					}
				},
				OnKeyDown: func(e event.Event) {
					if ke, ok := e.(*event.KeyEvent); ok {
						switch {
						case ke.MatchString("enter"):
							e.PreventDefault()
							e.StopPropagation()
							current := modal()
							if current.activeInput.OnSubmit != nil {
								act := current.activeInput.OnSubmit(current.targetOpt, current.inputVal)
								if props.OnAction != nil {
									props.OnAction(act, false)
								}
							}
							setModal(pickerModalState{mode: pickerModeBrowse})

						case ke.MatchString("esc", "escape"):
							e.PreventDefault()
							e.StopPropagation()
							setModal(pickerModalState{mode: pickerModeBrowse})
						}
					}
				},
			}),
		))
	}

	// Footer navigation hint
	footerHint := props.Footer
	if footerHint == "" {
		footerHint = DefaultPickerFooter
	}
	switch st.mode {
	case pickerModeConfirm:
		footerHint = "y/enter to confirm • n/esc to cancel"
	case pickerModeInput:
		footerHint = "enter to submit • esc to cancel"
	}

	rows = append(rows, trail.Newline(1), trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow),
	}, trail.Span(trail.SpanProps{
		Style: theme.StatusBarStyle,
	}, trail.Text(footerHint))))

	// Trailing blank line before global status bar
	rows = append(rows, trail.Newline(1))

	return trail.Box(trail.BoxProps{
		Style: theme.PickerTopBorderStyle.
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	}, rows...)
})
