package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"os/exec"
	"path/filepath"
	"runtime"
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
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
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

	var queuedMessages message.MessageList
	if msgsQuery.Data != nil && len(msgsQuery.Data.QueuedMessages) > 0 {
		rawArray := "[" + strings.Join(msgsQuery.Data.QueuedMessages, ",") + "]"
		_ = json.Unmarshal([]byte(rawArray), &queuedMessages)
	}

	status := "idle"
	if stateQuery.Data != nil {
		status = stateQuery.Data.Status
	}
	sending := status == "running"

	// 3. Reactive state for input composer and submitting state
	inputValue, setInputValue := kitex.UseState("")
	submitting, setSubmitting := kitex.UseState(false)

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

	hasRunningTasks := false
	if stateQuery.Data != nil && len(stateQuery.Data.RunningTasks) > 0 {
		hasRunningTasks = true
	}

	kitex.UseInterval(func() {
		if sending || hasRunningTasks {
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
		}
	}, 1000*time.Millisecond, []any{sending, hasRunningTasks, sessionID})

	// Invalidate messages query when the agent finishes execution (sending transitions to false)
	kitex.UseEffect(func() {
		if !sending {
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
		}
	}, []any{sending, sessionID})

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

	oneDotPulseDots := []string{"●", " ", "●", " "}
	oneDotCurrentDots := oneDotPulseDots[spinnerFrame()]

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
	}, []any{messagesKey})

	sendMessage := func(text string) {
		if text == "" || submitting() {
			return
		}

		setInputValue("")
		setSubmitting(true)

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
			setSubmitting(false)
			// Immediately invalidate queries to trigger a reload of messages and state
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
		}, func(err error) {
			setSubmitting(false)
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
		bgDark = t.Color.Surface.BaseHover
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
					toolResponses := make(map[string]*message.Tool)
					for _, m := range messages {
						if m.Role() == message.RoleTool {
							if tMsg, ok := m.(*message.Tool); ok {
								toolResponses[tMsg.ToolCallID] = tMsg
							}
						}
					}
					nodes := renderBubbles(messages, toolResponses, currentDots, oneDotCurrentDots)
					if statusNode := renderAgentStatus(t, sending, thinkingTime(), lastFinishedTime(), currentDots); statusNode != nil {
						nodes = append(nodes, statusNode)
					}
					if queuedWidget := renderQueuedMessages(t, queuedMessages); queuedWidget != nil {
						nodes = append(nodes, queuedWidget)
					}
					if stateQuery.Data != nil && len(stateQuery.Data.RunningTasks) > 0 {
						nodes = append(nodes, RunningTasksWidget(RunningTasksWidgetProps{
							Tasks: stateQuery.Data.RunningTasks,
						}))
					}
					return nodes
				}()...,
			),
		),

		// Composer Section
		kitex.Box(kitex.BoxProps{Style: composerContainerStyle},
			Composer(ComposerProps{
				Value:    inputValue(),
				Disabled: submitting(),
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
			mode.Set(mode.Normal)
		},
		OnKeyDown: func(e event.Event) {
			ke, ok := e.(*event.KeyEvent)
			if !ok {
				return
			}

			if ke.Code == key.KeyEscape {
				e.PreventDefault()
				e.StopPropagation()
				if props.Ref != nil && props.Ref.Current != nil {
					props.Ref.Current.Blur()
				}
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

type BubbleProps struct {
	Role                 message.Role
	Timestamp            string
	Children             []kitex.Node
	IsSystemNotification bool
	TaskID               string
	TaskName             string
	TaskStatus           string
	ExitCode             int
	TaskError            string
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

		var titleIcon kitex.Node = icon.Info
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
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Gap(1).
					Width(style.Percent(100)).
					MaxWidth(style.Percent(100)).
					Overflow(style.OverflowHidden),
			}, children...),
		),
	)
})

type MessageProps struct {
	Role              message.Role
	Content           message.Content
	ToolResponses     map[string]*message.Tool
	CurrentDots       string
	OneDotCurrentDots string
}

