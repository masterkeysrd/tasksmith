package chat

import (
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/resolver"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	"github.com/masterkeysrd/tasksmith/internal/tui/plugin/autocomplete"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/widgets"
)

// ComposerProps defines the properties for the Composer component.
type ComposerProps struct {
	Value     string
	Disabled  bool
	IsInsert  bool
	Ref       kitex.Ref[dom.Element]
	OnChange  func(string)
	OnKeyDown func(event.Event)
	OnSubmit  func(text string, refs []resolver.Reference)
	SessionID string
}

// resourceTypeFromKind maps an autocomplete Item.Kind to the corresponding
// resolver.ResourceType. It provides a shared mapping between the autocomplete
// system and the reference tracker.
func resourceTypeFromKind(kind string) resolver.ResourceType {
	switch strings.ToLower(kind) {
	case "file", "directory":
		return resolver.TypeFile
	case "function", "struct", "method", "variable", "class", "interface", "constant", "field", "property", "enum", "module", "namespace", "package", "constructor", "lsp":
		return resolver.TypeSymbol
	case "skill":
		return resolver.TypeSkill
	default:
		return resolver.TypeFile
	}
}

// Composer is a multiline composer component styled like a terminal UI box,
// matching the design mockup in mockup.tsx.
var Composer = kitex.FC("Composer", func(props ComposerProps) kitex.Node {
	isFocused, setIsFocused := kitex.UseState(false)
	wrapperRef := kitex.UseRef[dom.Element](nil)
	t := theme.UseTheme()

	// Tracked references accumulated from autocomplete selections.
	trackedRefs, setTrackedRefs := kitex.UseState([]resolver.Reference{})

	// 1. Instantiate the Autocomplete Controller once using UseMemo
	acController := kitex.UseMemo(func() *autocomplete.Controller {
		return autocomplete.New(autocomplete.Config{
			Triggers: map[string][]string{
				"@": {"file", "symbol", "skill"},
				"/": {"command"},
			},
			Prefixes: resolver.PrefixToSourceMap(),
		})
	}, nil)

	// Call Use() to bind the component's render cycle reactively to state changes
	_ = acController.Use()

	if t == nil {
		return kitex.Box(kitex.BoxProps{}, kitex.Text("No Theme"))
	}

	// Resolve the focus blue color from the palette/theme
	blueColor := t.Color.Surface.Info

	// Border color switches to blue when focused, otherwise comment color
	borderColor := t.Color.Text.Tertiary
	if isFocused() {
		borderColor = blueColor
	}

	// Wrapper style with a full single border
	wrapperStyle := style.S().
		Width(style.Percent(100)).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignEnd).
		Padding(0, 1).
		Border(style.SingleBorder().Color(borderColor))

	// Text area style
	var textCol color.Color
	if props.Disabled {
		textCol = t.Color.Text.Tertiary
	} else {
		textCol = t.Color.Text.Secondary // fg_dark: #a9b1d6
	}

	textareaStyle := style.S().
		Flex(1, 1, style.Cells(0)).
		MinHeight(style.Cells(1)).
		MaxHeight(style.Cells(8)).
		Background(color.Transparent).
		Foreground(textCol).
		Border(false)

	// Placeholder style
	ps := style.S().Foreground(t.Color.Text.Tertiary)

	textareaDisabled := props.Disabled || !props.IsInsert

	textareaProps := kitex.TextAreaProps{
		Name:             "composer-textarea",
		Value:            props.Value,
		Placeholder:      "Message TaskSmith...",
		PlaceholderStyle: ps,
		Disabled:         textareaDisabled,
		Style:            textareaStyle,
		Ref:              props.Ref,
		OnChange: func(e event.Event) {
			val := ""
			if ie, ok := e.(*event.ChangeEvent); ok {
				val = ie.Value
			} else if ie, ok := e.(*event.InputEvent); ok {
				val = ie.Value
			}
			if props.OnChange != nil {
				props.OnChange(val)
			}

			// Prune broken references: remove tracked refs whose InsertText
			// no longer appears in the updated text.
			current := trackedRefs()
			var surviving []resolver.Reference
			for _, ref := range current {
				if strings.Contains(val, ref.InsertText) {
					surviving = append(surviving, ref)
				}
			}
			if len(surviving) != len(current) {
				setTrackedRefs(surviving)
			}

			var cursorOffset int
			if props.Ref != nil && props.Ref.Current != nil {
				if sr, ok := props.Ref.Current.(interface{ SelectionRange() (int, int) }); ok {
					start, _ := sr.SelectionRange()
					cursorOffset = start
				} else {
					cursorOffset = len(val)
				}
			} else {
				cursorOffset = len(val)
			}
			acController.HandleOnChange(val, cursorOffset)
		},
		OnFocus: func(e event.Event) {
			setIsFocused(true)
		},
		OnBlur: func(e event.Event) {
			setIsFocused(false)
			acController.SetIsOpen(false)
			mode.Set(mode.Normal)
		},
		OnKeyDown: func(e event.Event) {
			ke, ok := e.(*event.KeyEvent)
			if !ok {
				return
			}

			// Intercept autocomplete keyboard controls (Enter/Tab/Escape/Arrows)
			if acController.HandleOnKeyDown(ke, props.Value, props.OnChange) {
				e.PreventDefault()
				e.StopPropagation()
				return
			}

			if ke.Code == key.KeyEscape {
				e.PreventDefault()
				e.StopPropagation()
				if props.Ref != nil && props.Ref.Current != nil {
					props.Ref.Current.Blur()
				}
				mode.Set(mode.Normal)
				return
			}

			// Enter without modifiers submits
			if (ke.Code == key.KeyEnter || ke.Text == "\r" || ke.Text == "\n") && (ke.Mod&key.ModShift) == 0 {
				e.PreventDefault()
				e.StopPropagation()
				if props.OnSubmit != nil {
					props.OnSubmit(props.Value, trackedRefs())
				}
				return
			}

			if props.OnKeyDown != nil {
				props.OnKeyDown(e)
			}
		},
	}

	// Send button style
	btnStyle := style.S().
		Padding(0, 1).
		Background(color.Transparent).
		Height(style.Cells(1))

	isSendDisabled := props.Disabled || strings.TrimSpace(props.Value) == ""

	if isSendDisabled {
		btnStyle = btnStyle.Foreground(t.Color.Text.Tertiary)
	} else {
		btnStyle = btnStyle.Foreground(t.Color.Text.Tertiary)
	}

	btnHoverStyle := style.S()
	if !isSendDisabled {
		btnHoverStyle = btnHoverStyle.
			Background(t.Color.Surface.BaseFocus).
			Foreground(blueColor)
	}

	var cursorOffset int
	if props.Ref != nil && props.Ref.Current != nil {
		if sr, ok := props.Ref.Current.(interface{ SelectionRange() (int, int) }); ok {
			start, _ := sr.SelectionRange()
			cursorOffset = start
		} else {
			cursorOffset = len(props.Value)
		}
	} else {
		cursorOffset = len(props.Value)
	}

	wrapperProps := kitex.BoxProps{
		Key:   "composer-input-container",
		Style: wrapperStyle,
		Ref:   wrapperRef,
	}
	if !props.Disabled && !props.IsInsert {
		wrapperProps.OnClick = func(e event.Event) {
			mode.Set(mode.Insert)
		}
	}

	outerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100))

	anchorEl := wrapperRef.Current
	if anchorEl == nil {
		anchorEl = props.Ref.Current
	}

	return kitex.Box(kitex.BoxProps{Style: outerStyle},
		widgets.AutocompleteOverlay(widgets.AutocompleteOverlayProps{
			Anchor:      anchorEl,
			InputAnchor: props.Ref.Current,
			Controller:  acController,
			Value:       props.Value,
		}, widgets.AutocompleteMenu(widgets.AutocompleteMenuProps{
			Controller: acController,
			SessionID:  props.SessionID,
			OnSelect: func(item autocomplete.Item) {
				newText, _ := acController.ApplySelection(props.Value, cursorOffset, item)
				if props.OnChange != nil {
					props.OnChange(newText)
				}

				// Dedup by full path: don't append if same file already tracked.
				newRef := resolver.Reference{
					Type:        resourceTypeFromKind(item.Kind),
					Value:       item.ID,
					InsertText:  item.InsertValue,
					FromTracker: true,
				}
				current := trackedRefs()
				for _, ref := range current {
					if ref.Type == newRef.Type && ref.Value == newRef.Value {
						return // already tracked
					}
				}
				setTrackedRefs(append(current, newRef))

				acController.SetIsOpen(false)
			},
		})),
		// Input bordered text box and submit button
		kitex.Box(wrapperProps,
			kitex.TextArea(textareaProps),
			components.Button(components.ButtonProps{
				Variant:  components.ButtonText,
				Disabled: isSendDisabled,
				OnClick: func() {
					if props.OnSubmit != nil {
						props.OnSubmit(props.Value, trackedRefs())
					}
				},
				Style:      btnStyle,
				HoverStyle: btnHoverStyle,
			}, icon.MoveUp),
		),
	)
})
