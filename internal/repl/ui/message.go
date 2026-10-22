package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
	"github.com/masterkeysrd/tasksmith/internal/ui/components"
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

// ReasoningProps defines the properties for rendering a reasoning block.
type ReasoningProps struct {
	Text     string
	Duration time.Duration // optional elapsed duration (e.g. 6200ms -> "6.2s")
	Running  bool          // true while actively streaming reasoning chunks
}

// FormatSingleLineReasoning collapses multi-line reasoning into a single line with an ellipsis.
func FormatSingleLineReasoning(r string) string {
	r = strings.TrimSpace(r)
	if r == "" {
		return ""
	}
	lines := strings.Split(r, "\n")
	firstLine := ""
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			firstLine = trimmed
			break
		}
	}
	if len(lines) > 1 {
		firstLine = strings.TrimRight(firstLine, ". ") + "..."
	}
	return firstLine
}

// Reasoning is a trail functional component rendering a single-line reasoning block with ellipsis.
var Reasoning = trail.FC("Reasoning", func(props ReasoningProps) trail.Node {
	line := FormatSingleLineReasoning(props.Text)
	if line == "" {
		return nil
	}
	var prefix string
	if props.Running {
		prefix = "○ Thinking ("
	} else if props.Duration > 0 {
		prefix = fmt.Sprintf("○ Thought for %.1fs (", props.Duration.Seconds())
	} else {
		prefix = "○ Thought ("
	}
	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Width(style.Percent(100)).
			MaxWidth(style.Percent(100)),
	},
		trail.Span(trail.SpanProps{
			Style: theme.ReasoningStyle.WhiteSpace(style.WhiteSpaceNoWrap).Flex(0, 0),
		}, trail.Text(prefix)),
		trail.Span(trail.SpanProps{
			Style: theme.ReasoningStyle.
				WhiteSpace(style.WhiteSpaceNoWrap).
				Flex(0, 1).
				MinWidth(style.Cells(0)).
				Overflow(style.OverflowHidden).
				TextOverflow(style.TextOverflowEllipsis),
		}, trail.Text(line)),
		trail.Span(trail.SpanProps{
			Style: theme.ReasoningStyle.WhiteSpace(style.WhiteSpaceNoWrap).Flex(0, 0),
		}, trail.Text(")")),
	)
})

// ToolCallProps defines the properties for a single tool call indicator.
type ToolCallProps struct {
	ToolCall  api.ToolCall
	Running   bool
	ErrorText string
}

// ToolGroupProps defines the properties for grouped tool calls.
type ToolGroupProps struct {
	Calls     []api.ToolCall
	Running   bool
	ErrorText string
}

// ToolErrorProps defines properties for a compact indented error block.
type ToolErrorProps struct {
	ErrorText string
}

// ToolError renders a compact indented error indicator under tool calls or results.
var ToolError = trail.FC("ToolError", func(props ToolErrorProps) trail.Node {
	firstLine := strings.TrimSpace(props.ErrorText)
	if lines := strings.Split(firstLine, "\n"); len(lines) > 0 {
		firstLine = strings.TrimSpace(lines[0])
	}
	if firstLine == "" {
		return nil
	}
	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow),
	},
		trail.Span(trail.SpanProps{
			Style: theme.StatusBarStyle.WhiteSpace(style.WhiteSpacePre),
		}, trail.Text("  ⎿  ")),
		trail.Span(trail.SpanProps{
			Style: theme.ToolErrorStyle,
		}, trail.Text("✗ ")),
		trail.Span(trail.SpanProps{
			Style: theme.ErrorStyle,
		}, trail.Text(firstLine)),
	)
})

func toolCategory(name string) string {
	switch name {
	case "bash":
		return "bash"
	case "view", "ls", "glob":
		return "explore"
	case "grep":
		return "search"
	case "edit":
		return "edit"
	case "write":
		return "write"
	case "remove":
		return "remove"
	case "web_fetch", "download":
		return "fetch"
	default:
		return name
	}
}

func toolPrimaryArg(tc api.ToolCall) string {
	if tc.Args == nil {
		return ""
	}
	var raw string
	switch tc.Name {
	case "bash":
		if v, ok := tc.Args["command"].(string); ok {
			raw = v
		}
	case "view":
		if v, ok := tc.Args["path"].(string); ok {
			raw = filepath.Base(v)
		}
	case "ls":
		if v, ok := tc.Args["path"].(string); ok {
			raw = v
		} else if v, ok := tc.Args["dir"].(string); ok {
			raw = v
		}
	case "glob":
		if v, ok := tc.Args["pattern"].(string); ok {
			raw = v
		}
	case "grep":
		if v, ok := tc.Args["pattern"].(string); ok {
			raw = v
		}
	case "edit", "write", "remove":
		if v, ok := tc.Args["path"].(string); ok {
			raw = v
		}
	case "web_fetch", "download":
		if v, ok := tc.Args["url"].(string); ok {
			raw = v
		}
	default:
		for _, k := range []string{"path", "pattern", "command", "url", "query", "name"} {
			if v, ok := tc.Args[k].(string); ok && v != "" {
				raw = v
				break
			}
		}
	}
	raw = strings.ReplaceAll(raw, "\n", " ")
	return strings.TrimSpace(raw)
}

