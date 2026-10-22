package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// UserPromptProps defines the properties for a user prompt scrollback item.
type UserPromptProps struct {
	Text string
}

// UserPrompt is a trail functional component rendering a user input prompt line.
var UserPrompt = trail.FC("UserPrompt", func(props UserPromptProps) trail.Node {
	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow),
	},
		trail.Span(trail.SpanProps{
			Style: theme.UserPromptStyle,
		}, trail.Text("❯ ")),
		trail.Span(trail.SpanProps{
			Style: theme.UserMsgStyle,
		}, trail.Text(props.Text)),
	)
})

// ToolCallProps defines the properties for a tool call indicator.
type ToolCallProps struct {
	ToolCall api.ToolCall
}

// ToolCall is a trail functional component rendering a tool invocation block.
var ToolCall = trail.FC("ToolCall", func(props ToolCallProps) trail.Node {
	tc := props.ToolCall
	argsStr := ""
	if len(tc.Args) > 0 {
		if raw, err := json.Marshal(tc.Args); err == nil {
			argsStr = " " + string(raw)
		}
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow),
	},
		trail.Span(trail.SpanProps{
			Style: theme.ToolBadgeStyle,
		}, trail.Text("[tool_call] ")),
		trail.Span(trail.SpanProps{
			Style: theme.ToolCallStyle,
		}, trail.Text(fmt.Sprintf("%s (%s)%s", tc.Name, tc.ID, argsStr))),
	)
})

// MessageProps defines the properties for an api.Message item.
type MessageProps struct {
	Message api.Message
}

// ExtractTextBlocks aggregates text from text-type content blocks.
func ExtractTextBlocks(blocks []api.Block) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == api.BlockText {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// Message is a trail functional component rendering a user, assistant, or tool message.
var Message = trail.FC("Message", func(props MessageProps) trail.Node {
	msg := props.Message
	switch msg.Role {
	case api.RoleUser:
		text := ExtractTextBlocks(msg.Content)
		return trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		},
			trail.Span(trail.SpanProps{
				Style: theme.UserBadgeStyle,
			}, trail.Text("[user] ")),
			trail.Span(trail.SpanProps{
				Style: theme.UserMsgStyle,
			}, trail.Text(text)),
		)

	case api.RoleAssistant:
		var rows []trail.Node
		var reasoning strings.Builder
		var text strings.Builder
		for _, b := range msg.Content {
			switch b.Type {
			case api.BlockReasoning:
				reasoning.WriteString(b.Text)
			case api.BlockText:
				text.WriteString(b.Text)
			case api.BlockToolCall:
				if b.ToolCall != nil {
					rows = append(rows, ToolCall(ToolCallProps{ToolCall: *b.ToolCall}))
				}
			}
		}

		if r := strings.TrimSpace(reasoning.String()); r != "" {
			rows = append([]trail.Node{
				trail.Box(trail.BoxProps{
					Style: style.S().FlexDirection(style.FlexRow),
				},
					trail.Span(trail.SpanProps{
						Style: theme.ReasoningStyle,
					}, trail.Text(fmt.Sprintf("[reasoning] %s", r))),
				),
			}, rows...)
		}

		if t := text.String(); t != "" {
			rows = append(rows, trail.Box(trail.BoxProps{
				Style: style.S().FlexDirection(style.FlexRow),
			},
				trail.Span(trail.SpanProps{
					Style: theme.AgentBadgeStyle,
				}, trail.Text("[assistant] ")),
				trail.Span(trail.SpanProps{
					Style: theme.AssistantMsgStyle,
				}, trail.Text(t)),
			))
		}

		if len(rows) == 0 {
			return trail.Box(trail.BoxProps{
				Style: style.S().FlexDirection(style.FlexRow),
			},
				trail.Span(trail.SpanProps{
					Style: theme.AgentBadgeStyle,
				}, trail.Text("[assistant] ")),
			)
		}

		return trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexColumn),
		}, rows...)

	case api.RoleTool:
		text := ExtractTextBlocks(msg.Content)
		name := msg.Name
		if name == "" {
			name = msg.ToolCallID
		}
		prefix := "[tool] "
		badgeStyle := theme.ToolSuccessStyle
		textStyle := theme.ToolResultStyle
		if msg.IsError {
			prefix = "[tool:error] "
			badgeStyle = theme.ToolErrorStyle
			textStyle = theme.ErrorStyle
		}

		var spans []trail.Node
		spans = append(spans, trail.Span(trail.SpanProps{
			Style: badgeStyle,
		}, trail.Text(prefix)))

		if name != "" {
			spans = append(spans,
				trail.Span(trail.SpanProps{
					Style: theme.ToolCallStyle,
				}, trail.Text(name+": ")),
			)
		}

		spans = append(spans, trail.Span(trail.SpanProps{
			Style: textStyle,
		}, trail.Text(text)))

		return trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, spans...)

	default:
		text := ExtractTextBlocks(msg.Content)
		return trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		},
			trail.Span(trail.SpanProps{
				Style: theme.StatusBarStyle,
			}, trail.Text(fmt.Sprintf("[%s] %s", msg.Role, text))),
		)
	}
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
