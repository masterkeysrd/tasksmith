package components

import (
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type LoadSkillToolWidgetProps struct {
	Tool store.ToolPart
}

var LoadSkillToolWidget = trail.FC("LoadSkillToolWidget", func(props LoadSkillToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	input := parseToolInput[api.LoadSkillInput](tool.Call)
	output := parseToolOutput[api.LoadSkillOutput](tool.Response)

	name := strings.TrimSpace(input.Skill)
	if tool.Response != nil && !tool.Response.IsError && strings.TrimSpace(output.Skill) != "" {
		name = strings.TrimSpace(output.Skill)
	}

	failed := tool.Status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError)
	status := tool.Status
	verb := "Loading Skill"
	if failed {
		status = store.ToolFailed
		verb = "Failed to load Skill"
	} else if status == store.ToolSucceeded {
		verb = "Loaded Skill"
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
		}, trail.Text("  ⎿  ⚠ Skill exceeds size limit (500 lines / 32 KB) and was truncated"))
	}

	return wrapToolWidget(headerNodes, subline)
})
