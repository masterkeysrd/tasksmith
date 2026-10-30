package components

import (
	"maps"
	"slices"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// DefaultPickerBindings defines the default keyboard shortcuts displayed on the picker.
var DefaultPickerBindings = []components.KeyBinding{
	{Key: "↑/↓", Label: "Navigate"},
	{Key: "enter", Label: "Confirm"},
	{Key: "esc", Label: "Cancel"},
}

// ConfirmAction defines an inline confirmation prompt triggered by a custom key.
type ConfirmAction struct {
	Prompt         func(opt components.Option) string
	DisabledReason func(opt components.Option) string
	OnConfirm      func(opt components.Option)
}

// InputAction defines an inline text input prompt triggered by a custom key.
type InputAction struct {
	Prompt       string
	InitialValue func(opt components.Option) string
	OnSubmit     func(opt components.Option, value string)
}

type pickerMode int

const (
	pickerModeBrowse pickerMode = iota
	pickerModeConfirm
	pickerModeInput
)

type pickerModalState struct {
	mode          pickerMode
	targetOpt     components.Option
	activeConfirm ConfirmAction
	activeInput   InputAction
	inputVal      string
}

// PickerProps defines the properties for the interactive Picker component.
type PickerProps struct {
	Title          string
	Header         trail.Node // Optional custom header row (e.g. column headers)
	Options        []components.Option
	MaxVisible     int                     // Max visible options before scrolling (defaults to components.DefaultPickerMaxVisible)
	Columns        []components.Column     // Column specifications forwarded to OptionList
	Bindings       []components.KeyBinding // Optional navigation shortcuts rendered inline via KeyboardHint
	Filterable     bool
	OnOpen         func()
	OnSelect       func(opt components.Option)
	Keys           map[string]func(opt components.Option)
	ConfirmActions map[string]ConfirmAction
	InputActions   map[string]InputAction
	OnCancel       func()
}

// NewPicker constructs an initialized PickerProps with default bindings and cursor at current.
func NewPicker(title string, opts []components.Option, onSelect func(opt components.Option)) PickerProps {
	return PickerProps{
		Title:    title,
		Options:  opts,
		Bindings: DefaultPickerBindings,
		OnSelect: onSelect,
	}
}

func initialCursor(opts []components.Option) int {
	for i, opt := range opts {
		if opt.Current {
			return i
		}
	}
	return 0
}

func filterOptions(opts []components.Option, query string) []components.Option {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return opts
	}
	var res []components.Option
	for _, opt := range opts {
		if strings.Contains(strings.ToLower(opt.Label), q) ||
			strings.Contains(strings.ToLower(opt.Description), q) ||
			strings.Contains(strings.ToLower(opt.Value), q) {
			res = append(res, opt)
			continue
		}
		match := false
		for _, cell := range opt.Cells {
			if strings.Contains(strings.ToLower(cell), q) {
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

// Picker is a trail functional component managing interactive item selection,
// filtering, custom actions, and keyboard navigation.
var Picker = trail.FC("Picker", func(props PickerProps) trail.Node {
	scheme := theme.UseTheme()

	cursor, setCursor := kitex.UseState(initialCursor(props.Options))
	filter, setFilter := kitex.UseState("")
	modal, setModal := kitex.UseState(pickerModalState{mode: pickerModeBrowse})
	notice, setNotice := kitex.UseState("")

	filterInputRef := kitex.UseRef[dom.Element](nil)
	actionInputRef := kitex.UseRef[dom.Element](nil)

	kitex.UseEffect(func() {
		if props.OnOpen != nil {
			props.OnOpen()
		}
	}, nil)

	st := modal()
	trail.UseFocus(filterInputRef, trail.FocusOptions{
		AutoFocus: true,
		IsActive:  props.Filterable && st.mode == pickerModeBrowse,
	})
	trail.UseFocus(actionInputRef, trail.FocusOptions{
		AutoFocus: true,
		IsActive:  st.mode == pickerModeInput,
	})

	filtered := kitex.UseMemo(func() []components.Option {
		return filterOptions(props.Options, filter())
	}, []any{props.Options, filter()})

	// Reconcile cursor when filtered length shrinks
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
		if notice() != "" {
			setNotice("")
		}
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
				props.OnSelect(filtered[cur])
			}
			return true

		case ke.MatchString("esc", "escape"):
			if props.OnCancel != nil {
				props.OnCancel()
			}
			return true
		}

		for _, k := range slices.Sorted(maps.Keys(props.ConfirmActions)) {
			if ke.MatchString(k) && len(filtered) > 0 {
				ca := props.ConfirmActions[k]
				if ca.DisabledReason != nil {
					if reason := strings.TrimSpace(ca.DisabledReason(filtered[cur])); reason != "" {
						setNotice(reason)
						return true
					}
				}
				if ca.Prompt != nil && strings.TrimSpace(ca.Prompt(filtered[cur])) == "" {
					return true
				}
				setModal(pickerModalState{
					mode:          pickerModeConfirm,
					targetOpt:     filtered[cur],
					activeConfirm: ca,
				})
				return true
			}
		}

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

		for _, k := range slices.Sorted(maps.Keys(props.Keys)) {
			if ke.MatchString(k) && len(filtered) > 0 {
				props.Keys[k](filtered[cur])
				return true
			}
		}

		return false
	}

	trail.UseInput(func(input string, k event.KeyEvent) {
		mst := modal()
		if mst.mode == pickerModeConfirm {
			if input == "y" || input == "Y" || k.MatchString("y", "enter") {
				if mst.activeConfirm.OnConfirm != nil {
					mst.activeConfirm.OnConfirm(mst.targetOpt)
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

		if mst.mode == pickerModeBrowse && !props.Filterable {
			handleBrowseKey(k)
		}
	})

	var rows []trail.Node

	// Title
	if props.Title != "" {
		titleStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Primary)
		rows = append(rows, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{
			Style: titleStyle,
		}, trail.Text(props.Title))))
	}

	// Filter input row
	if props.Filterable && st.mode == pickerModeBrowse {
		filterPromptStyle := style.S().Bold(true).Margin(0, 1, 0, 0).Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Primary)
		rows = append(rows, trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Width(style.Percent(100)),
		},
			trail.Span(trail.SpanProps{
				Style: filterPromptStyle,
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

	maxVis := props.MaxVisible
	if maxVis <= 0 {
		maxVis = components.DefaultPickerMaxVisible
	}

	optionStyles := &components.OptionListStyles{
		Option:      style.S().Foreground(scheme.Color.Text.Secondary),
		Selected:    style.S().Bold(true).Foreground(scheme.Color.Icon.Primary),
		Description: style.S().Foreground(scheme.Color.Text.Muted),
	}

	if props.Header != nil {
		rows = append(rows, props.Header)
	}

	rows = append(rows, components.OptionList(components.OptionListProps{
		Options:    filtered,
		Selected:   cur,
		EmptyText:  "  No matching options.",
		MaxVisible: maxVis,
		Columns:    props.Columns,
		Styles:     optionStyles,
	}))

	// Inline prompt mode (confirm or input)
	switch st.mode {
	case pickerModeConfirm:
		promptText := ""
		if st.activeConfirm.Prompt != nil {
			promptText = st.activeConfirm.Prompt(st.targetOpt)
		}
		warnStyle := style.S().Bold(true).Foreground(scheme.Color.Icon.Primary)
		hintStyle := style.S().Foreground(scheme.Color.Text.Secondary)
		rows = append(rows, trail.Newline(1), trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		},
			trail.Span(trail.SpanProps{
				Style: warnStyle,
			}, trail.Text("? "+promptText+" ")),
			trail.Span(trail.SpanProps{
				Style: hintStyle,
			}, trail.Text("(y/N)")),
		))

	case pickerModeInput:
		infoStyle := style.S().Bold(true).Margin(0, 1, 0, 0).Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Icon.Primary)
		rows = append(rows, trail.Newline(1), trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Width(style.Percent(100)),
		},
			trail.Span(trail.SpanProps{
				Style: infoStyle,
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
								current.activeInput.OnSubmit(current.targetOpt, current.inputVal)
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

	var activeBindings []components.KeyBinding
	switch st.mode {
	case pickerModeConfirm:
		activeBindings = []components.KeyBinding{
			{Key: "y/enter", Label: "Confirm"},
			{Key: "n/esc", Label: "Cancel"},
		}
	case pickerModeInput:
		activeBindings = []components.KeyBinding{
			{Key: "enter", Label: "Submit"},
			{Key: "esc", Label: "Cancel"},
		}
	default:
		activeBindings = props.Bindings
		if len(activeBindings) == 0 {
			activeBindings = DefaultPickerBindings
		}
	}

	if n := notice(); n != "" {
		noticeStyle := style.S().Margin(1, 0, 0, 0).Foreground(scheme.Color.Text.Secondary)
		rows = append(rows, trail.Span(trail.SpanProps{Style: noticeStyle}, trail.Text("  "+n)))
	}

	rows = append(rows, components.KeyboardHint(components.KeyboardHintProps{
		Bindings: activeBindings,
	}))

	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default)

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	}, rows...)
})
