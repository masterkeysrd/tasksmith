package components

import (
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type LoadRuleToolWidgetProps struct {
	Tool store.ToolPart
}

var LoadRuleToolWidget = trail.FC("LoadRuleToolWidget", func(props LoadRuleToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	input := parseToolInput[api.LoadRuleInput](tool.Call)
	output := parseToolOutput[api.LoadRuleOutput](tool.Response)

	name := strings.TrimSpace(input.Rule)
	if tool.Response != nil && !tool.Response.IsError && strings.TrimSpace(output.Rule) != "" {
		name = strings.TrimSpace(output.Rule)
	}

	failed := tool.Status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError)
	status := tool.Status
	verb := "Loading Rule"
	if failed {
		status = store.ToolFailed
		verb = "Failed to load Rule"
	} else if status == store.ToolSucceeded {
		verb = "Loaded Rule"
	}

	headerNodes := toolHeaderLeading(status, verb, name != "", scheme)
	if name != "" {
		headerNodes = append(headerNodes, trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 1).
				MinWidth(style.Cells(0)).
				WhiteSpace(style.WhiteSpaceNoWrap).
				Overflow(style.OverflowHidden).
				TextOverflow(style.TextOverflowEllipsis).
				Foreground(scheme.Color.Text.Primary),
		}, trail.Text(name)))
	}
	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil && !failed {
		headerNodes = append(headerNodes, stateSuffix)
	}

	subline := toolErrorSubline(tool, scheme)
	if subline == nil && !failed && output.Truncated {
		subline = trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 0).
				WhiteSpace(style.WhiteSpacePre).
				Overflow(style.OverflowHidden).
				TextOverflow(style.TextOverflowEllipsis).
				Foreground(scheme.Color.Text.Warning),
		}, trail.Text("  ⎿  ⚠ Rule exceeds size limit (500 lines / 32 KB) and was truncated"))
	}

	return wrapToolWidget(headerNodes, subline)
})
