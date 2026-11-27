package components

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type KVItem struct {
	Label     string
	Value     string
	ValueNode trail.Node
	Tone      Tone
	OmitEmpty bool
}

type KVListProps struct {
	Items         []KVItem
	Indent        int
	MinLabelWidth int
}

type kvListStyles struct {
	Common styles.Common
	List   style.Style
	Row    style.Style
	Label  style.Style
	Value  style.Style
}

func newKVListStyles(s *theme.Scheme) kvListStyles {
	common := styles.NewCommon(s)
	return kvListStyles{
		Common: common,
		List:   common.Column,
		Row: common.Row.
			Display(style.DisplayFlex).
			Gap(0, 1),
		Label: common.Secondary.
			MinWidth(style.Cells(0)).
			Flex(0, 0),
		Value: common.Column.
			Flex(1).
			MinWidth(style.Cells(0)),
	}
}

var KVList = trail.FC("KVList", func(props KVListProps) trail.Node {
	st := styles.Use(newKVListStyles)
	winSize := trail.UseWindowSize()
	width := props.MinLabelWidth
	hasLabels := props.MinLabelWidth > 0
	var visible []KVItem
	for _, item := range props.Items {
		if item.OmitEmpty && item.Value == "" && item.ValueNode == nil {
			continue
		}
		visible = append(visible, item)
		if item.Label != "" {
			hasLabels = true
			width = max(width, ansi.StringWidth(item.Label))
		}
	}

	labelWidth := 0
	if hasLabels {
		labelWidth = width + 1
		if avail := winSize.Width - props.Indent; avail > 0 {
			labelWidth = min(labelWidth, max(1, avail/2))
		}
	}
	labelStyle := st.Label.Width(style.Cells(labelWidth))

	return trail.Box(trail.BoxProps{Style: st.List.PaddingLeft(props.Indent)},
		kitex.Map(visible, func(item KVItem, _ int) trail.Node {
			value := item.ValueNode
			if value == nil {
				face := item.Tone.textStyle(st.Common).WhiteSpace(style.WhiteSpacePreWrap)
				value = trail.Span(trail.SpanProps{Style: face}, trail.Text(item.Value))
			}
			labelText := ""
			if item.Label != "" {
				labelText = item.Label + ":"
			}
			return trail.Box(trail.BoxProps{Style: st.Row},
				kitex.If(hasLabels, func() trail.Node {
					return trail.Span(trail.SpanProps{Style: labelStyle}, trail.Text(labelText))
				}),
				trail.Box(trail.BoxProps{Style: st.Value}, value),
			)
		}),
	)
})