// GroupToolCalls clusters consecutive tool calls belonging to the same category.
func GroupToolCalls(calls []api.ToolCall) [][]api.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	var groups [][]api.ToolCall
	var currentGroup []api.ToolCall
	var currentCat string

	for _, call := range calls {
		cat := toolCategory(call.Name)
		if len(currentGroup) == 0 {
			currentGroup = []api.ToolCall{call}
			currentCat = cat
		} else if cat == currentCat {
			currentGroup = append(currentGroup, call)
		} else {
			groups = append(groups, currentGroup)
			currentGroup = []api.ToolCall{call}
			currentCat = cat
		}
	}
	if len(currentGroup) > 0 {
		groups = append(groups, currentGroup)
	}
	return groups
}

// FormatToolGroupSummary formats the descriptive action text for a tool call group.
func FormatToolGroupSummary(calls []api.ToolCall, running bool) string {
	if len(calls) == 0 {
		return ""
	}
	n := len(calls)
	lastCall := calls[n-1]
	cat := toolCategory(lastCall.Name)
	lastArg := toolPrimaryArg(lastCall)

	switch cat {
	case "bash":
		if n == 1 {
			if running {
				if lastArg != "" {
					return fmt.Sprintf("Running (%s)", lastArg)
				}
				return "Running"
			}
			if lastArg != "" {
				return fmt.Sprintf("Ran (%s)", lastArg)
			}
			return "Ran"
		}
		if running {
			return fmt.Sprintf("Running %d commands (+%d · %s)", n, n-1, lastArg)
		}
		return fmt.Sprintf("Ran %d commands (+%d · %s)", n, n-1, lastArg)

	case "explore":
		if n == 1 {
			if running {
				if lastArg != "" {
					return fmt.Sprintf("Exploring 1 file (%s)", lastArg)
				}
				return "Exploring 1 file"
			}
			if lastArg != "" {
				return fmt.Sprintf("Explored 1 file (%s)", lastArg)
			}
			return "Explored 1 file"
		}
		if running {
			return fmt.Sprintf("Exploring %d files (+%d · %s)", n, n-1, lastArg)
		}
		return fmt.Sprintf("Explored %d files (+%d · %s)", n, n-1, lastArg)

	case "search":
		if n == 1 {
			if running {
				if lastArg != "" {
					return fmt.Sprintf("Searching (%s)", lastArg)
				}
				return "Searching"
			}
			if lastArg != "" {
				return fmt.Sprintf("Searched (%s)", lastArg)
			}
			return "Searched"
		}
		if running {
			return fmt.Sprintf("Searching %d patterns (+%d · %s)", n, n-1, lastArg)
		}
		return fmt.Sprintf("Searched %d patterns (+%d · %s)", n, n-1, lastArg)

	case "edit":
		if n == 1 {
			if running {
				if lastArg != "" {
					return fmt.Sprintf("Editing (%s)", lastArg)
				}
				return "Editing"
			}
			if lastArg != "" {
				return fmt.Sprintf("Edited (%s)", lastArg)
			}
			return "Edited"
		}
		if running {
			return fmt.Sprintf("Editing %d files (+%d · %s)", n, n-1, lastArg)
		}
		return fmt.Sprintf("Edited %d files (+%d · %s)", n, n-1, lastArg)

	case "write":
		if n == 1 {
			if running {
				if lastArg != "" {
					return fmt.Sprintf("Writing (%s)", lastArg)
				}
				return "Writing"
			}
			if lastArg != "" {
				return fmt.Sprintf("Wrote (%s)", lastArg)
			}
			return "Wrote"
		}
		if running {
			return fmt.Sprintf("Writing %d files (+%d · %s)", n, n-1, lastArg)
		}
		return fmt.Sprintf("Wrote %d files (+%d · %s)", n, n-1, lastArg)

	case "remove":
		if n == 1 {
			if running {
				if lastArg != "" {
					return fmt.Sprintf("Removing (%s)", lastArg)
				}
				return "Removing"
			}
			if lastArg != "" {
				return fmt.Sprintf("Removed (%s)", lastArg)
			}
			return "Removed"
		}
		if running {
			return fmt.Sprintf("Removing %d files (+%d · %s)", n, n-1, lastArg)
		}
		return fmt.Sprintf("Removed %d files (+%d · %s)", n, n-1, lastArg)

	case "fetch":
		if n == 1 {
			if running {
				if lastArg != "" {
					return fmt.Sprintf("Fetching (%s)", lastArg)
				}
				return "Fetching"
			}
			if lastArg != "" {
				return fmt.Sprintf("Fetched (%s)", lastArg)
			}
			return "Fetched"
		}
		if running {
			return fmt.Sprintf("Fetching %d URLs (+%d · %s)", n, n-1, lastArg)
		}
		return fmt.Sprintf("Fetched %d URLs (+%d · %s)", n, n-1, lastArg)

	default:
		name := lastCall.Name
		if n == 1 {
			if running {
				if lastArg != "" {
					return fmt.Sprintf("Calling %s (%s)", name, lastArg)
				}
				return fmt.Sprintf("Calling %s", name)
			}
			if lastArg != "" {
				return fmt.Sprintf("Called %s (%s)", name, lastArg)
			}
			return fmt.Sprintf("Called %s", name)
		}
		if running {
			return fmt.Sprintf("Calling %s %d times", name, n)
		}
		return fmt.Sprintf("Called %s %d times", name, n)
	}
}

