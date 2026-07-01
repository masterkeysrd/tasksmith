package chat

import (
	"strings"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// QueuedBubbleProps defines the properties for a single queued message bubble.
type QueuedBubbleProps struct {
	Key string
	// ID is the message ID. If it starts with "opt_" the bubble is treated as
	// optimistic (no Edit/Remove actions shown).
	ID string
	// Text is the message content to display.
	Text string
	// OnEdit is called when the user clicks [Edit].
	OnEdit func(id string)
	// OnRemove is called when the user clicks [Remove].
	OnRemove func(id string)
}

// QueuedBubble renders a single queued user message bubble with optional
// Edit and Remove actions.
var QueuedBubble = kitex.FC("QueuedBubble", func(props QueuedBubbleProps) kitex.Node {
	t := theme.UseTheme()

	isOptimistic := strings.HasPrefix(props.ID, "opt_")

	headerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Gap(0).
		Foreground(t.Color.Text.Tertiary).
		Bold(true)

	senderStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Gap(0).
		Foreground(t.Color.Surface.Primary)

	headerChildren := []kitex.Node{
		kitex.Box(kitex.BoxProps{Style: senderStyle},
			icon.User,
			kitex.Text(" USER"),
		),
		kitex.Text(" ─ "),
		kitex.Text("QUEUED"),
	}

	if !isOptimistic {
		editActionStyle := style.S().
			Foreground(t.Color.Surface.Info).
			Underline(true).
			MarginLeft(1)

		removeActionStyle := style.S().
			Foreground(t.Color.Text.Error).
			Underline(true).
			MarginLeft(1)

		id := props.ID
		headerChildren = append(headerChildren,
			kitex.Span(kitex.SpanProps{
				Style: editActionStyle,
				OnClick: func(e event.Event) {
					if props.OnEdit != nil {
						props.OnEdit(id)
					}
				},
			}, kitex.Text("[Edit]")),
			kitex.Span(kitex.SpanProps{
				Style: removeActionStyle,
				OnClick: func(e event.Event) {
					if props.OnRemove != nil {
						props.OnRemove(id)
					}
				},
			}, kitex.Text("[Remove]")),
		)
	}

	bubbleStyle := style.S().
		Padding(1).
		MaxWidth(style.Percent(90)).
		MinWidth(style.Percent(0)).
		Overflow(style.OverflowHidden).
		Foreground(t.Color.Text.Tertiary)

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Width(style.Percent(100)).
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			AlignItems(style.AlignEnd).
			MarginBottom(1),
	},
		kitex.Box(kitex.BoxProps{Style: headerStyle}, headerChildren...),
		kitex.Box(kitex.BoxProps{Style: bubbleStyle},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Gap(1).
					Width(style.Percent(100)).
					MaxWidth(style.Percent(100)).
					Overflow(style.OverflowHidden),
			},
				components.Markdown(components.MarkdownProps{
					Source: props.Text,
				}),
			),
		),
	)
})