func getToolOutput(content message.Content) string {
	var sb strings.Builder
	for _, block := range content {
		if tb, ok := block.(*message.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}

func getIntField(m map[string]any, key string) int {
	val, ok := m[key]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

func parseViewOutput(structured any) (startLine, endLine, totalLines int, truncated bool) {
	m, ok := structured.(map[string]any)
	if !ok {
		return
	}
	startLine = getIntField(m, "start_line")
	endLine = getIntField(m, "end_line")
	totalLines = getIntField(m, "total_lines")
	truncated, _ = m["truncated"].(bool)
	return
}

func detectLang(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		return "txt"
	}
	return strings.ToLower(ext[1:])
}

func parseRangeFromHeader(text string) (startLine, endLine int) {
	before, _, ok := strings.Cut(text, "\n")
	if !ok {
		return
	}
	firstLine := before
	openParen := strings.Index(firstLine, " (")
	if openParen == -1 {
		return
	}
	dash := strings.Index(firstLine[openParen:], "-")
	if dash == -1 {
		return
	}
	dash = openParen + dash
	ofWord := strings.Index(firstLine[dash:], " of ")
	if ofWord == -1 {
		return
	}
	ofWord = dash + ofWord

	startStr := strings.TrimSpace(firstLine[openParen+2 : dash])
	endStr := strings.TrimSpace(firstLine[dash+1 : ofWord])

	_, _ = fmt.Sscan(startStr, &startLine)
	_, _ = fmt.Sscan(endStr, &endLine)
	return
}

func parseViewStructuredOutput(structured any) (tools.ViewOutput, bool) {
	if structured == nil {
		return tools.ViewOutput{}, false
	}
	if val, ok := structured.(tools.ViewOutput); ok {
		return val, true
	}
	if val, ok := structured.(*tools.ViewOutput); ok && val != nil {
		return *val, true
	}
	data, err := json.Marshal(structured)
	if err != nil {
		return tools.ViewOutput{}, false
	}
	var out tools.ViewOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return tools.ViewOutput{}, false
	}
	return out, true
}

func parseWriteStructuredOutput(structured any) (tools.WriteOutput, bool) {
	if structured == nil {
		return tools.WriteOutput{}, false
	}
	if val, ok := structured.(tools.WriteOutput); ok {
		return val, true
	}
	if val, ok := structured.(*tools.WriteOutput); ok && val != nil {
		return *val, true
	}
	data, err := json.Marshal(structured)
	if err != nil {
		return tools.WriteOutput{}, false
	}
	var out tools.WriteOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return tools.WriteOutput{}, false
	}
	return out, true
}

func parseEditStructuredOutput(structured any) (tools.EditOutput, bool) {
	if structured == nil {
		return tools.EditOutput{}, false
	}
	if val, ok := structured.(tools.EditOutput); ok {
		return val, true
	}
	if val, ok := structured.(*tools.EditOutput); ok && val != nil {
		return *val, true
	}
	data, err := json.Marshal(structured)
	if err != nil {
		return tools.EditOutput{}, false
	}
	var out tools.EditOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return tools.EditOutput{}, false
	}
	return out, true
}

func parseMultiEditStructuredOutput(structured any) (tools.MultiEditOutput, bool) {
	if structured == nil {
		return tools.MultiEditOutput{}, false
	}
	if val, ok := structured.(tools.MultiEditOutput); ok {
		return val, true
	}
	if val, ok := structured.(*tools.MultiEditOutput); ok && val != nil {
		return *val, true
	}
	data, err := json.Marshal(structured)
	if err != nil {
		return tools.MultiEditOutput{}, false
	}
	var out tools.MultiEditOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return tools.MultiEditOutput{}, false
	}
	return out, true
}

func parseRemoveStructuredOutput(structured any) (tools.RemoveOutput, bool) {
	if structured == nil {
		return tools.RemoveOutput{}, false
	}
	if val, ok := structured.(tools.RemoveOutput); ok {
		return val, true
	}
	if val, ok := structured.(*tools.RemoveOutput); ok && val != nil {
		return *val, true
	}
	data, err := json.Marshal(structured)
	if err != nil {
		return tools.RemoveOutput{}, false
	}
	var out tools.RemoveOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return tools.RemoveOutput{}, false
	}
	return out, true
}

