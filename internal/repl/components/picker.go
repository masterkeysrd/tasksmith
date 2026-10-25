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
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// DefaultPickerBindings defines the default keyboard shortcuts displayed on the picker.
var DefaultPickerBindings = []KeyBinding{
	{Key: "↑/↓", Label: "Navigate"},
	{Key: "enter", Label: "Confirm"},
	{Key: "esc", Label: "Cancel"},
}

// ConfirmAction defines an inline confirmation prompt triggered by a custom key.
type ConfirmAction struct {
	Prompt    func(opt uicomponents.Option) string
	OnConfirm func(opt uicomponents.Option)
}

// InputAction defines an inline text input prompt triggered by a custom key.
type InputAction struct {
	Prompt       string
	InitialValue func(opt uicomponents.Option) string
	OnSubmit     func(opt uicomponents.Option, value string)
}

type pickerMode int

const (
	pickerModeBrowse pickerMode = iota
	pickerModeConfirm
	pickerModeInput
)

type pickerModalState struct {
	mode          pickerMode
	targetOpt     uicomponents.Option
	activeConfirm ConfirmAction
	activeInput   InputAction
	inputVal      string
}

// PickerProps defines the properties for the interactive Picker component.
type PickerProps struct {
	Title          string
	Options        []uicomponents.Option
	MaxVisible     int                   // Max visible options before scrolling (defaults to uicomponents.DefaultPickerMaxVisible)
	Columns        []uicomponents.Column // Column specifications forwarded to OptionList
	Bindings       []KeyBinding          // Optional navigation shortcuts rendered inline via KeyboardHint
	Filterable     bool
	OnOpen         func()
	OnSelect       func(opt uicomponents.Option)
	Keys           map[string]func(opt uicomponents.Option)
	ConfirmActions map[string]ConfirmAction
	InputActions   map[string]InputAction
	OnCancel       func()
}

// NewPicker constructs an initialized PickerProps with default bindings and cursor at current.
func NewPicker(title string, opts []uicomponents.Option, onSelect func(opt uicomponents.Option)) PickerProps {
	return PickerProps{
		Title:    title,
		Options:  opts,
		Bindings: DefaultPickerBindings,
		OnSelect: onSelect,
	}
}

func initialCursor(opts []uicomponents.Option) int {
	for i, opt := range opts {
		if opt.Current {
			return i
		}
	}
	return 0
}

func filterOptions(opts []uicomponents.Option, query string) []uicomponents.Option {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return opts
	}
	var res []uicomponents.Option
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

	filtered := kitex.UseMemo(func() []uicomponents.Option {
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
				setModal(pickerModalState{
					mode:          pickerModeConfirm,
					targetOpt:     filtered[cur],
					activeConfirm: props.ConfirmActions[k],
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
		titleStyle := style.S().Bold(true)
		if scheme != nil && scheme.Color.Text.Primary != nil {
			titleStyle = titleStyle.Foreground(scheme.Color.Text.Primary)
		}
		rows = append(rows, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{
			Style: titleStyle,
		}, trail.Text(props.Title))))
	}

	// Filter input row
	if props.Filterable && st.mode == pickerModeBrowse {
		filterPromptStyle := style.S().Bold(true).Margin(0, 1, 0, 0).Flex(0, 0).WhiteSpace(style.WhiteSpacePre)
		if scheme != nil && scheme.Color.Text.Primary != nil {
			filterPromptStyle = filterPromptStyle.Foreground(scheme.Color.Text.Primary)
		}
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
		maxVis = uicomponents.DefaultPickerMaxVisible
	}

	optionStyles := &uicomponents.OptionListStyles{
		Option:      style.S(),
		Selected:    style.S().Bold(true),
		Description: style.S(),
	}
	if scheme != nil {
		if scheme.Color.Text.Secondary != nil {
			optionStyles.Option = optionStyles.Option.Foreground(scheme.Color.Text.Secondary)
		}
		if scheme.Color.Icon.Primary != nil {
			optionStyles.Selected = optionStyles.Selected.Foreground(scheme.Color.Icon.Primary)
		}
		if scheme.Color.Text.Secondary != nil {
			optionStyles.Description = optionStyles.Description.Foreground(scheme.Color.Text.Secondary)
		}
	}

	rows = append(rows, uicomponents.OptionList(uicomponents.OptionListProps{
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
		warnStyle := style.S().Bold(true)
		if scheme != nil && scheme.Color.Text.Purple != nil {
			warnStyle = warnStyle.Foreground(scheme.Color.Text.Purple)
		}
		hintStyle := style.S()
		if scheme != nil && scheme.Color.Text.Secondary != nil {
			hintStyle = hintStyle.Foreground(scheme.Color.Text.Secondary)
		}
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
		infoStyle := style.S().Bold(true).Margin(0, 1, 0, 0).Flex(0, 0).WhiteSpace(style.WhiteSpacePre)
		if scheme != nil && scheme.Color.Surface.Info != nil {
			infoStyle = infoStyle.Foreground(scheme.Color.Surface.Info)
		}
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

	var activeBindings []KeyBinding
	switch st.mode {
	case pickerModeConfirm:
		activeBindings = []KeyBinding{
			{Key: "y/enter", Label: "Confirm"},
			{Key: "n/esc", Label: "Cancel"},
		}
	case pickerModeInput:
		activeBindings = []KeyBinding{
			{Key: "enter", Label: "Submit"},
			{Key: "esc", Label: "Cancel"},
		}
	default:
		activeBindings = props.Bindings
		if len(activeBindings) == 0 {
			activeBindings = DefaultPickerBindings
		}
	}

	rows = append(rows, KeyboardHint(KeyboardHintProps{
		Bindings: activeBindings,
	}))

	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false)
	if scheme != nil && scheme.Color.Border.Primary != nil {
		border = border.Color(scheme.Color.Border.Primary)
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	}, rows...)
})
