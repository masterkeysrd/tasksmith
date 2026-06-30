package components

import (
	"image/color"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// BarSegment defines a single segment within a Bar.
type BarSegment struct {
	// Percentage is the portion of the bar (0-100).
	Percentage float64
	// Color is the fill color. If nil, a theme palette color is used.
	Color color.Color
	// Label is an optional label for the segment.
	Label string
}

// BarProps defines properties for the Bar component.
type BarProps struct {
	// Segments are the proportional segments to render.
	Segments []BarSegment
	// Height is the bar height in cells (default 1).
	Height int
	// Style allows passing additional style overrides.
	Style style.Style
}

var (
	// BarBaseStyle is the base style for the bar container.
	BarBaseStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Width(style.Percent(100)).
			Height(style.Cells(1))

	// BarSegmentGapStyle is the style for the gap between segments.
	BarSegmentGapStyle = style.S().
				Width(style.Cells(0)).
				Height(style.Cells(1))
)

// Bar renders a segmented progress bar with proportional segments.
// Each segment's width is determined by its percentage relative to the total.
var Bar = kitex.FCC("Bar", func(props BarProps) kitex.Node {
	t := theme.UseTheme()

	if t == nil || len(props.Segments) == 0 {
		return kitex.Box(kitex.BoxProps{
			Style: BarBaseStyle.Merge(props.Style).Height(style.Cells(props.Height)),
		})
	}

	// Calculate total percentage for proportional sizing
	var total float64
	for _, s := range props.Segments {
		total += s.Percentage
	}

	// Theme palette fallback colors
	palette := []color.Color{
		t.Color.Surface.Primary,
		t.Color.Surface.Success,
		t.Color.Surface.Info,
		t.Color.Surface.Tertiary,
		t.Color.Surface.Error,
		t.Color.Surface.Secondary,
	}

	var nodes []kitex.Node
	for i, seg := range props.Segments {
		if seg.Percentage <= 0 || total <= 0 {
			continue
		}

		// Calculate flex basis proportionally
		fraction := seg.Percentage / total
		flexValue := fraction * float64(props.Height*10)
		if flexValue < 1 {
			flexValue = 1
		}

		// Determine color: segment-specific or from palette
		var segColor color.Color
		if seg.Color != nil {
			segColor = seg.Color
		} else {
			segColor = palette[i%len(palette)]
		}

		segStyle := style.S().
			Display(style.DisplayFlex).
			Flex(int(flexValue), int(flexValue), style.Cells(0)).
			Background(segColor)

		// Add gap between segments (except the last one)
		if i < len(props.Segments)-1 {
			segStyle = segStyle.MarginRight(1)
		}

		nodes = append(nodes,
			kitex.Box(kitex.BoxProps{
				Style: segStyle,
			}),
		)
	}

	return kitex.Box(kitex.BoxProps{
		Style: BarBaseStyle.Merge(props.Style).Height(style.Cells(props.Height)),
	}, nodes...)
})
