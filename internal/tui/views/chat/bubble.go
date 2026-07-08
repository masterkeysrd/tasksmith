package chat

import (
	"encoding/json"
	"fmt"
	"image/color"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/core/preview"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/tokenutils"
)

type BubbleGroupProps struct {
	Key                   string
	Role                  message.Role
	Msgs                  []message.Message
	ToolResponses         map[string]*message.Tool
	MainAgentName         string
	IsGenerating          bool
	LiveThinkingTime      int
	PendingAuthorizations []permissions.AuthorizationRequest
	SessionID             string
	OnViewFullOutput      func(title, cachedPath string)
	OnViewPreview         func(title string, p preview.ToolPreview)
}

var BubbleGroup = kitex.FC("BubbleGroup", func(props BubbleGroupProps) kitex.Node {
	msgKey := computeMsgsKey(props.Msgs)
	var authKey string
	if len(props.PendingAuthorizations) > 0 {
		var sb strings.Builder
		for _, auth := range props.PendingAuthorizations {
			sb.WriteString(auth.ToolCallID)
			sb.WriteString(auth.ToolName)
		}
		authKey = sb.String()
	}

	return kitex.UseMemo(func() kitex.Node {
		timestamp := ""
		var msgAgentName string
		if len(props.Msgs) > 0 {
			meta := props.Msgs[0].GetMetadata()
			if meta != nil {
				if val, ok := meta["created_at"].(string); ok {
					timestamp = val
				}
				if val, ok := meta["agent_name"].(string); ok {
					msgAgentName = val
				}
			}
		}
		if timestamp == "" {
			timestamp = time.Now().Format("15:04")
		}

		var tokensInput, tokensOutput, tokensTotal int
		for _, msg := range props.Msgs {
			if asstMsg, ok := msg.(*message.Assistant); ok {
				if asstMsg.Metrics != nil {
					tokensInput += asstMsg.Metrics.Tokens.Input
					tokensOutput += asstMsg.Metrics.Tokens.Output
					tokensTotal += asstMsg.Metrics.TotalTokens
				} else if meta := asstMsg.GetMetadata(); meta != nil {
					if promptToks, ok := meta["prompt_tokens"].(int); ok {
						tokensInput += promptToks
					} else if promptToksFloat, ok := meta["prompt_tokens"].(float64); ok {
						tokensInput += int(promptToksFloat)
					}
					if compToks, ok := meta["completion_tokens"].(int); ok {
						tokensOutput += compToks
					} else if compToksFloat, ok := meta["completion_tokens"].(float64); ok {
						tokensOutput += int(compToksFloat)
					}
					if totalToks, ok := meta["total_tokens"].(int); ok {
						tokensTotal += totalToks
					} else if totalToksFloat, ok := meta["total_tokens"].(float64); ok {
						tokensTotal += int(totalToksFloat)
					}
				}
			}
		}

		lastNonToolIndex := -1
		for idx := len(props.Msgs) - 1; idx >= 0; idx-- {
			if props.Msgs[idx].Role() != message.RoleTool {
				lastNonToolIndex = idx
				break
			}
		}

		var children []kitex.Node
		for i, msg := range props.Msgs {
			if msg.Role() == message.RoleTool {
				continue
			}

			var reasoningTokens int
			var thinkingDuration time.Duration
			if asstMsg, ok := msg.(*message.Assistant); ok {
				if asstMsg.Metrics != nil {
					reasoningTokens = asstMsg.Metrics.Tokens.Reasoning
					if asstMsg.Metrics.Timing.Generation > 0 {
						thinkingDuration = asstMsg.Metrics.Timing.Generation
					}
				}
				if thinkingDuration == 0 {
					if meta := asstMsg.GetMetadata(); meta != nil {
						if durSec, ok := meta["thinking_duration"].(int); ok {
							thinkingDuration = time.Duration(durSec) * time.Second
						} else if durSecFloat, ok := meta["thinking_duration"].(float64); ok {
							thinkingDuration = time.Duration(durSecFloat) * time.Second
						}
					}
				}
				if props.IsGenerating && i == len(props.Msgs)-1 {
					thinkingDuration = time.Duration(props.LiveThinkingTime) * time.Second
				}
			}

			var msgPendingAuths []permissions.AuthorizationRequest
			if i == lastNonToolIndex {
				msgPendingAuths = props.PendingAuthorizations
			}

			node := Message(MessageProps{
				Role:                  msg.Role(),
				Content:               msg.GetContent(),
				ToolResponses:         props.ToolResponses,
				ReasoningTokens:       reasoningTokens,
				ThinkingDuration:      thinkingDuration,
				PendingAuthorizations: msgPendingAuths,
				SessionID:             props.SessionID,
				OnViewFullOutput:      props.OnViewFullOutput,
				OnViewPreview:         props.OnViewPreview,
			})
			if node != nil {
				children = append(children, node)
			}
		}

		if len(children) == 0 {
			return nil
		}

		isSys := len(props.Msgs) > 0 && isSystemNotification(props.Msgs[0])
		var taskID, taskName, taskStatus, taskError string
		var exitCode int
		if isSys {
			meta := props.Msgs[0].GetMetadata()
			taskID, _ = meta["task_id"].(string)
			taskName, _ = meta["task_name"].(string)
			taskStatus, _ = meta["task_status"].(string)
			if ecVal, ok := meta["exit_code"]; ok {
				switch ec := ecVal.(type) {
				case float64:
					exitCode = int(ec)
				case int:
					exitCode = ec
				case int64:
					exitCode = int(ec)
				}
			}

			for _, block := range props.Msgs[0].GetContent() {
				if tb, ok := block.(*message.TextBlock); ok {
					if idx := strings.Index(tb.Text, "\nError: "); idx != -1 {
						taskError = strings.TrimSpace(tb.Text[idx+len("\nError: "):])
					}
				}
			}
		}

		return Bubble(BubbleProps{
			Key:                  props.Key,
			Role:                 props.Role,
			Timestamp:            timestamp,
			Children:             children,
			IsSystemNotification: isSys,
			TaskID:               taskID,
			TaskName:             taskName,
			TaskStatus:           taskStatus,
			ExitCode:             exitCode,
			TaskError:            taskError,
			AgentName:            msgAgentName,
			MainAgentName:        props.MainAgentName,
			TokensInput:          tokensInput,
			TokensOutput:         tokensOutput,
			TokensTotal:          tokensTotal,
		})
	}, []any{
		props.Key,
		props.Role,
		msgKey,
		props.IsGenerating,
		props.LiveThinkingTime,
		authKey,
	})
})

