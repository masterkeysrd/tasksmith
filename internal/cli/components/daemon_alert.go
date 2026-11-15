package components

import (
	"image/color"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// DaemonAlert renders a non-blocking alert banner above the composer when the background Brain Daemon
// is disconnected or restarting.
var DaemonAlert = trail.SimpleFC("DaemonAlert", func() trail.Node {
	if store.UseHasActiveSlot() {
		return nil
	}
	status := store.UseDaemonStatus()
	if status == "" || status == store.DaemonConnected {
		return nil
	}

	scheme := theme.UseTheme()

	var borderColor color.Color
	var line1Text string
	var line1Color color.Color
	var line2Text string

	switch status {
	case store.DaemonDisconnected:
		borderColor = scheme.Color.Border.Error
		line1Text = "⚠ Brain Daemon is offline"
		line1Color = scheme.Color.Text.Error
		line2Text = "Waiting to reconnect automatically, or run /daemon restart"
	case store.DaemonRestarting:
		borderColor = scheme.Color.Border.Warning
		line1Text = "↻ Restarting Brain Daemon..."
		line1Color = scheme.Color.Text.Warning
		line2Text = "Spawning background process and syncing session state"
	default:
		return nil
	}

	border := style.SingleBorder().Top(false).Bottom(false).Right(false).Left(true).Color(borderColor)
	containerStyle := style.S().
		Border(border).
		PaddingLeft(1).
		MarginBottom(1).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100))

	headerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		Width(style.Percent(100)).
		WhiteSpace(style.WhiteSpaceNoWrap)

	return trail.Box(trail.BoxProps{
		Style: containerStyle,
	},
		trail.Box(trail.BoxProps{Style: headerStyle},
			trail.Span(trail.SpanProps{
				Style: style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(line1Color),
			}, trail.Text(line1Text)),
		),
		trail.Box(trail.BoxProps{Style: headerStyle},
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Text.Secondary),
			}, trail.Text(line2Text)),
		),
	)
})
