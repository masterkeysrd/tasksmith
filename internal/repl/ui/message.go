package ui

import (
	"fmt"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// UserPromptProps defines the properties for a user prompt scrollback item.
type UserPromptProps struct {
	Text     string
	NoMargin bool
}

// UserPrompt is a trail functional component rendering a user input prompt line.
var UserPrompt = trail.FC("UserPrompt", func(props UserPromptProps) trail.Node {
	s := style.S().FlexDirection(style.FlexRow)
	if !props.NoMargin {
		s = s.Margin(0, 0, 1, 0)
	}
	return trail.Box(trail.BoxProps{
		Style: s,
	},
		trail.Span(trail.SpanProps{
			Style: theme.UserPromptStyle,
		}, trail.Text("> ")),
		trail.Span(trail.SpanProps{
			Style: theme.UserMsgStyle,
		}, trail.Text(props.Text)),
	)
})

// CommandOutputProps defines the properties for a command execution result.
type CommandOutputProps struct {
	Command  string       // e.g. "/mode" or "/mode plan"
	Output   string       // e.g. "Switched to plan mode." or "Exited /mode command"
	Children []trail.Node // Optional children (injected by trail.FCC)
}

// CommandOutput is a trail functional component rendering a command prompt line and its indented result indicator.
var CommandOutput = trail.FCC("CommandOutput", func(props CommandOutputProps) trail.Node {
	rows := []trail.Node{
		UserPrompt(UserPromptProps{Text: props.Command, NoMargin: true}),
		trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		},
			trail.Span(trail.SpanProps{
				Style: theme.StatusBarStyle.WhiteSpace(style.WhiteSpacePre),
			}, trail.Text("  ⎿  ")),
			trail.Span(trail.SpanProps{
				Style: theme.StatusBarStyle,
			}, trail.Text(props.Output)),
		),
	}
	if len(props.Children) > 0 {
		rows = append(rows, trail.Box(trail.BoxProps{
			Style: style.S().Margin(0, 0, 0, 5).FlexDirection(style.FlexColumn),
		}, props.Children...))
	}
	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn),
	}, rows...)
})

// DecisionSummaryProps defines the properties for a permission decision summary item.
type DecisionSummaryProps struct {
	Request  api.PermissionRequest
	Decision api.PermissionDecision
}

// DecisionSummary is a trail functional component rendering a 1-line resolution indicator.
var DecisionSummary = trail.FC("DecisionSummary", func(props DecisionSummaryProps) trail.Node {
	perm := props.Request
	decision := props.Decision

	target := perm.Summary
	if target == "" {
		target = perm.ToolName
	}

	var summary string
	s := theme.AllowDecisionStyle
	switch decision.Kind {
	case api.DecisionAllowOnce:
		summary = fmt.Sprintf("✓ Allowed once: %s", target)
	case api.DecisionAllowSession:
		summary = fmt.Sprintf("✓ Allowed for session: %s", target)
	default:
		s = theme.RejectDecisionStyle
		if decision.Feedback != "" {
			summary = fmt.Sprintf("✗ Rejected: %s (%s)", target, decision.Feedback)
		} else {
			summary = fmt.Sprintf("✗ Rejected: %s", target)
		}
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow),
	},
		trail.Span(trail.SpanProps{
			Style: s,
		}, trail.Text(summary)),
	)
})

// ErrorMessageProps defines the properties for an error notification item.
type ErrorMessageProps struct {
	Text string
}

// ErrorMessage is a trail functional component rendering a styled error line.
var ErrorMessage = trail.FC("ErrorMessage", func(props ErrorMessageProps) trail.Node {
	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow),
	},
		trail.Span(trail.SpanProps{
			Style: theme.ErrorBadgeStyle,
		}, trail.Text("error: ")),
		trail.Span(trail.SpanProps{
			Style: theme.ErrorStyle,
		}, trail.Text(props.Text)),
	)
})

// NoticeProps defines the properties for an informational notice item.
type NoticeProps struct {
	Text string
}

// Notice is a trail functional component rendering a dim status notice line.
var Notice = trail.FC("Notice", func(props NoticeProps) trail.Node {
	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow),
	},
		trail.Span(trail.SpanProps{
			Style: theme.StatusBarStyle,
		}, trail.Text(props.Text)),
	)
})
