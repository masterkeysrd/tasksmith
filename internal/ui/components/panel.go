package components

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type PanelProps struct {
	Title      string
	Subtitle   string
	Header     trail.Node
	Notice     string
	Error      string
	Bindings   []KeyBinding
	OuterStyle *style.Style
	Children   []trail.Node
}

type panelStyles struct {
	Outer    style.Style
	Header   style.Style
	Title    style.Style
	Subtitle style.Style
	Row      style.Style
	Notice   style.Style
	Error    style.Style
}

func newPanelStyles(s *theme.Scheme) panelStyles {
	common := styles.NewCommon(s)
	return panelStyles{
		Outer: common.Panel,
		Header: common.Row.
			Display(style.DisplayFlex).
			Gap(0, 1).
			MarginBottom(1),
		Title:    common.Title,
		Subtitle: common.Muted,
		Row:      common.Row,
		Notice: common.Secondary.
			WhiteSpace(style.WhiteSpacePreWrap),
		Error: common.Error.
			WhiteSpace(style.WhiteSpacePreWrap),
	}
}

var Panel = trail.FCC("Panel", func(props PanelProps) trail.Node {
	st := styles.Use(newPanelStyles)
	outer := st.Outer
	if props.OuterStyle != nil {
		outer = *props.OuterStyle
	}

	header := props.Header
	if header == nil {
		header = kitex.If(props.Title != "" || props.Subtitle != "", func() trail.Node {
			return trail.Box(trail.BoxProps{Style: st.Header},
				kitex.If(props.Title != "", func() trail.Node {
					return trail.Span(trail.SpanProps{Style: st.Title}, trail.Text(props.Title))
				}),
				kitex.If(props.Subtitle != "", func() trail.Node {
					return trail.Span(trail.SpanProps{Style: st.Subtitle}, trail.Text(props.Subtitle))
				}),
			)
		})
	}

	return trail.Box(trail.BoxProps{Style: outer},
		header,
		kitex.Fragment(props.Children...),
		kitex.If(props.Notice != "", func() trail.Node {
			return trail.Box(trail.BoxProps{Style: st.Row},
				trail.Span(trail.SpanProps{Style: st.Notice}, trail.Text(props.Notice)),
			)
		}),
		kitex.If(props.Error != "", func() trail.Node {
			return trail.Box(trail.BoxProps{Style: st.Row},
				trail.Span(trail.SpanProps{Style: st.Error}, trail.Text(props.Error)),
			)
		}),
		kitex.If(len(props.Bindings) > 0, func() trail.Node {
			return KeyboardHint(KeyboardHintProps{Bindings: props.Bindings})
		}),
	)
})
