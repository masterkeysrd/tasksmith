package components

import (
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type planToolProps struct {
	Tool store.ToolPart
}

var enterPlanToolWidget = trail.FC("EnterPlanToolWidget", func(props planToolProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool

	verb := "Entering plan mode"
	switch tool.Status {
	case store.ToolSucceeded:
		verb = "Entered plan mode"
	case store.ToolFailed:
		verb = "Enter plan mode"
	}

	headerNodes := toolHeaderLeading(tool.Status, verb, false, scheme)
	if suffix := toolStateSuffix(tool.Status, scheme); suffix != nil {
		headerNodes = append(headerNodes, suffix)
	}
	return wrapToolWidget(tool, scheme, headerNodes)
})

var exitPlanToolWidget = trail.FC("ExitPlanToolWidget", func(props planToolProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool

	input := parseToolInput[api.ExitPlanModeInput](tool.Call)
	var output api.ExitPlanModeOutput
	if tool.Response != nil && !tool.Response.IsError {
		output = parseToolOutput[api.ExitPlanModeOutput](tool.Response)
	}

	targetPath := strings.TrimSpace(output.PlanPath)
	if targetPath == "" {
		targetPath = strings.TrimSpace(input.Path)
	}

	verb := "Reviewing plan"
	var subline string
	if tool.Response != nil && !tool.Response.IsError {
		if output.Approved {
			verb = "Approved plan"
			if output.Mode != "" {
				subline = "Switched to " + output.Mode + " mode"
			}
		} else {
			verb = "Kept planning"
			if strings.TrimSpace(output.Feedback) != "" {
				subline = output.Feedback
			}
		}
	} else if tool.Status == store.ToolFailed {
		verb = "Submit plan"
	}

	headerNodes := toolHeaderLeading(tool.Status, verb, targetPath != "", scheme)
	if targetPath != "" {
		headerNodes = append(headerNodes, trail.Span(trail.SpanProps{
			Style: style.S().Flex(0, 1).MinWidth(style.Cells(0)).Overflow(style.OverflowHidden).TextOverflow(style.TextOverflowEllipsis).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Primary),
		}, trail.Text(targetPath)))
	}
	if suffix := toolStateSuffix(tool.Status, scheme); suffix != nil {
		headerNodes = append(headerNodes, suffix)
	}

	var planSubline *toolSubline
	if subline != "" {
		planSubline = &toolSubline{
			prefixColor: scheme.Color.Text.Secondary,
			wrap:        true,
			nodes: []trail.Node{
				trail.Span(trail.SpanProps{
					Style: style.S().Flex(1, 1).MinWidth(style.Cells(0)).WhiteSpace(style.WhiteSpacePreWrap).OverflowWrap(style.OverflowWrapAnywhere).Foreground(scheme.Color.Text.Secondary),
				}, trail.Text(subline)),
			},
		}
	}
	return wrapToolWidget(tool, scheme, headerNodes, planSubline)
})

// PlanToolRenderer renders enter_plan_mode and exit_plan_mode tool invocations.
func PlanToolRenderer(tool store.ToolPart) trail.Node {
	var name string
	if tool.Call != nil {
		name = tool.Call.Name
	}
	if name == "" && tool.Response != nil {
		name = tool.Response.Name
	}
	if name == "enter_plan_mode" {
		return enterPlanToolWidget(planToolProps{Tool: tool})
	}
	return exitPlanToolWidget(planToolProps{Tool: tool})
}
