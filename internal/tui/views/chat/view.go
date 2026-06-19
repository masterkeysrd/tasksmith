package chat

import (
	"context"
	"encoding/json"
	"fmt"
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
			kitex.PostMacro(func() {
				if inputRef.Current != nil {
					if doc := inputRef.Current.OwnerDocument(); doc != nil {
						doc.Focus(inputRef.Current)
					}
				}
			})
		}
	}, []any{isInsert})

	// 4. Polling query updates while the agent is running in the background
	kitex.UseInterval(func() {
		if sending {
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
		}
	}, 100*time.Millisecond, []any{sending, sessionID})

	kitex.UseInterval(func() {
		if sending {
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
		}
	}, 1000*time.Millisecond, []any{sending, sessionID})

	// Autoscroll history to bottom if already at bottom
	historyRef := kitex.UseRef[dom.Element](nil)
	lastMaxScrollY := kitex.UseRef(0)

	// 5. Reactive states for tracking agent thinking time and animations
	thinkingTime, setThinkingTime := kitex.UseState(0)
	lastFinishedTime, setLastFinishedTime := kitex.UseState(-1) // -1 represents null/unset
	spinnerFrame, setSpinnerFrame := kitex.UseState(0)

	// Keep lastFinishedTime up to date and reset thinkingTime when sending changes
	kitex.UseEffect(func() {
		if sending {
			setThinkingTime(0)
		} else {
			if thinkingTime() > 0 {
				setLastFinishedTime(thinkingTime())
			}
		}
	}, []any{sending})

	// Increment thinkingTime every second when running
	kitex.UseInterval(func() {
		if sending {
			setThinkingTime(thinkingTime() + 1)
		}
	}, 1*time.Second, []any{sending})

	// Rotate spinner frame when running
	kitex.UseInterval(func() {
		if sending {
			setSpinnerFrame((spinnerFrame() + 1) % 4)
		}
	}, 250*time.Millisecond, []any{sending})

	pulseDots := []string{"●  ", "●● ", "●●●", "   "}
	currentDots := pulseDots[spinnerFrame()]

	// Calculate a simple integer key of the messages state to trigger the effect reactively
	messagesKey := 0
	for _, msg := range messages {
		for _, block := range msg.GetContent() {
			if tb, ok := block.(*message.TextBlock); ok {
				messagesKey += len(tb.Text)
			} else if tb, ok := block.(*message.ThinkingBlock); ok {
				messagesKey += len(tb.Thinking)
			}
		}
	}
	messagesKey += len(messages)

	kitex.UseLayoutEffect(func() {
		if historyRef.Current == nil {
			return
		}

		el := historyRef.Current
		doc := el.OwnerDocument()
		if doc == nil {
			return
		}
		view := doc.DefaultView()
		if view == nil {
			return
		}

		_, maxScrollY := view.GetMaxScroll(el)
		_, currentY := el.Scroll()

		// If the user was previously scrolled to the bottom (within a 2-cell tolerance),
		// we scroll to the absolute bottom using a large value (99999).
		// Kite's paint engine will synchronously clamp this value to the fresh scroll extent at paint time in the current frame.
		if currentY >= lastMaxScrollY.Current-2 {
			el.ScrollTo(0, 99999)
		}

		lastMaxScrollY.Current = maxScrollY
	}, nil)

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

	bgDark := color.Color(color.RGBA{R: 22, G: 22, B: 30, A: 255})
	if t != nil {
		if c, ok := t.Palette["bg_dark"]; ok {
			bgDark = c
		} else {
			bgDark = t.Color.Surface.BaseHover
		}
	}

	messagesContainerStyle := style.S().
		Flex(1, 1, style.Cells(0)).
		MinHeight(style.Cells(0)).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Gap(1).
		Padding(1).
		Overflow(style.OverflowAuto).
		Background(bgDark)

	composerContainerStyle := style.S().
		PaddingHorizontal(1).
		PaddingVertical(1).
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		Background(bgDark)

	return kitex.Box(kitex.BoxProps{Style: outerStyle},
		// Session Title Bar
		kitex.Box(kitex.BoxProps{Style: sessionTitleBarStyle},
			kitex.Text(title),
		),

		// Message History Section
		kitex.Box(kitex.BoxProps{Style: messagesContainerStyle, Ref: historyRef},
			kitex.Fragment(
				func() []kitex.Node {
					nodes := renderBubbles(messages)
					if statusNode := renderAgentStatus(t, sending, thinkingTime(), lastFinishedTime(), currentDots); statusNode != nil {
						nodes = append(nodes, statusNode)
					}
					return nodes
				}()...,
			),
		),

		// Composer Section
		kitex.Box(kitex.BoxProps{Style: composerContainerStyle},
			Composer(ComposerProps{
				Value:    inputValue(),
				Disabled: sending,
				IsInsert: isInsert,
				Ref:      inputRef,
				OnChange: func(val string) {
					setInputValue(val)
				},
				OnSubmit: func() {
					sendMessage(inputValue())
				},
			}),
		),
	)
})

