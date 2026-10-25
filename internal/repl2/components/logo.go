package components

import (
	"image/color"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
)

var (
	logoColorTop    = color.RGBA{R: 42, G: 195, B: 222, A: 255} // #2ac3de (Top Bar)
	logoColorCenter = color.RGBA{R: 26, G: 188, B: 156, A: 255} // #1abc9c (Center Pillar)
	logoColorBase   = color.RGBA{R: 255, G: 95, B: 95, A: 255}  // #ff5f5f (Flared Base)
)

const (
	logoLine1 = " ◥██████████◣"
	logoLine2 = "     ▐██▌    "
	logoLine3 = "    ◢████◣   "
)

// Logo renders the 3-line ASCII brand logo with distinct colors for the top bar,
// center pillar, and flared base.
var Logo = trail.SimpleFC("Logo", func() trail.Node {
	line1Style := style.S().
		WhiteSpace(style.WhiteSpacePre).
		Foreground(logoColorTop)

	line2Style := style.S().
		WhiteSpace(style.WhiteSpacePre).
		Foreground(logoColorCenter)

	line3Style := style.S().
		WhiteSpace(style.WhiteSpacePre).
		Foreground(logoColorBase)

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			MarginRight(3),
	},
		trail.Span(trail.SpanProps{
			Style: line1Style,
		}, trail.Text(logoLine1)),
		trail.Span(trail.SpanProps{
			Style: line2Style,
		}, trail.Text(logoLine2)),
		trail.Span(trail.SpanProps{
			Style: line3Style,
		}, trail.Text(logoLine3)),
	)
})
