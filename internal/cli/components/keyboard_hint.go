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

// KeyboardHint renders an inline "Keyboard: <key> <label>  ..." bar with keys highlighted in cyan.
var KeyboardHint = trail.FC("KeyboardHint", func(props KeyboardHintProps) trail.Node {
	if len(props.Bindings) == 0 {
		return nil
	}

	scheme := theme.UseTheme()

	prefixStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Muted)
	keyStyle := style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Icon.Primary)
	labelStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary)

	var spans []trail.Node
	spans = append(spans, trail.Span(trail.SpanProps{Style: prefixStyle}, trail.Text("Keyboard: ")))

	for i, b := range props.Bindings {
		if i > 0 {
			spans = append(spans, trail.Span(trail.SpanProps{Style: labelStyle}, trail.Text("  ")))
		}
		spans = append(spans, trail.Span(trail.SpanProps{Style: keyStyle}, trail.Text(b.Key)))
		if b.Label != "" {
			spans = append(spans, trail.Span(trail.SpanProps{Style: labelStyle}, trail.Text(" "+b.Label)))
		}
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow).MarginTop(1),
	}, spans...)
})