// ToolCall is a trail functional component rendering a single tool invocation block.
var ToolCall = trail.FC("ToolCall", func(props ToolCallProps) trail.Node {
	return ToolGroup(ToolGroupProps{
		Calls:     []api.ToolCall{props.ToolCall},
		Running:   props.Running,
		ErrorText: props.ErrorText,
	})
})

// ToolGroup is a trail functional component rendering one or more grouped tool calls.
var ToolGroup = trail.FC("ToolGroup", func(props ToolGroupProps) trail.Node {
	if len(props.Calls) == 0 {
		return nil
	}
	bullet := "● "
	bulletStyle := theme.ToolBadgeStyle
	if props.Running {
		bullet = "○ "
	} else if props.ErrorText != "" {
		bulletStyle = theme.ToolErrorStyle
	}

	summary := FormatToolGroupSummary(props.Calls, props.Running)

	var spans []trail.Node
	spans = append(spans, trail.Span(trail.SpanProps{
		Style: bulletStyle.WhiteSpace(style.WhiteSpaceNoWrap).Flex(0, 0),
	}, trail.Text(bullet)))

	if idx := strings.Index(summary, " ("); idx != -1 && strings.HasSuffix(summary, ")") {
		prefix := summary[:idx+2] // e.g. "Ran (" or "Ran 2 commands (+1 · "
		inner := summary[idx+2 : len(summary)-1]
		suffix := ")"

		spans = append(spans,
			trail.Span(trail.SpanProps{
				Style: theme.ToolCallStyle.WhiteSpace(style.WhiteSpaceNoWrap).Flex(0, 0),
			}, trail.Text(prefix)),
			trail.Span(trail.SpanProps{
				Style: theme.ToolCallStyle.
					WhiteSpace(style.WhiteSpaceNoWrap).
					Flex(0, 1).
					MinWidth(style.Cells(0)).
					Overflow(style.OverflowHidden).
					TextOverflow(style.TextOverflowEllipsis),
			}, trail.Text(inner)),
			trail.Span(trail.SpanProps{
				Style: theme.ToolCallStyle.WhiteSpace(style.WhiteSpaceNoWrap).Flex(0, 0),
			}, trail.Text(suffix)),
		)
	} else {
		spans = append(spans, trail.Span(trail.SpanProps{
			Style: theme.ToolCallStyle.
				WhiteSpace(style.WhiteSpaceNoWrap).
				Flex(0, 1).
				MinWidth(style.Cells(0)).
				Overflow(style.OverflowHidden).
				TextOverflow(style.TextOverflowEllipsis),
		}, trail.Text(summary)))
	}

	nodes := []trail.Node{
		trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				Width(style.Percent(100)).
				MaxWidth(style.Percent(100)),
		}, spans...),
	}

	if props.ErrorText != "" {
		firstLine := strings.TrimSpace(props.ErrorText)
		if lines := strings.Split(firstLine, "\n"); len(lines) > 0 {
			firstLine = strings.TrimSpace(lines[0])
		}
		if firstLine != "" {
			nodes = append(nodes, trail.Box(trail.BoxProps{
				Style: style.S().FlexDirection(style.FlexRow),
			},
				trail.Span(trail.SpanProps{
					Style: theme.StatusBarStyle.WhiteSpace(style.WhiteSpacePre),
				}, trail.Text("  ⎿  ")),
				trail.Span(trail.SpanProps{
					Style: theme.ToolErrorStyle,
				}, trail.Text("✗ ")),
				trail.Span(trail.SpanProps{
					Style: theme.ErrorStyle,
				}, trail.Text(firstLine)),
			))
		}
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn),
	}, nodes...)
})

