package components

import (
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type MessageToolProps struct {
	Tool store.ToolPart
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
	case "ls":
		return LsToolRenderer(props.Tool)
	case "glob":
		return GlobToolRenderer(props.Tool)
	case "grep":
		return GrepToolRenderer(props.Tool)
	case "load_skill":
		return LoadSkillToolWidget(LoadSkillToolWidgetProps{Tool: props.Tool})
	case "write":
		return WriteToolRenderer(props.Tool)
	case "edit":
		return EditToolRenderer(props.Tool)
	case "remove":
		return RemoveToolRenderer(props.Tool)
	case "bash":
		return BashToolRenderer(props.Tool)
	case "web_fetch":
		return WebFetchToolRenderer(props.Tool)
	case "download":
		return DownloadToolRenderer(props.Tool)
	case "tasks":
		return TasksToolRenderer(props.Tool)
	case "todos":
		return TodosToolRenderer(props.Tool)
	case "ask_question":
		return QuestionToolRenderer(props.Tool)
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
		Overflow(style.OverflowHidden)

	labelStyle := style.S().WhiteSpace(style.WhiteSpaceNoWrap).Flex(0, 0).Foreground(scheme.Color.Text.Secondary)
	detailsStyle := style.S().
		WhiteSpace(style.WhiteSpaceNoWrap).
		Flex(0, 1).
		MinWidth(style.Cells(0)).
		Overflow(style.OverflowHidden).
		TextOverflow(style.TextOverflowEllipsis).
		Foreground(scheme.Color.Text.Secondary)
	return trail.Box(trail.BoxProps{Style: rowStyle},
		trail.Span(trail.SpanProps{Style: labelStyle}, trail.Text(prefix)),
		trail.Span(trail.SpanProps{Style: detailsStyle}, trail.Text(line)),
		trail.Span(trail.SpanProps{Style: labelStyle}, trail.Text(")")),
	)
})