// ComposerProps defines the properties for the Composer component.
type ComposerProps struct {
	Value     string
	Disabled  bool
	IsInsert  bool
	Ref       kitex.Ref[dom.Element]
	OnChange  func(string)
	OnKeyDown func(event.Event)
	OnSubmit  func()
}

// Composer is a multiline composer component styled like a terminal UI box,
// matching the design mockup in mockup.tsx.
var Composer = kitex.FC("Composer", func(props ComposerProps) kitex.Node {
	isFocused, setIsFocused := kitex.UseState(false)
	t := theme.UseTheme()

	if t == nil {
		return kitex.Box(kitex.BoxProps{}, kitex.Text("No Theme"))
	}

	// Resolve the focus blue color from the palette/theme
	blueColor := t.Color.Surface.Info
	if c, ok := t.Palette["blue"]; ok {
		blueColor = c
	}

	// Border color switches to blue when focused, otherwise comment color
	borderColor := t.Color.Text.Tertiary // comment: #565f89
	if isFocused() {
		borderColor = blueColor // blue: #7aa2f7
	}

	// Wrapper style with a full single border
	wrapperStyle := style.S().
		Width(style.Percent(100)).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignEnd).
		Padding(0, 1). // 1 cell padding inside
		Border(style.SingleBorder().Color(borderColor))

	// Text area style
	var textCol color.Color
	if props.Disabled {
		textCol = t.Color.Text.Tertiary
	} else {
		textCol = t.Color.Text.Secondary // fg_dark: #a9b1d6
	}

	textareaStyle := style.S().
		Flex(1, 1, style.Cells(0)).
		MinHeight(style.Cells(1)).
		MaxHeight(style.Cells(8)).
		Background(color.Transparent).
		Foreground(textCol).
		Border(false)

	// Placeholder style
	ps := style.S().Foreground(t.Color.Text.Tertiary)

	textareaDisabled := props.Disabled || !props.IsInsert

	textareaProps := kitex.TextAreaProps{
		Name:             "composer-textarea",
		Value:            props.Value,
		Placeholder:      "Message TaskSmith...",
		PlaceholderStyle: ps,
		Disabled:         textareaDisabled,
		Style:            textareaStyle,
		Ref:              props.Ref,
		OnChange: func(e event.Event) {
			if props.OnChange != nil {
				if ie, ok := e.(*event.ChangeEvent); ok {
					props.OnChange(ie.Value)
				} else if ie, ok := e.(*event.InputEvent); ok {
					props.OnChange(ie.Value)
				}
			}
		},
		OnFocus: func(e event.Event) {
			setIsFocused(true)
		},
		OnBlur: func(e event.Event) {
			setIsFocused(false)
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

			// Enter without modifiers submits
			if ke.Code == key.KeyEnter && (ke.Mod&key.ModShift) == 0 {
				e.PreventDefault()
				e.StopPropagation()
				if props.OnSubmit != nil {
					props.OnSubmit()
				}
				return
			}

			if props.OnKeyDown != nil {
				props.OnKeyDown(e)
			}
		},
	}

	// Send button style
	btnStyle := style.S().
		Padding(0, 1).
		Background(color.Transparent).
		Height(style.Cells(1))

	isSendDisabled := props.Disabled || strings.TrimSpace(props.Value) == ""

	if isSendDisabled {
		btnStyle = btnStyle.Foreground(t.Color.Text.Tertiary)
	} else {
		btnStyle = btnStyle.Foreground(t.Color.Text.Tertiary)
	}

	btnHoverStyle := style.S()
	if !isSendDisabled {
		btnHoverStyle = btnHoverStyle.
			Background(t.Color.Surface.BaseFocus).
			Foreground(blueColor)
	}

	wrapperProps := kitex.BoxProps{Style: wrapperStyle}
	if !props.Disabled && !props.IsInsert {
		wrapperProps.OnClick = func(e event.Event) {
			mode.Set(mode.Insert)
		}
	}

	return kitex.Box(wrapperProps,
		kitex.TextArea(textareaProps),
		components.Button(components.ButtonProps{
			Variant:    components.ButtonText,
			Disabled:   isSendDisabled,
			OnClick:    props.OnSubmit,
			Style:      btnStyle,
			HoverStyle: btnHoverStyle,
		}, icon.MoveUp),
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
			MaxWidth(style.Percent(90))

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
		var children []kitex.Node
		for _, block := range content {
			var node kitex.Node
			switch b := block.(type) {
			case *message.TextBlock:
				if strings.TrimSpace(b.Text) != "" {
					node = components.Markdown(components.MarkdownProps{Source: b.Text})
				}
			case *message.ThinkingBlock:
				if strings.TrimSpace(b.Thinking) != "" {
					node = CollapsibleThinking(CollapsibleThinkingProps{Content: b.Thinking})
				}
			}
			if node != nil {
				children = append(children, node)
			}
		}

		if len(children) == 0 {
			return nil
		}

		return kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
		}, children...)
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
	return components.Markdown(components.MarkdownProps{Source: strings.Join(texts, "\n")})
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

