package components

import (
	"image/color"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// ModalProps defines properties for the Modal component.
type ModalProps struct {
	// IsOpen controls the visibility of the modal dialog.
	IsOpen bool
	// Title is the kitex.Node containing the header title layout.
	Title kitex.Node
	// OnClose is triggered when the dialog is dismissed.
	OnClose func()
	// Style extends the base modal container paper card style.
	Style style.Style
	// BodyStyle extends or overrides the base modal body style.
	BodyStyle style.Style
	// HeaderActions contains custom elements injected next to the close button.
	HeaderActions kitex.Node
	// Footer contains custom elements to display in the modal footer statusrail.
	Footer kitex.Node
	// Attributes contains custom DOM attributes.
	Attributes map[string]string
	// Ref is the scrollable body wrapper element reference.
	Ref kitex.Ref[dom.Element]
	// OnKeyDown is triggered when a key is pressed.
	OnKeyDown func(event.Event)
	// Children is the list of child elements displayed in the body.
	Children []kitex.Node
}

func GetScrollTarget(body dom.Element, doc dom.Document) dom.Element {
	if body == nil {
		return nil
	}
	target := body
	if doc != nil {
		if focused := doc.CurrentFocus(); focused != nil {
			curr := focused
			isDescendant := false
			for curr != nil {
				if curr == body {
					isDescendant = true
					break
				}
				curr = curr.ParentElement()
			}
			if isDescendant {
				target = focused
			}
		}
	}
	return target
}

func ScrollElement(start dom.Element, boundary dom.Element, dx, dy int) bool {
	curr := start
	scrolled := false
	for curr != nil {
		xBefore, yBefore := curr.Scroll()
		curr.ScrollBy(dx, dy)
		xAfter, yAfter := curr.Scroll()

		if xBefore != xAfter || yBefore != yAfter {
			scrolled = true
			break
		}

		if curr == boundary {
			break
		}
		curr = curr.ParentElement()
	}
	return scrolled
}

func FindAndScrollHorizontally(node dom.Node, dx int) bool {
	if node == nil {
		return false
	}
	if el, ok := node.(dom.Element); ok {
		xBefore, _ := el.Scroll()
		el.ScrollBy(dx, 0)
		xAfter, _ := el.Scroll()
		if xBefore != xAfter {
			return true
		}
	}
	for child := range node.ChildNodes() {
		if FindAndScrollHorizontally(child, dx) {
			return true
		}
	}
	return false
}

// Modal renders a standardized full-screen modal card overlay.
// It relies on Dialog's FocusScope for autofocus connection and registers
// keyboard hotkeys to close on Escape or 'q'.
var Modal = kitex.FCC("Modal", func(props ModalProps) kitex.Node {
	if !props.IsOpen {
		return nil
	}

	t := theme.UseTheme()

	baseStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(85)).
		Height(style.Percent(80)).
		Padding(0).
		Overflow(style.OverflowHidden)

	baseStyle = baseStyle.Merge(props.Style)

	var titleNode kitex.Node = props.Title
	var borderCol color.Color
	var labelTextColor color.Color
	var commentColor color.Color
	var successColor color.Color
	var statusBg color.Color

	if t != nil {
		borderCol = t.Color.Border.Primary
		labelTextColor = t.Color.Text.Secondary
		commentColor = t.Color.Text.Tertiary
		successColor = t.Color.Surface.Success
		statusBg = t.Color.Surface.BaseFocus
	}

	if props.Title != nil && t != nil {
		titleNode = kitex.Span(kitex.SpanProps{
			Style: style.S().Foreground(labelTextColor).Bold(true),
		}, props.Title)
	}

	headerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		JustifyContent(style.JustifyBetween).
		AlignItems(style.AlignCenter).
		Height(style.Cells(1)).
		PaddingHorizontal(1)

	bodyStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Flex(1, 1, style.Cells(0)).
		MinHeight(style.Cells(0)).
		OverflowY(style.OverflowAuto).
		Padding(1).
		Merge(props.BodyStyle)

	statusStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Height(style.Cells(1)).
		PaddingHorizontal(1)

	if t != nil {
		statusStyle = statusStyle.Background(statusBg)
	}

	bodyBoxProps := kitex.BoxProps{
		Style: bodyStyle,
	}
	if props.Ref != nil {
		bodyBoxProps.Ref = props.Ref
	}

	return kitex.Dialog(kitex.DialogProps{
		ZIndex: 100,
		OnKeyDown: func(e event.Event) {
			if props.OnKeyDown != nil {
				props.OnKeyDown(e)
				if e.DefaultPrevented() {
					return
				}
			}
			ke, ok := e.(*event.KeyEvent)
			if !ok {
				return
			}
			if ke.Code == key.KeyEscape || ke.Text == "q" {
				e.PreventDefault()
				e.StopPropagation()
				if props.OnClose != nil {
					props.OnClose()
				}
				return
			}
		},
		OnWheel: func(e event.Event) {
			e.StopPropagation()
		},
		OnScroll: func(e event.Event) {
			e.StopPropagation()
		},
	},
		Paper(PaperProps{
			Color:   PaperHover,
			Variant: PaperOutlined,
			Style:   baseStyle,
		},
			// Header Row
			kitex.Box(kitex.BoxProps{
				Style: headerStyle,
			},
				titleNode,
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						Gap(1),
				},
					props.HeaderActions,
					Button(ButtonProps{
						Variant: ButtonText,
						Color:   ButtonPrimary,
						OnClick: props.OnClose,
					}, kitex.Text("[X] CLOSE")),
				),
			),
			// Body Content
			kitex.Box(bodyBoxProps,
				props.Children...,
			),
			// Statusrail Divider Row
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Height(style.Cells(0)).
					BorderBottom(true, style.SingleBorder(), borderCol),
			}),
			// Statusrail (Footer)
			kitex.Box(kitex.BoxProps{
				Style: statusStyle,
			},
				kitex.If(props.Footer != nil, func() kitex.Node {
					return props.Footer
				}),
				kitex.If(props.Footer == nil, func() kitex.Node {
					return kitex.Fragment(
						kitex.Span(kitex.SpanProps{
							Style: style.S().Foreground(commentColor).Bold(true),
						}, kitex.Text("INTERACTIVE")),
						kitex.Box(kitex.BoxProps{
							Style: style.S().Flex(1, 1, style.Cells(0)),
						}),
						kitex.Span(kitex.SpanProps{
							Style: style.S().Foreground(successColor).Bold(true),
						}, kitex.Text("ESC TO CLOSE")),
					)
				}),
			),
		),
	)
})