// MessageProps defines the properties for an api.Message item.
type MessageProps struct {
	Message           api.Message
	ReasoningDuration time.Duration
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
		return UserPrompt(UserPromptProps{Text: text})

	case api.RoleAssistant:
		var rows []trail.Node
		var reasoning strings.Builder
		var toolCalls []api.ToolCall
		var text strings.Builder
		for _, b := range msg.Content {
			switch b.Type {
			case api.BlockReasoning:
				reasoning.WriteString(b.Text)
			case api.BlockToolCall:
				if b.ToolCall != nil {
					toolCalls = append(toolCalls, *b.ToolCall)
				}
			case api.BlockText:
				text.WriteString(b.Text)
			}
		}

		if r := strings.TrimSpace(reasoning.String()); r != "" {
			rows = append(rows, Reasoning(ReasoningProps{
				Text:     r,
				Duration: props.ReasoningDuration,
				Running:  false,
			}))
		}

		if len(toolCalls) > 0 {
			groups := GroupToolCalls(toolCalls)
			for _, grp := range groups {
				rows = append(rows, ToolGroup(ToolGroupProps{
					Calls:   grp,
					Running: false,
				}))
			}
		}

		hasText := false
		if t := strings.TrimSpace(text.String()); t != "" {
			hasText = true
			rows = append(rows, components.Markdown(components.MarkdownProps{
				Source: text.String(),
			}))
		}

		if len(rows) == 0 {
			return nil
		}

		boxStyle := style.S().FlexDirection(style.FlexColumn)
		if hasText {
			boxStyle = boxStyle.Margin(0, 0, 1, 0)
		}

		return trail.Box(trail.BoxProps{
			Style: boxStyle,
		}, rows...)

	case api.RoleTool:
		if !msg.IsError {
			return nil
		}
		text := ExtractTextBlocks(msg.Content)
		return ToolError(ToolErrorProps{ErrorText: text})

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

// RenderHistoryMessages transforms a session message history into trail UI nodes,
// coalescing consecutive tool calls across turns into grouped ToolGroup components.
func RenderHistoryMessages(hist []api.Message) []trail.Node {
	var nodes []trail.Node
	var pendingTools []api.ToolCall

	flushTools := func() {
		if len(pendingTools) > 0 {
			groups := GroupToolCalls(pendingTools)
			for _, grp := range groups {
				nodes = append(nodes, ToolGroup(ToolGroupProps{
					Calls:   grp,
					Running: false,
				}))
			}
			pendingTools = nil
		}
	}

	for _, msg := range hist {
		switch msg.Role {
		case api.RoleUser:
			flushTools()
			nodes = append(nodes, Message(MessageProps{Message: msg}))

		case api.RoleAssistant:
			var reasoning strings.Builder
			var toolCalls []api.ToolCall
			var text strings.Builder
			for _, b := range msg.Content {
				switch b.Type {
				case api.BlockReasoning:
					reasoning.WriteString(b.Text)
				case api.BlockToolCall:
					if b.ToolCall != nil {
						toolCalls = append(toolCalls, *b.ToolCall)
					}
				case api.BlockText:
					text.WriteString(b.Text)
				}
			}

			r := strings.TrimSpace(reasoning.String())
			t := strings.TrimSpace(text.String())

			if r != "" {
				flushTools()
				nodes = append(nodes, Reasoning(ReasoningProps{
					Text:    r,
					Running: false,
				}))
			}

			if len(toolCalls) > 0 {
				if t != "" {
					pendingTools = append(pendingTools, toolCalls...)
					flushTools()
				} else {
					pendingTools = append(pendingTools, toolCalls...)
				}
			}

			if t != "" {
				flushTools()
				nodes = append(nodes, trail.Box(trail.BoxProps{
					Style: style.S().FlexDirection(style.FlexColumn).Margin(0, 0, 1, 0),
				}, components.Markdown(components.MarkdownProps{
					Source: text.String(),
				})))
			}

		case api.RoleTool:
			if msg.IsError {
				flushTools()
				nodes = append(nodes, ToolError(ToolErrorProps{
					ErrorText: ExtractTextBlocks(msg.Content),
				}))
			}
			// Non-error tool outputs are suppressed because tool calls are grouped and displayed above.

		default:
			flushTools()
			nodes = append(nodes, Message(MessageProps{Message: msg}))
		}
	}

	flushTools()
	return nodes
}
