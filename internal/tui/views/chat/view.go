package chat

import (
	"context"
	"encoding/json"
	"image/color"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// ViewProps defines the properties for the Chat view.
type ViewProps struct {
	SessionID string
}

// View is the main Chat view component.
var View = kitex.FC("ChatView", func(props ViewProps) kitex.Node {
	t := theme.UseTheme()
	client := tuiapi.UseClient()
	windClient := wind.UseClient()

	sessionID := props.SessionID

	// 1. Fetch messages reactively from database/active runner
	msgsQuery := queries.UseGetSessionMessages(sessionID)

	// 2. Fetch session execution status reactively
	stateQuery := queries.UseGetSessionState(sessionID)

	// 2b. Fetch sessions reactively to resolve session title
	sessionsQuery := queries.UseListSessions()

	title := sessionID
	if sessionsQuery.Data != nil {
		for _, s := range sessionsQuery.Data.Sessions {
			if s.ID == sessionID {
				title = s.Title
				break
			}
		}
	}

	var messages message.MessageList
	if msgsQuery.Data != nil && len(msgsQuery.Data.Messages) > 0 {
		rawArray := "[" + strings.Join(msgsQuery.Data.Messages, ",") + "]"
		_ = json.Unmarshal([]byte(rawArray), &messages)
	}

	status := "idle"
	if stateQuery.Data != nil {
		status = stateQuery.Data.Status
	}
	sending := status == "running"

	// 3. Reactive state for input composer
	inputValue, setInputValue := kitex.UseState("")

	// Mode handling & Focus management
	m := mode.Use()
	isInsert := m == mode.Insert
	inputRef := kitex.CreateRef[dom.Element]()

	// Focus the input when entering insert mode
	kitex.UseEffect(func() {
		if isInsert {
			if inputRef.Current != nil {
				if doc := inputRef.Current.OwnerDocument(); doc != nil {
					doc.Focus(inputRef.Current)
				}
			} else {
				kitex.PostMacro(func() {
					if inputRef.Current != nil {
						if doc := inputRef.Current.OwnerDocument(); doc != nil {
							doc.Focus(inputRef.Current)
						}
					}
				})
			}
		}
	}, []any{isInsert})

	// 4. Polling query updates while the agent is running in the background
	polling, setPolling := kitex.UseState(false)

	kitex.UseEffect(func() {
		if sending {
			setPolling(true)
			go func() {
				ticker := time.NewTicker(100 * time.Millisecond)
				defer ticker.Stop()
				for range ticker.C {
					if !polling() {
						return
					}
					windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
					windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
				}
			}()
		} else {
			setPolling(false)
		}
	}, []any{sending, sessionID})

	sendMessage := func(text string) {
		if text == "" || sending {
			return
		}

		// Exit insert mode upon sending
		mode.Set(mode.Normal)
		setInputValue("")

		// Trigger SendMessage on the backend asynchronously
		promise.New(func(ctx context.Context) (bool, error) {
			_, err := client.SendMessage(ctx, api.SendMessageRequest{
				SessionID: sessionID,
				Text:      text,
			})
			if err != nil {
				return false, err
			}
			return true, nil
		}).Then(func(success bool) {
			// Immediately invalidate queries to trigger a reload of messages and state
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
		}, func(err error) {
			// Error handling (e.g. log or show message)
		})
	}

	// 6. Layout Style definitions
	sessionTitleBarStyle := style.S().
		Width(style.Percent(100)).
		Height(style.Cells(1)).
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		JustifyContent(style.JustifyCenter).
		Bold(true)

	if t != nil {
		sessionTitleBarStyle = sessionTitleBarStyle.
			Background(t.Color.Surface.BaseFocus).
			Foreground(t.Color.Text.Primary)
	}

	outerStyle := style.S().
		Width(style.Percent(100)).
		Height(style.Percent(100)).
		Flex(1, 1, style.Cells(0)).
		MinHeight(style.Cells(0)).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn)

	messagesContainerStyle := style.S().
		Flex(1, 1, style.Cells(0)).
		MinHeight(style.Cells(0)).
		Padding(1).
		Overflow(style.OverflowAuto)

	composerContainerStyle := style.S().
		PaddingHorizontal(1).
		PaddingVertical(0).
		Height(style.Cells(3)).
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter)

	if t != nil {
		composerContainerStyle = composerContainerStyle.Border(style.SingleBorder().Top(true).Color(t.Color.Border.Primary))
	}

	return kitex.Box(kitex.BoxProps{Style: outerStyle},
		// Session Title Bar
		kitex.Box(kitex.BoxProps{Style: sessionTitleBarStyle},
			kitex.Text(title),
		),

		// Message History Section
		kitex.Box(kitex.BoxProps{Style: messagesContainerStyle},
			kitex.Fragment(
				renderBubbles(messages)...,
			),
		),

		// Composer Section
		kitex.Box(kitex.BoxProps{Style: composerContainerStyle},
			components.Input(components.InputProps{
				Name:        "composer",
				Placeholder: "Ask TaskSmith anything... (Press Enter to send)",
				Value:       inputValue(),
				Disabled:    sending,
				Ref:         inputRef,
				Variant:     components.InputSolid,
				OnChange: func(val string) {
					setInputValue(val)
				},
				OnKeyDown: func(e event.Event) {
					ke, ok := e.(*event.KeyEvent)
					if !ok {
						return
					}

					if ke.Code == key.KeyEscape {
						e.PreventDefault()
						e.StopPropagation()
						mode.Set(mode.Normal)
						return
					}

					if ke.Code == key.KeyEnter {
						e.PreventDefault()
						e.StopPropagation()
						sendMessage(inputValue())
					}
				},
				Style: style.S().Border(false).Flex(1),
			}),
		),
	)
})