func renderAgentStatus(t *theme.Scheme, sending bool, thinkingTime int, lastFinishedTime int, currentDots string) kitex.Node {
	if t == nil {
		return nil
	}
	blueColor := t.Color.Surface.Info
	if c, ok := t.Palette["blue"]; ok {
		blueColor = c
	}
	greenColor := t.Color.Surface.Success
	if c, ok := t.Palette["green"]; ok {
		greenColor = c
	}
	timeStr := fmt.Sprintf("[%02d:%02d]", thinkingTime/60, thinkingTime%60)
	if sending {
		containerStyle := style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Gap(1).
			PaddingLeft(2).
			Foreground(blueColor)
		dotsStyle := style.S().Foreground(blueColor).Width(style.Cells(3))
		labelStyle := style.S().Foreground(blueColor).Bold(true)
		timeStyle := style.S().Foreground(t.Color.Text.Tertiary)
		return kitex.Box(kitex.BoxProps{Style: containerStyle},
			kitex.Box(kitex.BoxProps{Style: dotsStyle}, kitex.Text(currentDots)),
			kitex.Box(kitex.BoxProps{Style: labelStyle}, kitex.Text("Thinking")),
			kitex.Box(kitex.BoxProps{Style: timeStyle}, kitex.Text(timeStr)),
		)
	}
	if lastFinishedTime >= 0 {
		finishedTimeStr := fmt.Sprintf("[%02d:%02d]", lastFinishedTime/60, lastFinishedTime%60)
		containerStyle := style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Gap(1).
			PaddingLeft(2).
			Foreground(t.Color.Text.Secondary)
		checkStyle := style.S().Foreground(greenColor)
		labelStyle := style.S().Foreground(t.Color.Text.Secondary)
		timeStyle := style.S().Foreground(t.Color.Text.Secondary)
		return kitex.Box(kitex.BoxProps{Style: containerStyle},
			kitex.Box(kitex.BoxProps{Style: checkStyle}, icon.Checkmark),
			kitex.Box(kitex.BoxProps{Style: labelStyle}, kitex.Text("Agent completed in")),
			kitex.Box(kitex.BoxProps{Style: timeStyle}, kitex.Text(finishedTimeStr)),
		)
	}
	return nil
}
