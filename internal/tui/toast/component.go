package toast

import (
	"fmt"
	"image/color"
	"time"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/geom"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type ToastOverlayProps struct {
	Style    style.Style
	Children []kitex.Node
}

// ToastOverlay renders a generic Box that registers itself as a document overlay
// on mount, allowing fallback margin positioning without expanding to full-screen size.
var ToastOverlay = kitex.FCC("ToastOverlay", func(props ToastOverlayProps) kitex.Node {
	elRef := kitex.UseRef[dom.Node](nil)
	docFunc := kitex.UseDocument()
	doc := docFunc()

	kitex.UseEffectCleanup(func() func() {
		node := elRef.Current
		if node != nil && doc != nil {
			elVal := node.(dom.Element)
			doc.ShowOverlay(elVal, 999)
			return func() {
				doc.HideOverlay(elVal)
			}
		}
		return nil
	}, []any{elRef.Current})

	return kitex.Box(kitex.BoxProps{
		Ref:   elRef,
		Style: props.Style,
	}, props.Children...)
})

func useViewportSize() geom.Size {
	docFunc := kitex.UseDocument()
	doc := docFunc()

	var initialSize geom.Size
	if doc != nil {
		if view := doc.DefaultView(); view != nil {
			initialSize = view.ViewportSize()
		}
	}

	size, setSize := kitex.UseState(initialSize)

	kitex.UseEffectCleanup(func() func() {
		if doc == nil {
			return nil
		}

		// Initial sync when doc is resolved
		if view := doc.DefaultView(); view != nil {
			currSize := view.ViewportSize()
			if currSize != size() {
				setSize(currSize)
			}
		}

		// Listen to resize events on the document
		sub := doc.AddEventListener(event.EventResize, func(ev event.Event) {
			if view := doc.DefaultView(); view != nil {
				setSize(view.ViewportSize())
			}
		})

		return func() {
			sub.Cancel()
		}
	}, []any{doc})

	return size()
}

func calculateToastsHeight(toasts []Toast) int {
	if len(toasts) == 0 {
		return 0
	}
	displayToasts := toasts
	if len(toasts) > 3 {
		displayToasts = toasts[len(toasts)-3:]
	}

	totalHeight := 0
	for _, t := range displayToasts {
		msgLen := len(t.Message)
		msgLines := (msgLen + 40) / 41
		if msgLines == 0 {
			msgLines = 1
		}
		// A mathematically precise height estimate of a single card:
		// msgLines + TitleRow(1) + ProgressFooter(1) + Padding(2) + Border(2) + Gaps(2) = msgLines + 8
		cardHeight := msgLines + 8
		totalHeight += cardHeight
	}
	totalHeight += len(displayToasts) - 1 // gaps between toast cards
	return totalHeight
}

// ToastContainer renders the overlay dialog containing active toasts stacked vertically.
var ToastContainer = kitex.SimpleFC("ToastContainer", func() kitex.Node {
	toasts := UseToasts()

	if len(toasts) == 0 {
		return nil
	}

	// If we exceed the visual limit of 3, asynchronously dismiss the oldest active toast
	// so that it is cleanly removed from the store and does not reappear later.
	if len(toasts) > 3 {
		kitex.PostMacro(func() {
			Dismiss(toasts[0].ID)
		})
	}

	// Display only the last 3 toasts to avoid clutter
	displayToasts := toasts
	if len(toasts) > 3 {
		displayToasts = toasts[len(toasts)-3:]
	}

	size := useViewportSize()
	contentHeight := calculateToastsHeight(toasts)
	log.Info("ToastContainer rendering toasts", log.Int("active_toasts", len(toasts)), log.Int("displayed_toasts", len(displayToasts)), log.Int("calculated_height", contentHeight))

	marginLeft := size.Width - 45 - 2
	marginTop := size.Height - contentHeight - 1

	if marginLeft < 0 {
		marginLeft = 0
	}
	if marginTop < 0 {
		marginTop = 0
	}

	containerStyle := style.S().
		Width(style.Cells(45)).
		MarginLeft(marginLeft).
		MarginTop(marginTop).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Gap(1).
		Background(color.Transparent)

	return ToastOverlay(ToastOverlayProps{
		Style: containerStyle,
	},
		kitex.Map(displayToasts, func(t Toast, _ int) kitex.Node {
			return ToastItem(ToastItemProps{Key: t.ID, Toast: t})
		}),
	)
})

