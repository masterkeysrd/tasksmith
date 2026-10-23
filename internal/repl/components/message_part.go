package components

import (
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/hooks"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type MessageToolProps struct {
	Tool hooks.ToolPart
}

var MessageTool = trail.FC("MessageTool", func(props MessageToolProps) trail.Node {
	var name string
	if props.Tool.Call != nil {
		name = props.Tool.Call.Name
	}
	if name == "" && props.Tool.Response != nil {
		name = props.Tool.Response.Name
	}
	switch name {
	case "view":
		return ViewToolRenderer(props.Tool)
	case "write":
		return WriteToolRenderer(props.Tool)
	case "ls":
		return LsRenderer(props.Tool)
	case "bash":
		return BashToolRenderer(props.Tool)
	default:
		return DefaultToolRenderer(props.Tool)
	}
})

type MessageTextProps struct {
	Text string
}

var MessageText = trail.FC("MessageText", func(props MessageTextProps) trail.Node {
	return uicomponents.Markdown(uicomponents.MarkdownProps{Source: props.Text})
})

type MessageThoughtProps struct {
	Text      string
	Streaming bool
}

var MessageThought = trail.FC("MessageThought", func(props MessageThoughtProps) trail.Node {
	scheme := theme.UseTheme()
	text := strings.TrimSpace(props.Text)
	if text == "" {
		return nil
	}
	line, _, multiline := strings.Cut(text, "\n")
	line = strings.TrimSpace(line)
	if multiline {
		line = strings.TrimRight(line, ". ") + "..."
	}
	prefix := "● Thought ("
	if props.Streaming {
		prefix = "○ Thinking ("
	}
	rowStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		MinWidth(style.Cells(0)).
		Overflow(style.OverflowHidden).
		Italic(true)
	if scheme != nil {
		rowStyle = rowStyle.Foreground(scheme.Color.Text.Tertiary)
	}
	labelStyle := style.S().WhiteSpace(style.WhiteSpaceNoWrap).Flex(0, 0)
	detailsStyle := style.S().
		WhiteSpace(style.WhiteSpaceNoWrap).
		Flex(0, 1).
		MinWidth(style.Cells(0)).
		Overflow(style.OverflowHidden).
		TextOverflow(style.TextOverflowEllipsis)
	return trail.Box(trail.BoxProps{Style: rowStyle},
		trail.Span(trail.SpanProps{Style: labelStyle}, trail.Text(prefix)),
		trail.Span(trail.SpanProps{Style: detailsStyle}, trail.Text(line)),
		trail.Span(trail.SpanProps{Style: labelStyle}, trail.Text(")")),
	)
})
