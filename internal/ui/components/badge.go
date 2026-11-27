package components

import (
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
)

type Tone int

const (
	ToneDefault Tone = iota
	ToneMuted
	ToneAccent
	ToneSuccess
	ToneWarning
	ToneError
)

func (t Tone) textStyle(s styles.Common) style.Style {
	switch t {
	case ToneMuted:
		return s.Muted
	case ToneAccent:
		return s.Accent
	case ToneSuccess:
		return s.Success
	case ToneWarning:
		return s.Warning
	case ToneError:
		return s.Error
	default:
		return s.Primary
	}
}

type BadgeProps struct {
	Icon  string
	Label string
	Tone  Tone
}

var Badge = trail.FC("Badge", func(props BadgeProps) trail.Node {
	s := styles.UseCommon()
	return trail.Span(trail.SpanProps{Style: props.Tone.textStyle(s)},
		trail.Text(strings.TrimSpace(props.Icon+" "+props.Label)),
	)
})
