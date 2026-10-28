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

type MessageProps struct {
	Message store.Message
}

var Message = trail.FC("Message", func(props MessageProps) trail.Node {
	switch props.Message.Role {
	case api.RoleUser:
		return UserMessage(props)
	case api.RoleAssistant:
		return AssistantMessage(props)
	case api.RoleSystem:
		return SystemMessage(props)
	case api.RoleTool:
		return messageBody(props.Message)
	default:
		return messageError(fmt.Sprintf("Unable to render message: unsupported role %q", props.Message.Role))
	}
})

var UserMessage = trail.FC("UserMessage", func(props MessageProps) trail.Node {
	scheme := theme.UseTheme()
	parts := messageParts(props.Message)
	if len(parts) == 0 {
		return nil
	}
	rowStyle := messageStyle.FlexDirection(style.FlexRow).Bold(true).Foreground(scheme.Color.Icon.Primary)
	return trail.Box(trail.BoxProps{Key: props.Message.ID, Style: rowStyle},
		trail.Span(trail.SpanProps{
			Style: style.S().WhiteSpace(style.WhiteSpaceNoWrap).Flex(0, 0).Bold(true).Foreground(scheme.Color.Icon.Primary),
		}, trail.Text("> ")),
		trail.Box(trail.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Flex(1, 1).MinWidth(style.Cells(0)),
		}, parts...),
	)
})

var AssistantMessage = trail.FC("AssistantMessage", func(props MessageProps) trail.Node {
	return messageBody(props.Message)
})

var SystemMessage = trail.FC("SystemMessage", func(props MessageProps) trail.Node {
	scheme := theme.UseTheme()
	parts := messageParts(props.Message)
	if len(parts) == 0 {
		return nil
	}
	rowStyle := messageStyle.FlexDirection(style.FlexRow).Foreground(scheme.Color.Text.Secondary)
	return trail.Box(trail.BoxProps{Key: props.Message.ID, Style: rowStyle},
		trail.Span(trail.SpanProps{
			Style: style.S().WhiteSpace(style.WhiteSpaceNoWrap).Flex(0, 0),
		}, trail.Text("[system] ")),
		trail.Box(trail.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Flex(1, 1).MinWidth(style.Cells(0)),
		}, parts...),
	)
})

var messageStyle = style.S().
	Display(style.DisplayFlex).
	FlexDirection(style.FlexColumn).
	Width(style.Percent(100)).
	MaxWidth(style.Percent(100)).
	MinWidth(style.Cells(0)).
	MarginBottom(1)

func messageBody(message store.Message) trail.Node {
	parts := messageParts(message)
	if len(parts) == 0 {
		return nil
	}
	return trail.Box(trail.BoxProps{Key: message.ID, Style: messageStyle}, parts...)
}

func messageParts(message store.Message) []trail.Node {
	scheme := theme.UseTheme()
	parts := make([]trail.Node, 0, len(message.Parts))
	for _, part := range message.Parts {
		var node trail.Node
		switch part.Type {
		case store.PartText:
			if strings.TrimSpace(part.Text.Text) == "" {
				continue
			}
			if message.Role == api.RoleUser {
				text := part.Text.Text
				if strings.Contains(text, "<embedded_skill") {
					skillName := extractEmbeddedSkillName(text)
					cmdText := extractEmbeddedSkillCommand(text)
					node = trail.Box(trail.BoxProps{
						Style: style.S().FlexDirection(style.FlexRow).AlignItems(style.AlignCenter),
					},
						trail.Span(trail.SpanProps{
							Style: style.S().
								WhiteSpace(style.WhiteSpacePreWrap).
								Bold(true).
								Foreground(scheme.Color.Icon.Primary),
						}, trail.Text(cmdText+" ")),
						trail.Span(trail.SpanProps{
							Style: style.S().Bold(true).Foreground(scheme.Color.Text.Secondary),
						}, trail.Text(fmt.Sprintf("[Skill: %s]", skillName))),
					)
				} else {
					node = trail.Span(trail.SpanProps{
						Style: style.S().
							WhiteSpace(style.WhiteSpacePreWrap).
							OverflowWrap(style.OverflowWrapAnywhere).
							Bold(true).
							Foreground(scheme.Color.Icon.Primary),
					}, trail.Text(part.Text.Text))
				}
			} else if message.Role == api.RoleSystem {
				node = trail.Span(trail.SpanProps{
					Style: style.S().WhiteSpace(style.WhiteSpacePreWrap).OverflowWrap(style.OverflowWrapAnywhere),
				}, trail.Text(part.Text.Text))
			} else {
				node = MessageText(MessageTextProps{Text: part.Text.Text})
			}
		case store.PartThought:
			if strings.TrimSpace(part.Thought.Text) == "" {
				continue
			}
			node = MessageThought(MessageThoughtProps{Text: part.Thought.Text, Streaming: part.Thought.Streaming})
		case store.PartTool:
			node = MessageTool(MessageToolProps{Tool: *part.Tool})
		default:
			node = messageError(fmt.Sprintf("Unable to render message part: unsupported type %q", part.Type))
		}
		parts = append(parts, trail.Box(trail.BoxProps{
			Key: part.ID,
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Width(style.Percent(100)).
				MinWidth(style.Cells(0)),
		}, node))
	}
	return parts
}

var messageError = trail.FC("MessageError", func(text string) trail.Node {
	scheme := theme.UseTheme()
	textStyle := style.S().WhiteSpace(style.WhiteSpacePreWrap).OverflowWrap(style.OverflowWrapAnywhere).Foreground(scheme.Color.Text.Error)
	return trail.Span(trail.SpanProps{Style: textStyle}, trail.Text(text))
})

func extractEmbeddedSkillName(s string) string {
	idx := strings.Index(s, "<embedded_skill name=\"")
	if idx == -1 {
		return ""
	}
	start := idx + len("<embedded_skill name=\"")
	end := strings.Index(s[start:], "\"")
	if end == -1 {
		return ""
	}
	return s[start : start+end]
}

func extractEmbeddedSkillCommand(s string) string {
	if _, after, ok := strings.Cut(s, "The user invoked: "); ok {
		after := after
		line, _, _ := strings.Cut(after, "\n")
		return strings.TrimSpace(line)
	}
	if _, after, ok := strings.Cut(s, "The user explicitly invoked the skill \""); ok {
		after := after
		line, _, _ := strings.Cut(after, "\"")
		return strings.TrimSpace(line)
	}
	name := extractEmbeddedSkillName(s)
	if name != "" {
		return "/" + name
	}
	return ""
}