type ToastItemProps struct {
	Key   string
	Toast Toast
}

// ToastItem represents a single toast card in the stack.
var ToastItem = kitex.FCC("ToastItem", func(props ToastItemProps) kitex.Node {
	t := theme.UseTheme()

	totalDuration := props.Toast.Duration
	if totalDuration <= 0 {
		totalDuration = 5 * time.Second
	}

	remainingDuration, setRemainingDuration := kitex.UseState(totalDuration)
	startRef := kitex.UseRef(time.Now())

	// UseInterval triggers a callback at 150ms intervals managed by the TUI framework
	kitex.UseInterval(func() {
		elapsed := time.Since(startRef.Current)
		rem := totalDuration - elapsed
		if rem <= 0 {
			Dismiss(props.Toast.ID)
			return
		}
		setRemainingDuration(rem)
	}, 150*time.Millisecond, []any{})

	var borderCol color.Color
	var textCol color.Color
	var iconNode kitex.Node

	if t != nil {
		switch props.Toast.Severity {
		case Success:
			borderCol = t.Color.Surface.Success
			textCol = t.Color.Surface.Success
			iconNode = icon.Check
		case Warning:
			borderCol = t.Color.Surface.Tertiary
			textCol = t.Color.Surface.Tertiary
			iconNode = icon.Warning
		case Error:
			borderCol = t.Color.Surface.Error
			textCol = t.Color.Surface.Error
			iconNode = icon.Error
		default:
			borderCol = t.Color.Surface.Info
			textCol = t.Color.Surface.Info
			iconNode = icon.Info
		}
	}

	// Frame styling
	cardStyle := style.S().
		Width(style.Cells(45)).
		Padding(1).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Gap(1).
		Border(style.SingleBorder().Color(borderCol))

	headerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		JustifyContent(style.JustifyBetween).
		Width(style.Percent(100))

	titleWrapperStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Gap(1)

	msgStyle := style.S().
		Width(style.Percent(100))

	if t != nil {
		msgStyle = msgStyle.Foreground(t.Color.Text.Secondary)
	}

	footerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Width(style.Percent(100))

	return components.Paper(components.PaperProps{
		Color:   components.PaperBase,
		Variant: components.PaperOutlined,
		Style:   cardStyle,
	},
		// Header Row (Icon + Title + Dismiss button)
		kitex.Box(kitex.BoxProps{Style: headerStyle},
			kitex.Box(kitex.BoxProps{Style: titleWrapperStyle},
				kitex.Span(kitex.SpanProps{
					Style: style.S().Foreground(textCol).Bold(true),
				}, iconNode),
				kitex.Span(kitex.SpanProps{
					Style: style.S().Bold(true),
				}, kitex.Text(props.Toast.Title)),
			),
			components.Button(components.ButtonProps{
				Variant: components.ButtonText,
				Color:   components.ButtonBase,
				OnClick: func() {
					Dismiss(props.Toast.ID)
				},
			}, kitex.Text("✖")),
		),
		// Message Body
		kitex.Box(kitex.BoxProps{Style: msgStyle},
			kitex.Text(props.Toast.Message),
		),
		// Progress Timer Footer (Layout-driven progress bar)
		kitex.Box(kitex.BoxProps{Style: footerStyle},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Flex(1, 1, style.Cells(0)).
					Height(style.Cells(1)).
					Background(t.Color.Surface.BaseFocus).
					Display(style.DisplayFlex),
			},
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Width(style.Percent(float32(remainingDuration()) / float32(totalDuration) * 100)).
						Height(style.Percent(100)).
						Background(borderCol),
				}),
			),
			kitex.Span(kitex.SpanProps{
				Style: style.S().Foreground(t.Color.Text.Tertiary).MarginLeft(2),
			}, kitex.Text(fmt.Sprintf("%ds", int((remainingDuration()+999*time.Millisecond)/time.Second)))),
		),
	)
})