type CollapsibleThinkingProps struct {
	Content string
}

var CollapsibleThinking = kitex.FC("CollapsibleThinking", func(props CollapsibleThinkingProps) kitex.Node {
	t := theme.UseTheme()
	isOpen, setIsOpen := kitex.UseState(false)

	toggleText := "[+] Thinking..."
	if isOpen() {
		toggleText = "[-] Thinking..."
	}

	headerStyle := style.S().
		Foreground(t.Color.Text.Secondary).
		Bold(true)

	contentStyle := style.S().
		Foreground(t.Color.Text.Tertiary).
		PaddingLeft(2).
		MarginBottom(1)

	return kitex.Box(kitex.BoxProps{
		Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
	},
		components.Button(components.ButtonProps{
			Variant: components.ButtonText,
			Style:   headerStyle,
			OnClick: func() {
				setIsOpen(!isOpen())
			},
		}, kitex.Text(toggleText)),
		kitex.If(isOpen(), func() kitex.Node {
			return kitex.Box(kitex.BoxProps{Style: contentStyle},
				kitex.Text(props.Content),
			)
		}),
	)
})

type BubbleProps struct {
	Role      message.Role
	Timestamp string
	Children  []kitex.Node
}

var Bubble = kitex.FC("Bubble", func(props BubbleProps) kitex.Node {
	t := theme.UseTheme()
	role := props.Role
	timestamp := props.Timestamp
	children := props.Children

	var align style.Align
	var bubbleStyle style.Style

	if t != nil {
		bubbleStyle = style.S().
			Padding(1).
			MarginBottom(1).
			MaxWidth(style.Percent(70))

		switch role {
		case message.RoleUser:
			align = style.AlignEnd
			bubbleStyle = bubbleStyle.
				Background(t.Color.Surface.Primary).
				Foreground(t.Color.Text.Primary)
		case message.RoleSystem:
			align = style.AlignCenter
			bubbleStyle = bubbleStyle.
				Background(t.Color.Surface.BaseDisabled).
				Foreground(t.Color.Text.Tertiary)
		default:
			align = style.AlignStart
			bubbleStyle = bubbleStyle.
				Background(t.Color.Surface.Secondary).
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
		senderIcon = icon.Cpu
		if role == message.RoleAssistant {
			senderName = " AGENT"
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
		Bold(true).
		MarginBottom(1)

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

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Width(style.Percent(100)).
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			AlignItems(align),
	},
		headerNode,
		kitex.Box(kitex.BoxProps{Style: bubbleStyle},
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
			}, children...),
		),
	)
})

type MessageProps struct {
	Role    message.Role
	Content message.Content
}

var Message = kitex.FC("Message", func(props MessageProps) kitex.Node {
	role := props.Role
	content := props.Content

	if role == message.RoleAssistant {
		return kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
		},
			kitex.Map(content, func(block message.Block, _ int) kitex.Node {
				switch b := block.(type) {
				case *message.TextBlock:
					if strings.TrimSpace(b.Text) != "" {
						return kitex.Text(b.Text)
					}
				case *message.ThinkingBlock:
					if strings.TrimSpace(b.Thinking) != "" {
						return CollapsibleThinking(CollapsibleThinkingProps{Content: b.Thinking})
					}
				}
				return nil
			}),
		)
	}

	// Combine all text blocks for other roles
	var texts []string
	for _, block := range content {
		if tb, ok := block.(*message.TextBlock); ok {
			texts = append(texts, tb.Text)
		}
	}
	if len(texts) == 0 {
		return nil
	}
	return kitex.Text(strings.Join(texts, "\n"))
})

func getBubbleRole(role message.Role) message.Role {
	switch role {
	case message.RoleUser:
		return message.RoleUser
	case message.RoleSystem:
		return message.RoleSystem
	default:
		// Group assistant, tool, and any other roles into Assistant bubble
		return message.RoleAssistant
	}
}

func renderBubbles(messages message.MessageList) []kitex.Node {
	if len(messages) == 0 {
		return nil
	}

	var nodes []kitex.Node
	var currentGroup []message.Message
	var currentGroupRole message.Role

	flush := func() {
		if len(currentGroup) > 0 {
			if node := createBubbleNode(currentGroupRole, currentGroup); node != nil {
				nodes = append(nodes, node)
			}
		}
	}

	for _, msg := range messages {
		role := msg.Role()
		groupRole := getBubbleRole(role)

		if len(currentGroup) == 0 {
			currentGroup = append(currentGroup, msg)
			currentGroupRole = groupRole
		} else if groupRole == currentGroupRole {
			currentGroup = append(currentGroup, msg)
		} else {
			flush()
			currentGroup = []message.Message{msg}
			currentGroupRole = groupRole
		}
	}

	flush()

	return nodes
}

func createBubbleNode(role message.Role, msgs []message.Message) kitex.Node {
	timestamp := ""
	if len(msgs) > 0 {
		meta := msgs[0].GetMetadata()
		if meta != nil {
			if val, ok := meta["created_at"].(string); ok {
				timestamp = val
			}
		}
	}
	if timestamp == "" {
		timestamp = time.Now().Format("15:04")
	}

	var children []kitex.Node
	for _, msg := range msgs {
		node := Message(MessageProps{
			Role:    msg.Role(),
			Content: msg.GetContent(),
		})
		if node != nil {
			children = append(children, node)
		}
	}

	if len(children) == 0 {
		return nil
	}

	return Bubble(BubbleProps{
		Role:      role,
		Timestamp: timestamp,
		Children:  children,
	})
}