type BubbleProps struct {
	Key                  string
	Role                 message.Role
	Timestamp            string
	Children             []kitex.Node
	IsSystemNotification bool
	TaskID               string
	TaskName             string
	TaskStatus           string
	ExitCode             int
	TaskError            string
	AgentName            string
	MainAgentName        string
	TokensInput          int
	TokensOutput         int
	TokensTotal          int
}

var Bubble = kitex.FC("Bubble", func(props BubbleProps) kitex.Node {
	t := theme.UseTheme()
	role := props.Role
	timestamp := props.Timestamp
	children := props.Children

	if props.IsSystemNotification && t != nil {
		align := style.AlignStart
		borderCol := t.Color.Surface.Tertiary
		if props.TaskStatus != "" {
			if props.ExitCode == 0 {
				borderCol = t.Color.Surface.Success
			} else {
				borderCol = t.Color.Surface.Error
			}
		}

		cardStyle := style.S().
			Padding(1).
			Width(style.Percent(100)).
			MaxWidth(style.Percent(90)).
			Background(t.Color.Surface.BaseDisabled).
			Border(true, style.SingleBorder(), borderCol).
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Gap(1).
			Overflow(style.OverflowHidden)

		titleStyle := style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Gap(1).
			Bold(true).
			Foreground(borderCol)

		var titleIcon = icon.Info
		titleText := "SYSTEM NOTIFICATION"
		if props.TaskStatus != "" {
			titleText = "BACKGROUND TASK COMPLETED"
			if props.ExitCode == 0 {
				titleIcon = icon.Checkmark
			} else {
				titleIcon = icon.Alert
			}
		}

		cardHeader := kitex.Box(kitex.BoxProps{Style: titleStyle},
			titleIcon,
			kitex.Text(" "+titleText),
			kitex.Text(" ─ "),
			kitex.Text(timestamp),
		)

		// A nice label line showing Name and ID
		var detailsNode kitex.Node
		if props.TaskID != "" {
			var statText string
			var statCol color.Color
			switch props.TaskStatus {
			case "running":
				statText = "● RUNNING"
				statCol = t.Color.Surface.Info
			case "finished", "completed":
				if props.ExitCode == 0 {
					statText = "✔ COMPLETED"
					statCol = t.Color.Surface.Success
				} else {
					statText = fmt.Sprintf("✘ FAILED (%d)", props.ExitCode)
					statCol = t.Color.Text.Error
				}
			case "killed":
				statText = "⏹ KILLED"
				statCol = t.Color.Text.Secondary
			default:
				statText = strings.ToUpper(props.TaskStatus)
				statCol = t.Color.Text.Primary
			}

			statusBadgeStyle := style.S().
				Foreground(statCol).
				Bold(true)

			idStyle := style.S().
				Foreground(t.Color.Text.Secondary).
				Italic(true)

			nameStyle := style.S().
				Bold(true).
				Foreground(t.Color.Text.Primary)

			metaRowStyle := style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Gap(1).
				MarginTop(1)

			detailsNode = kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Width(style.Percent(100)),
			},
				kitex.Box(kitex.BoxProps{Style: nameStyle}, kitex.Text(props.TaskName)),
				kitex.Box(kitex.BoxProps{Style: metaRowStyle},
					kitex.Span(kitex.SpanProps{Style: idStyle}, kitex.Text("ID: "+props.TaskID)),
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("─")),
					kitex.Span(kitex.SpanProps{Style: statusBadgeStyle}, kitex.Text(statText)),
				),
			)
		}

		var errorNode kitex.Node
		if props.TaskError != "" {
			errStyle := style.S().
				Foreground(t.Color.Text.Error).
				MarginTop(1)
			errorNode = kitex.Box(kitex.BoxProps{Style: errStyle}, kitex.Text("Error: "+props.TaskError))
		}

		contentStyle := style.S().
			Foreground(t.Color.Text.Primary).
			PaddingLeft(2)

		var contentNodes []kitex.Node
		if detailsNode != nil {
			contentNodes = append(contentNodes, detailsNode)
		}
		if errorNode != nil {
			contentNodes = append(contentNodes, errorNode)
		}
		if props.TaskID == "" {
			contentNodes = append(contentNodes, children...)
		}

		return kitex.Box(kitex.BoxProps{
			Key: props.Key,
			Style: style.S().
				Width(style.Percent(100)).
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				AlignItems(align),
		},
			kitex.Box(kitex.BoxProps{Style: cardStyle},
				cardHeader,
				kitex.Box(kitex.BoxProps{Style: contentStyle}, contentNodes...),
			),
		)
	}

	var align style.Align
	var bubbleStyle style.Style

	if t != nil {
		bubbleStyle = style.S().
			Padding(1).
			MaxWidth(style.Percent(90)).
			MinWidth(style.Percent(0)).
			Overflow(style.OverflowHidden)

		switch role {
		case message.RoleUser:
			align = style.AlignEnd
			bubbleStyle = bubbleStyle.
				Foreground(t.Color.Text.Primary)
		case message.RoleSystem:
			align = style.AlignCenter
			bubbleStyle = bubbleStyle.
				Background(t.Color.Surface.BaseDisabled).
				Foreground(t.Color.Text.Tertiary)
		default:
			align = style.AlignStart
			bubbleStyle = bubbleStyle.
				Width(style.Percent(100)).
				Foreground(t.Color.Text.Primary)
		}
	}

	var senderColor color.Color
	var senderIcon kitex.Node
	senderName := ""

	if role == message.RoleUser {
		senderColor = t.Color.Surface.Primary
		senderIcon = icon.User
		senderName = " USER"
	} else {
		senderColor = t.Color.Surface.Tertiary
		senderIcon = icon.CPU
		if role == message.RoleAssistant {
			senderName = " AGENT"
			if props.AgentName != "" && props.AgentName != props.MainAgentName {
				senderName = fmt.Sprintf(" AGENT (%s)", props.AgentName)
			}
		} else {
			senderName = strings.ToUpper(string(role))
		}
	}

	headerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Gap(1).
		Foreground(t.Color.Text.Tertiary).
		Bold(true)

	senderStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Gap(1).
		Foreground(senderColor)

	headerNode := kitex.Box(kitex.BoxProps{Style: headerStyle},
		kitex.Box(kitex.BoxProps{Style: senderStyle},
			senderIcon,
			kitex.Text(senderName),
		),
		kitex.Text("─ "),
		kitex.Text(timestamp),
	)

	// Add right-aligned dimmed token metrics if present
	if props.TokensInput > 0 || props.TokensOutput > 0 || props.TokensTotal > 0 {
		var tokenStr string
		if props.TokensInput > 0 || props.TokensOutput > 0 {
			tokenStr = fmt.Sprintf("↑%s ↓%s", tokenutils.FormatTokens(props.TokensInput), tokenutils.FormatTokens(props.TokensOutput))
		} else {
			tokenStr = fmt.Sprintf("%s TOTAL", tokenutils.FormatTokens(props.TokensTotal))
		}

		tokenStyle := style.S().Foreground(t.Color.Text.Tertiary).Italic(true)
		headerContainerStyle := style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			JustifyContent(style.JustifyBetween).
			Width(style.Percent(100))

		headerNode = kitex.Box(kitex.BoxProps{Style: headerContainerStyle},
			headerNode,
			kitex.Box(kitex.BoxProps{Style: tokenStyle}, kitex.Text(tokenStr)),
		)
	}

	return kitex.Box(kitex.BoxProps{
		Key: props.Key,
		Style: style.S().
			Width(style.Percent(100)).
			MinWidth(style.Percent(0)).
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			AlignItems(align),
	},
		headerNode,
		kitex.Box(kitex.BoxProps{Style: bubbleStyle},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Gap(1).
					Width(style.Percent(100)).
					MaxWidth(style.Percent(100)).
					MinWidth(style.Percent(0)).
					Overflow(style.OverflowHidden),
			}, children...),
		),
	)
})

func computeMsgsKey(msgs []message.Message) string {
	var sb strings.Builder
	for _, msg := range msgs {
		sb.WriteString(string(msg.Role()))
		for _, b := range msg.GetContent() {
			switch block := b.(type) {
			case *message.TextBlock:
				sb.WriteString(block.Text)
			case *message.ThinkingBlock:
				sb.WriteString(block.Thinking)
			case *message.ToolCall:
				sb.WriteString(block.ID)
				sb.WriteString(block.Name)
				if data, err := json.Marshal(block.Args); err == nil {
					sb.Write(data)
				}
			}
		}
	}
	return sb.String()
}

func isSystemNotification(msg message.Message) bool {
	meta := msg.GetMetadata()
	if meta == nil {
		return false
	}
	_, hasType := meta["type"]
	return hasType
}
