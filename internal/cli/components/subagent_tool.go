package components

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// SubagentColor deterministically picks a palette color for an agent persona.
func SubagentColor(scheme *theme.Scheme, agentName string) color.Color {
	if scheme == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(agentName)) {
	case "explore":
		return scheme.Color.Agent.Subagents.Cyan
	case "general":
		return scheme.Color.Agent.Subagents.Purple
	}
	palette := []color.Color{
		scheme.Color.Agent.Subagents.Blue,
		scheme.Color.Agent.Subagents.Purple,
		scheme.Color.Agent.Subagents.Cyan,
		scheme.Color.Agent.Subagents.Green,
		scheme.Color.Agent.Subagents.Orange,
		scheme.Color.Agent.Subagents.Pink,
		scheme.Color.Agent.Subagents.Yellow,
		scheme.Color.Agent.Subagents.Red,
	}
	var h uint32 = 2166136261
	for i := 0; i < len(agentName); i++ {
		h ^= uint32(agentName[i])
		h *= 16777619
	}
	return palette[int(h)%len(palette)]
}

type InvokeAgentToolWidgetProps struct {
	Tool store.ToolPart
}

var InvokeAgentToolWidget = trail.FC("InvokeAgentToolWidget", func(props InvokeAgentToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	status := tool.Status

	input := parseToolInput[api.InvokeAgentInput](tool.Call)
	output := parseToolOutput[api.InvokeAgentOutput](tool.Response)

	isFailed := status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError) || output.Status == "failed"
	if isFailed {
		status = store.ToolFailed
	}
	isDone := status == store.ToolSucceeded || status == store.ToolFailed

	verb := "Spawning"
	if isDone {
		verb = "Spawned"
	}

	agentName := input.Agent
	if agentName == "" && output.Agent != "" {
		agentName = output.Agent
	}
	agentColor := SubagentColor(scheme, agentName)

	title := input.Title
	if title == "" && output.Title != "" {
		title = output.Title
	}

	leading := toolHeaderLeading(status, verb, true, scheme)
	headerNodes := append([]trail.Node{}, leading...)

	headerNodes = append(headerNodes, trail.Span(trail.SpanProps{
		Style: style.S().
			Flex(0, 0).
			WhiteSpace(style.WhiteSpacePre).
			Bold(true).
			Foreground(agentColor),
	}, trail.Text("@"+agentName)))

	if title != "" {
		headerNodes = append(headerNodes, trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 0).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Text.Primary),
		}, trail.Text(" "+title)))
	}

	var suffix string
	isWorktree := input.Workspace == "worktree"
	if output.SessionID != "" && isWorktree {
		suffix = fmt.Sprintf(" (%s, worktree)", output.SessionID)
	} else if output.SessionID != "" {
		suffix = fmt.Sprintf(" (%s)", output.SessionID)
	} else if isWorktree {
		suffix = " (worktree)"
	}

	if suffixNode := renderSuffixSpan(suffix, scheme); suffixNode != nil {
		headerNodes = append(headerNodes, suffixNode)
	}

	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil {
		headerNodes = append(headerNodes, stateSuffix)
	}

	return wrapToolWidget(headerNodes, toolErrorSubline(tool, scheme))
})

func InvokeAgentToolRenderer(tool store.ToolPart) trail.Node {
	return InvokeAgentToolWidget(InvokeAgentToolWidgetProps{Tool: tool})
}

type ManageAgentsToolWidgetProps struct {
	Tool store.ToolPart
}

var ManageAgentsToolWidget = trail.FC("ManageAgentsToolWidget", func(props ManageAgentsToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	status := tool.Status
	isDone := status == store.ToolSucceeded || status == store.ToolFailed

	input := parseToolInput[api.ManageAgentsInput](tool.Call)
	output := parseToolOutput[api.ManageAgentsOutput](tool.Response)

	var verb string
	var target string
	var suffix string

	switch input.Action {
	case "list":
		verb = "Listing agents"
		if isDone {
			verb = "Listed agents"
		}
		if tool.Response != nil && !tool.Response.IsError {
			n := len(output.Agents)
			if n == 1 {
				suffix = " (1 agent)"
			} else {
				suffix = fmt.Sprintf(" (%d agents)", n)
			}
		}
	case "status":
		verb = "Checking agent"
		if isDone {
			verb = "Checked agent"
		}
		target = input.SessionID
		if tool.Response != nil && !tool.Response.IsError && output.Status != "" {
			suffix = fmt.Sprintf(" (%s)", output.Status)
		}
	case "send":
		verb = "Messaging agent"
		if isDone {
			verb = "Messaged agent"
		}
		target = input.SessionID
		if tool.Response != nil && !tool.Response.IsError && output.Status != "" {
			suffix = fmt.Sprintf(" (%s)", output.Status)
		}
	case "kill":
		verb = "Killing agent"
		if isDone {
			verb = "Killed agent"
		}
		target = input.SessionID
		if tool.Response != nil && !tool.Response.IsError && output.Status != "" {
			suffix = fmt.Sprintf(" (%s)", output.Status)
		}
	default:
		verb = "Managing agents"
		if isDone {
			verb = "Managed agents"
		}
		target = input.SessionID
	}

	hasTarget := target != ""
	leading := toolHeaderLeading(status, verb, hasTarget, scheme)
	headerNodes := append([]trail.Node{}, leading...)

	if hasTarget {
		targetSpan := trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 0).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Text.Primary),
		}, trail.Text(target))
		headerNodes = append(headerNodes, targetSpan)
	}

	if suffixNode := renderSuffixSpan(suffix, scheme); suffixNode != nil {
		headerNodes = append(headerNodes, suffixNode)
	}

	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil {
		headerNodes = append(headerNodes, stateSuffix)
	}

	return wrapToolWidget(headerNodes, toolErrorSubline(tool, scheme))
})

func ManageAgentsToolRenderer(tool store.ToolPart) trail.Node {
	return ManageAgentsToolWidget(ManageAgentsToolWidgetProps{Tool: tool})
}