func stripLinePrefixes(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		before, after, ok := strings.Cut(line, " | ")
		if ok {
			isNum := true
			prefix := before
			if len(prefix) == 0 {
				isNum = false
			}
			for _, r := range prefix {
				if r < '0' || r > '9' {
					isNum = false
					break
				}
			}
			if isNum {
				lines[i] = after
			}
		}
	}
	return strings.Join(lines, "\n")
}

func openWithSystemViewer(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", path)
	default:
		return
	}
	_ = cmd.Start()
}

type ToolExecutionProps struct {
	ToolCall    *message.ToolCall
	ToolMessage *message.Tool
	CurrentDots string
}

const lsPreviewLines = 10

// parseLsOutput extracts FileEntry values and metadata from a tool's StructuredContent.
// It handles both same-process typed values and JSON-deserialized map[string]any forms.
func parseLsOutput(structured any) (files []tools.FileEntry, totalCount int, truncated bool) {
	if structured == nil {
		return
	}

	// Same-process: StructuredContent is already a typed LsOutput.
	if out, ok := structured.(tools.LsOutput); ok {
		for _, f := range out.Files {
			fe := tools.FileEntry{
				Name:          f.Name,
				Permissions:   f.Permissions,
				Links:         uint64(f.Links),
				Owner:         f.Owner,
				Group:         f.Group,
				Size:          int64(f.Size),
				IsDir:         f.IsDir,
				IsSymlink:     f.IsSymlink,
				NameTruncated: f.NameTruncated,
				LinkTarget:    f.LinkTarget,
			}
			if t, err := time.Parse(time.RFC3339, f.Modified); err == nil {
				fe.Modified = t
			}
			files = append(files, fe)
		}
		return files, out.TotalCount, out.Truncated
	}

	// Cross-process / deserialized: round-trip through JSON.
	data, err := json.Marshal(structured)
	if err != nil {
		return
	}
	var raw struct {
		Files      []json.RawMessage `json:"files"`
		TotalCount int               `json:"total_count"`
		Truncated  bool              `json:"truncated"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	totalCount = raw.TotalCount
	truncated = raw.Truncated
	for _, f := range raw.Files {
		var fe tools.FileEntry
		if err := json.Unmarshal(f, &fe); err == nil {
			files = append(files, fe)
		}
	}
	return
}

// parseGlobOutput extracts structured file lists, count, and truncation from a glob tool result.
func parseGlobOutput(structured any) (matches []string, totalCount int, truncated bool) {
	if structured == nil {
		return
	}

	// Same-process: StructuredContent is already a typed GlobOutput.
	if out, ok := structured.(tools.GlobOutput); ok {
		return out.Matches, out.TotalCount, out.Truncated
	}

	// Cross-process / deserialized: round-trip through JSON.
	data, err := json.Marshal(structured)
	if err != nil {
		return
	}
	var raw struct {
		Matches    []string `json:"matches"`
		TotalCount int      `json:"total_count"`
		Truncated  bool     `json:"truncated"`
	}
	if err := json.Unmarshal(data, &raw); err == nil {
		matches = raw.Matches
		totalCount = raw.TotalCount
		truncated = raw.Truncated
	}
	return
}

// parseGrepOutput extracts structured matches, count, and truncation from a grep tool result.
func parseGrepOutput(structured any) (matches []tools.GrepOutputMatchesItem, totalCount int, truncated bool) {
	if structured == nil {
		return
	}

	// Same-process: StructuredContent is already a typed GrepOutput.
	if out, ok := structured.(tools.GrepOutput); ok {
		return out.Matches, out.TotalCount, out.Truncated
	}

	// Cross-process / deserialized: round-trip through JSON.
	data, err := json.Marshal(structured)
	if err != nil {
		return
	}
	var raw struct {
		Matches    []tools.GrepOutputMatchesItem `json:"matches"`
		TotalCount int                           `json:"total_count"`
		Truncated  bool                          `json:"truncated"`
	}
	if err := json.Unmarshal(data, &raw); err == nil {
		matches = raw.Matches
		totalCount = raw.TotalCount
		truncated = raw.Truncated
	}
	return
}

// parseTasksOutput extracts structured TasksOutput fields from a tasks tool result.
func parseTasksOutput(structured any) (out tools.TasksOutput, ok bool) {
	if structured == nil {
		return
	}

	// Same-process: StructuredContent is already a typed TasksOutput.
	if val, ok := structured.(tools.TasksOutput); ok {
		return val, true
	}

	// Cross-process / deserialized: round-trip through JSON.
	data, err := json.Marshal(structured)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &out); err == nil {
		return out, true
	}
	return
}

// lsEntryRow renders a single FileEntry as a table row (kitex.TR).
// Each metadata field occupies its own TD so the table layout engine
// distributes column widths automatically — no manual Sprintf padding needed.
func lsEntryRow(t *theme.Scheme, fe tools.FileEntry) kitex.Node {
	var metaColor color.Color
	var nameColor color.Color

	if t != nil {
		metaColor = t.Color.Text.Tertiary
		switch {
		case fe.IsDir:
			nameColor = t.Color.Surface.Info
		case fe.IsSymlink:
			nameColor = t.Color.Surface.Tertiary
		default:
			nameColor = t.Color.Text.Primary
		}
	}

	displayName := fe.Name
	if fe.NameTruncated && len(fe.Name) > tools.MaxFilenameChars {
		displayName = fe.Name[:tools.MaxFilenameChars] + "…"
	}

	// metaCell shrinks to its content width and adds a right padding gap.
	metaCell := func(text string, s style.Style) kitex.Node {
		tdStyle := s.Width(style.MaxContent).PaddingRight(1)
		return kitex.TD(kitex.TDProps{Style: tdStyle},
			kitex.Span(kitex.SpanProps{Style: s}, kitex.Text(text)),
		)
	}

	metaStyle := style.S().Foreground(metaColor).Width(style.Percent(1)) // shrink to content

	nameStyle := style.S().Foreground(nameColor)
	if fe.IsDir {
		nameStyle = nameStyle.Bold(true)
	}

	nameText := displayName
	if fe.IsSymlink && fe.LinkTarget != "" {
		nameText += " → " + fe.LinkTarget
	}

	// Name cell takes all remaining width.
	nameTDStyle := nameStyle.Width(style.Percent(100))

	return kitex.TR(kitex.TRProps{},
		metaCell(fe.Permissions, metaStyle),
		metaCell(fmt.Sprintf("%d", fe.Links), metaStyle),
		metaCell(fe.Owner, metaStyle),
		metaCell(fe.Group, metaStyle),
		metaCell(tools.FormatSize(fe.Size), metaStyle),
		metaCell(fe.Modified.Format("Jan _2 15:04"), metaStyle),
		kitex.TD(kitex.TDProps{Style: nameTDStyle},
			kitex.Span(kitex.SpanProps{Style: nameStyle}, kitex.Text(nameText)),
		),
	)
}

var ToolExecution = kitex.FC("ToolExecution", func(props ToolExecutionProps) kitex.Node {
	if props.ToolCall != nil && props.ToolCall.Name == "view" {
		return ViewToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "ls" {
		return LsToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "glob" {
		return GlobToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "grep" {
		return GrepToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "write" {
		return WriteToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "edit" {
		return EditToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "multi_edit" {
		return MultiEditToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "remove" {
		return RemoveToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "bash" {
		return BashToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "tasks" {
		return TasksToolWidget(props)
	}

	t := theme.UseTheme()
	isOpen, setIsOpen := kitex.UseState(true)

	tc := props.ToolCall
	tm := props.ToolMessage

	var argsStr string
	if len(tc.Args) > 0 {
		if data, err := json.Marshal(tc.Args); err == nil {
			argsStr = string(data)
		}
	}

	containerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		MarginVertical(1).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		Overflow(style.OverflowHidden)

	headerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		JustifyContent(style.JustifyBetween).
		Padding(0, 1).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		Overflow(style.OverflowHidden)

	bodyStyle := style.S().
		Padding(1).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		Overflow(style.OverflowHidden)

	var iconNode kitex.Node
	var statusLabel string
	var headerBg color.Color
	var headerFg color.Color
	var borderCol color.Color

	if t != nil {
		if tm == nil {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			statusLabel = fmt.Sprintf("RUNNING TOOL: %s", tc.Name)
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Surface.Info
			borderCol = t.Color.Surface.Info
		} else if tm.IsError {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			statusLabel = fmt.Sprintf("TOOL ERROR: %s", tc.Name)
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Text.Error
			borderCol = t.Color.Text.Error
		} else {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Checkmark)
			statusLabel = fmt.Sprintf("TOOL SUCCESS: %s", tc.Name)
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Surface.Success
			borderCol = t.Color.Surface.Success
		}

		containerStyle = containerStyle.
			Border(true, style.SingleBorder(), borderCol).
			Background(t.Color.Surface.BaseHover)

		headerStyle = headerStyle.
			Background(headerBg).
			Foreground(headerFg)

		bodyStyle = bodyStyle.
			Background(t.Color.Surface.BaseHover)
	}

	return kitex.Box(kitex.BoxProps{Style: containerStyle},
		components.Button(components.ButtonProps{
			Variant: components.ButtonText,
			Color:   components.ButtonBase,
			Style:   headerStyle,
			OnClick: func() {
				setIsOpen(!isOpen())
			},
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Gap(1),
			},
				iconNode,
				kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(statusLabel)),
			),
			kitex.If(tm != nil, func() kitex.Node {
				var label string
				if isOpen() {
					label = "▲ COLLAPSE"
				} else {
					label = "▼ EXPAND"
				}
				var textCol color.Color
				if t != nil {
					textCol = t.Color.Text.Secondary
				}
				return kitex.Span(kitex.SpanProps{
					Style: style.S().Foreground(textCol),
				}, kitex.Text(label))
			}),
		),
		kitex.If(isOpen(), func() kitex.Node {
			return kitex.Box(kitex.BoxProps{Style: bodyStyle},
				kitex.If(argsStr != "", func() kitex.Node {
					var textCol color.Color
					var valCol color.Color
					if t != nil {
						textCol = t.Color.Text.Secondary
						valCol = t.Color.Text.Tertiary
					}
					return kitex.Box(kitex.BoxProps{
						Style: style.S().
							MarginBottom(1).
							Display(style.DisplayFlex).
							FlexDirection(style.FlexColumn).
							Gap(0),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(textCol).Bold(true)}, kitex.Text("Parameters:")),
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(valCol).WhiteSpace(style.WhiteSpacePreWrap)}, kitex.Text(argsStr)),
					)
				}),
				kitex.If(tm != nil, func() kitex.Node {
					outText := getToolOutput(tm.Content)
					return kitex.Fragment(
						kitex.If(strings.TrimSpace(outText) != "", func() kitex.Node {
							var borderCol color.Color
							var textCol color.Color
							if t != nil {
								borderCol = t.Color.Border.Primary
								textCol = t.Color.Text.Secondary
							}
							outputContainerStyle := style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexColumn).
								Border(true, style.SingleBorder(), borderCol).
								Background(t.Color.Surface.BaseHover).
								Padding(1).
								Width(style.Percent(100)).
								MaxWidth(style.Percent(100)).
								Overflow(style.OverflowHidden).
								Foreground(textCol).
								WhiteSpace(style.WhiteSpacePreWrap)

							cleanText := strings.ReplaceAll(outText, "\t", "    ")
							return kitex.Box(kitex.BoxProps{Style: outputContainerStyle},
								kitex.Text(cleanText),
							)
						}),
						kitex.If(strings.TrimSpace(outText) == "", func() kitex.Node {
							var textCol color.Color
							if t != nil {
								textCol = t.Color.Text.Tertiary
							}
							return kitex.Box(kitex.BoxProps{
								Style: style.S().Foreground(textCol).Italic(true),
							}, kitex.Text("(no output)"))
						}),
					)
				}),
			)
		}),
	)
})

var Message = kitex.FC("Message", func(props MessageProps) kitex.Node {
	role := props.Role
	content := props.Content
	toolResponses := props.ToolResponses
	currentDots := props.CurrentDots

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
			case *message.ToolCall:
				var toolMsg *message.Tool
				if toolResponses != nil {
					toolMsg = toolResponses[b.ID]
				}
				dots := currentDots
				if b.Name == "bash" {
					dots = props.OneDotCurrentDots
				}
				node = ToolExecution(ToolExecutionProps{
					ToolCall:    b,
					ToolMessage: toolMsg,
					CurrentDots: dots,
				})
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
			cleaned := tryExtractTextFromJSON(tb.Text)
			texts = append(texts, cleaned)
		}
	}
	if len(texts) == 0 {
		return nil
	}
	return components.Markdown(components.MarkdownProps{Source: strings.Join(texts, "\n")})
})

func tryExtractTextFromJSON(input string) string {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "{") && !strings.HasPrefix(input, "[") {
		return input
	}

	// Try to unmarshal as a full message struct
	var msgObj struct {
		Role    string `json:"role"`
		Content []struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(input), &msgObj); err == nil && len(msgObj.Content) > 0 {
		var parts []string
		for _, b := range msgObj.Content {
			if (b.Kind == "text" || b.Kind == "") && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}

	// Try to unmarshal as a content array
	var contentArr []struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(input), &contentArr); err == nil && len(contentArr) > 0 {
		var parts []string
		for _, b := range contentArr {
			if (b.Kind == "text" || b.Kind == "") && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}

	return input
}

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

func isSystemNotification(msg message.Message) bool {
	meta := msg.GetMetadata()
	if meta == nil {
		return false
	}
	val, ok := meta["is_system_notification"].(bool)
	return ok && val
}

func renderBubbles(messages message.MessageList, toolResponses map[string]*message.Tool, currentDots string, oneDotCurrentDots string) []kitex.Node {
	if len(messages) == 0 {
		return nil
	}

	var nodes []kitex.Node
	var currentGroup []message.Message
	var currentGroupRole message.Role

	flush := func() {
		if len(currentGroup) > 0 {
			if node := createBubbleNode(currentGroupRole, currentGroup, toolResponses, currentDots, oneDotCurrentDots); node != nil {
				nodes = append(nodes, node)
			}
		}
	}

	for _, msg := range messages {
		isSys := isSystemNotification(msg)
		role := msg.Role()
		groupRole := getBubbleRole(role)

		if isSys {
			flush()
			currentGroup = []message.Message{msg}
			currentGroupRole = "system_notification"
			flush()
			currentGroup = nil
			currentGroupRole = ""
			continue
		}

		if len(currentGroup) == 0 {
			currentGroup = append(currentGroup, msg)
			currentGroupRole = groupRole
		} else if groupRole == currentGroupRole && currentGroupRole != "system_notification" {
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

func createBubbleNode(role message.Role, msgs []message.Message, toolResponses map[string]*message.Tool, currentDots string, oneDotCurrentDots string) kitex.Node {
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
		if msg.Role() == message.RoleTool {
			continue // Do not render tool messages as separate children. They are rendered inline in the assistant message.
		}
		node := Message(MessageProps{
			Role:              msg.Role(),
			Content:           msg.GetContent(),
			ToolResponses:     toolResponses,
			CurrentDots:       currentDots,
			OneDotCurrentDots: oneDotCurrentDots,
		})
		if node != nil {
			children = append(children, node)
		}
	}

	if len(children) == 0 {
		return nil
	}

	isSys := len(msgs) > 0 && isSystemNotification(msgs[0])
	var taskID, taskName, taskStatus, taskError string
	var exitCode int
	if isSys {
		meta := msgs[0].GetMetadata()
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

		// Extract error from the text block of the message if any
		for _, block := range msgs[0].GetContent() {
			if tb, ok := block.(*message.TextBlock); ok {
				if idx := strings.Index(tb.Text, "\nError: "); idx != -1 {
					taskError = strings.TrimSpace(tb.Text[idx+len("\nError: "):])
				}
			}
		}
	}

	return Bubble(BubbleProps{
		Role:                 role,
		Timestamp:            timestamp,
		Children:             children,
		IsSystemNotification: isSys,
		TaskID:               taskID,
		TaskName:             taskName,
		TaskStatus:           taskStatus,
		ExitCode:             exitCode,
		TaskError:            taskError,
	})
}

func renderAgentStatus(t *theme.Scheme, sending bool, thinkingTime int, lastFinishedTime int, currentDots string) kitex.Node {
	if t == nil {
		return nil
	}
	blueColor := t.Color.Surface.Info
	greenColor := t.Color.Surface.Success
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

func renderQueuedMessages(t *theme.Scheme, queuedMessages message.MessageList) kitex.Node {
	if len(queuedMessages) == 0 || t == nil {
		return nil
	}

	blueColor := t.Color.Surface.Info
	containerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Gap(0).
		PaddingVertical(1).
		PaddingHorizontal(2).
		Border(style.DoubleBorder().Color(t.Color.Text.Tertiary)).
		Background(t.Color.Surface.BaseHover).
		MarginTop(1).
		MarginBottom(1)

	titleStyle := style.S().
		Foreground(blueColor).
		Bold(true).
		MarginBottom(1)

	var msgNodes []kitex.Node
	for _, msg := range queuedMessages {
		if meta := msg.GetMetadata(); meta != nil {
			if isSys, ok := meta["is_system_notification"]; ok {
				if b, ok := isSys.(bool); ok && b {
					continue // Skip system notification messages
				}
			}
		}

		text := ""
		for _, block := range msg.GetContent() {
			if tb, ok := block.(*message.TextBlock); ok {
				text += tb.Text
			}
		}
		if text == "" {
			continue
		}

		msgStyle := style.S().
			Foreground(t.Color.Text.Secondary).
			MarginLeft(2)

		msgNodes = append(msgNodes, kitex.Box(kitex.BoxProps{Style: msgStyle}, kitex.Text("󰑮  "+text)))
	}

	if len(msgNodes) == 0 {
		return nil
	}

	children := []kitex.Node{
		kitex.Box(kitex.BoxProps{Style: titleStyle}, kitex.Text("󰑮 Queued Feedback")),
	}
	children = append(children, msgNodes...)

	return kitex.Box(kitex.BoxProps{Style: containerStyle}, children...)
}

type RunningTasksWidgetProps struct {
	Tasks []api.RunningTaskInfo
}

var RunningTasksWidget = kitex.FC("RunningTasksWidget", func(props RunningTasksWidgetProps) kitex.Node {
	t := theme.UseTheme()
	if len(props.Tasks) == 0 {
		return nil
	}

	taskWord := "task"
	if len(props.Tasks) > 1 {
		taskWord = "tasks"
	}

	summaryText := fmt.Sprintf("%d %s running", len(props.Tasks), taskWord)

	var taskRows []kitex.Node
	for _, task := range props.Tasks {
		dispDetails := task.Details
		if dispDetails == "" {
			dispDetails = "-"
		}

		// Truncate task command if too long
		dispName := task.Name
		if len(dispName) > 40 {
			dispName = dispName[:37] + "..."
		}

		taskRows = append(taskRows, kitex.TR(kitex.TRProps{},
			kitex.TD(kitex.TDProps{Style: style.S().Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text(task.ID)),
			kitex.TD(kitex.TDProps{Style: style.S().Foreground(t.Color.Surface.Info).PaddingRight(1).Width(style.MaxContent)}, kitex.Text(strings.ToUpper(task.Type))),
			kitex.TD(kitex.TDProps{Style: style.S().Foreground(t.Color.Surface.Success).PaddingRight(1).Width(style.MaxContent)}, kitex.Text(dispDetails)),
			kitex.TD(kitex.TDProps{Style: style.S().Foreground(t.Color.Text.Primary).Width(style.Percent(100))}, kitex.Text(dispName)),
		))
	}

	headerRow := kitex.TR(kitex.TRProps{},
		// Columns: TASK ID | TYPE | DETAILS | COMMAND / NAME
		kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text("TASK ID")),
		kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text("TYPE")),
		kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text("DETAILS")),
		kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).Width(style.Percent(100))}, kitex.Text("COMMAND / NAME")),
	)

	allRows := append([]kitex.Node{headerRow}, taskRows...)

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			MarginTop(1).
			MarginBottom(1).
			Width(style.Percent(100)).
			MaxWidth(style.Percent(90)).
			AlignSelf(style.AlignStart),
	},
		components.Accordion(components.AccordionProps{
			Color:   components.PaperSurface,
			Variant: components.PaperOutlined,
		},
			components.AccordionSummary(components.AccordionSummaryProps{},
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary).Bold(true)}, kitex.Text(summaryText)),
			),
			components.AccordionDetails(components.AccordionDetailsProps{
				Style: style.S().Padding(1, 1),
			},
				kitex.Table(kitex.TableProps{},
					kitex.TBody(kitex.TBodyProps{},
						allRows...,
					),
				),
			),
		),
	)
})
