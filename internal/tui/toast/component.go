package toast

import (
	"fmt"
	"image/color"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// ToastContainer renders the overlay dialog containing active toasts stacked vertically.
var ToastContainer = kitex.SimpleFC("ToastContainer", func() kitex.Node {
	toasts := UseToasts()

	if len(toasts) == 0 {
		return nil
	}

	// Stacking layout: overlay container on top of all views (ZIndex 999),
	// filling full-screen but transparent, aligning items to the bottom-right.
	containerStyle := style.S().
		Width(style.Percent(100)).
		Height(style.Percent(100)).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		JustifyContent(style.JustifyEnd).
		AlignItems(style.AlignEnd).
		PaddingBottom(1).
		PaddingRight(2).
		Gap(1).
		Background(color.Transparent)

	// Display only the last 3 toasts to avoid clutter
	displayToasts := toasts
	if len(toasts) > 3 {
		displayToasts = toasts[len(toasts)-3:]
	}

	return kitex.Dialog(kitex.DialogProps{
		ZIndex: 999,
	},
		kitex.Box(kitex.BoxProps{Style: containerStyle},
			kitex.Map(displayToasts, func(t Toast, _ int) kitex.Node {
				return ToastItem(ToastItemProps{Toast: t})
			}),
		),
	)
})

type ToastItemProps struct {
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
