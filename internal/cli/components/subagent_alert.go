package components

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// SubagentAlert renders a non-blocking alert banner above the composer when a child subagent
// requires user authorization or an answer to a question.
var SubagentAlert = trail.SimpleFC("SubagentAlert", func() trail.Node {
	if store.UseHasActiveSlot() {
		return nil
	}
	waiting := store.UseWaitingSubagents()
	if len(waiting) == 0 {
		return nil
	}

	sub := waiting[0]
	scheme := theme.UseTheme()

	title := strings.TrimSpace(strings.ReplaceAll(sub.Title, "\n", " "))
	if title == "" {
		title = sub.ID
	}
	agentName := sub.Config.Agent
	if agentName == "" {
		agentName = "subagent"
	}
	agentColor := SubagentColor(scheme, agentName)

	border := style.SingleBorder().Top(false).Bottom(false).Right(false).Left(true).Color(scheme.Color.Border.Warning)
	containerStyle := style.S().
		Border(border).
		PaddingLeft(1).
		MarginBottom(1).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100))

	headerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		Width(style.Percent(100)).
		WhiteSpace(style.WhiteSpaceNoWrap)

	var lines []trail.Node

	moreWaiting := ""
	if len(waiting) > 1 {
		moreWaiting = fmt.Sprintf(" (+%d more waiting)", len(waiting)-1)
	}

	if len(sub.PendingPermissions) > 0 {
		req := sub.PendingPermissions[0]
		// Line 1: ⚠ Subagent @<agent> (<title>) requires permission
		lines = append(lines, trail.Box(trail.BoxProps{Style: headerStyle},
			trail.Span(trail.SpanProps{
				Style: style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Warning),
			}, trail.Text("⚠ Subagent ")),
			trail.Span(trail.SpanProps{
				Style: style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(agentColor),
			}, trail.Text("@"+agentName)),
			trail.Span(trail.SpanProps{
				Style: style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Primary),
			}, trail.Text(fmt.Sprintf(" (%s) requires permission", title))),
		))

		// Line 2: <tool_name> · <target or summary>
		targetOrSummary := req.Target
		if targetOrSummary == "" {
			targetOrSummary = req.Summary
		}
		if targetOrSummary == "" {
			targetOrSummary = req.ID
		}
		toolLabel := req.ToolName
		if toolLabel == "" {
			toolLabel = "tool"
		}
		lines = append(lines, trail.Box(trail.BoxProps{Style: headerStyle},
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Text.Secondary),
			}, trail.Text(toolLabel+" · "+targetOrSummary)),
		))

		// Line 3: ctrl+y allow once • ctrl+g manage subagent (+N more waiting)
		actionHint := "ctrl+y allow once • ctrl+g manage subagent" + moreWaiting
		lines = append(lines, trail.Box(trail.BoxProps{Style: headerStyle},
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Text.Secondary),
			}, trail.Text(actionHint)),
		))
	} else if len(sub.PendingQuestions) > 0 || sub.Status == api.StatusWaitingQuestion {
		// Line 1: ⚠ Subagent @<agent> (<title>) is waiting for an answer
		lines = append(lines, trail.Box(trail.BoxProps{Style: headerStyle},
			trail.Span(trail.SpanProps{
				Style: style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Warning),
			}, trail.Text("⚠ Subagent ")),
			trail.Span(trail.SpanProps{
				Style: style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(agentColor),
			}, trail.Text("@"+agentName)),
			trail.Span(trail.SpanProps{
				Style: style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Primary),
			}, trail.Text(fmt.Sprintf(" (%s) is waiting for an answer", title))),
		))

		// Line 2: ctrl+g manage subagent (+N more waiting)
		actionHint := "ctrl+g manage subagent" + moreWaiting
		lines = append(lines, trail.Box(trail.BoxProps{Style: headerStyle},
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Text.Secondary),
			}, trail.Text(actionHint)),
		))
	}

	return trail.Box(trail.BoxProps{
		Style: containerStyle,
	}, lines...)
})
