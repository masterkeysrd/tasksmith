package components

import (
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// KeyBinding represents a single keyboard shortcut and its action description.
type KeyBinding struct {
	Key   string
	Label string
}

// KeyboardHintProps defines the properties for the KeyboardHint component.
type KeyboardHintProps struct {
	Bindings []KeyBinding
}

// KeyboardHint renders a "Keyboard: <key> <label>  ..." bar with keys
// highlighted in the primary icon color, wrapping whole key/label pairs onto
// continuation lines aligned after the "Keyboard: " gutter on narrow screens.
var KeyboardHint = trail.FC("KeyboardHint", func(props KeyboardHintProps) trail.Node {
	if len(props.Bindings) == 0 {
		return nil
	}

	scheme := theme.UseTheme()

	prefixStyle := style.S().Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Muted)
	keyStyle := style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Icon.Primary)
	labelStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary)
	pairStyle := style.S().Display(style.DisplayFlex).Flex(0, 0).FlexDirection(style.FlexRow)

	items := make([]trail.Node, 0, len(props.Bindings))
	for _, b := range props.Bindings {
		pair := []trail.Node{
			trail.Span(trail.SpanProps{Style: keyStyle}, trail.Text(b.Key)),
		}
		if b.Label != "" {
			pair = append(pair, trail.Span(trail.SpanProps{Style: labelStyle}, trail.Text(" "+b.Label)))
		}
		items = append(items, trail.Box(trail.BoxProps{Style: pairStyle}, pair...))
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Width(style.Percent(100)).
			MarginTop(1),
	},
		trail.Span(trail.SpanProps{Style: prefixStyle}, trail.Text("Keyboard: ")),
		trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				Flex(1).
				MinWidth(style.Cells(0)).
				FlexDirection(style.FlexRow).
				FlexWrap(style.FlexWrapOn).
				Gap(0, 2),
		}, items...),
	)
})
