package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// FormatUserPrompt formats a submitted user prompt line for scrollback output.
func FormatUserPrompt(text string) string {
	return theme.UserPromptStyle.Render("❯ " + text)
}

// FormatMessage renders a persisted or streamed [api.Message] including user text,
// assistant reasoning/text, tool calls, and tool results.
func FormatMessage(msg api.Message) string {
	switch msg.Role {
	case api.RoleUser:
		text := ExtractTextBlocks(msg.Content)
		return theme.UserMsgStyle.Render(fmt.Sprintf("[user] %s", text))

	case api.RoleAssistant:
		return FormatAssistantMessage(msg.Content)

	case api.RoleTool:
		return FormatToolMessage(msg)

	default:
		text := ExtractTextBlocks(msg.Content)
		return theme.StatusBarStyle.Render(fmt.Sprintf("[%s] %s", msg.Role, text))
	}
}

// FormatAssistantMessage formats the reasoning, text, and tool calls of an assistant message.
func FormatAssistantMessage(blocks []api.Block) string {
	var parts []string
	var reasoning strings.Builder
	var text strings.Builder

	for _, b := range blocks {
		switch b.Type {
		case api.BlockReasoning:
			reasoning.WriteString(b.Text)
		case api.BlockText:
			text.WriteString(b.Text)
		case api.BlockToolCall:
			if b.ToolCall != nil {
				parts = append(parts, FormatToolCall(*b.ToolCall))
			}
		}
	}

	var out []string
	if r := strings.TrimSpace(reasoning.String()); r != "" {
		out = append(out, theme.ReasoningStyle.Render(fmt.Sprintf("[reasoning] %s", r)))
	}
	if t := text.String(); t != "" {
		out = append(out, theme.AssistantMsgStyle.Render(fmt.Sprintf("[assistant] %s", t)))
	}
	out = append(out, parts...)

	if len(out) == 0 {
		return theme.AssistantMsgStyle.Render("[assistant] ")
	}
	return strings.Join(out, "\n")
}

// FormatToolCall renders a formatted tool call indicator.
func FormatToolCall(tc api.ToolCall) string {
	if len(tc.Args) == 0 {
		return theme.ToolCallStyle.Render(fmt.Sprintf("[tool_call] %s (%s)", tc.Name, tc.ID))
	}
	raw, err := json.Marshal(tc.Args)
	if err != nil {
		return theme.ToolCallStyle.Render(fmt.Sprintf("[tool_call] %s (%s)", tc.Name, tc.ID))
	}
	return theme.ToolCallStyle.Render(fmt.Sprintf("[tool_call] %s (%s) %s", tc.Name, tc.ID, string(raw)))
}

// FormatToolMessage renders a formatted tool result block.
func FormatToolMessage(msg api.Message) string {
	text := ExtractTextBlocks(msg.Content)
	name := msg.Name
	if name == "" {
		name = msg.ToolCallID
	}
	prefix := "[tool]"
	if msg.IsError {
		prefix = "[tool:error]"
	}
	if name != "" {
		line := fmt.Sprintf("%s %s: %s", prefix, name, text)
		if msg.IsError {
			return theme.ErrorStyle.Render(line)
		}
		return theme.ToolResultStyle.Render(line)
	}
	line := fmt.Sprintf("%s %s", prefix, text)
	if msg.IsError {
		return theme.ErrorStyle.Render(line)
	}
	return theme.ToolResultStyle.Render(line)
}

// ExtractTextBlocks concatenates all text blocks in a message.
func ExtractTextBlocks(blocks []api.Block) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == api.BlockText {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// FormatError styles an error message for scrollback display.
func FormatError(msg string) string {
	return theme.ErrorStyle.Render("error: " + msg)
}
