package chat

import (
	"context"
	"encoding/json"
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
				for {
					select {
					case <-ticker.C:
						if !polling() {
							return
						}
						kitex.PostMacro(func() {
							windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
							windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
						})
					}
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
		// Message History Section
		kitex.Box(kitex.BoxProps{Style: messagesContainerStyle},
			kitex.Map(messages, func(msg message.Message, idx int) kitex.Node {
				role := msg.Role()
				content := msg.GetContent().Text()

				var align style.Align
				var bubbleStyle style.Style

				if t != nil {
					bubbleStyle = style.S().
						Padding(1).
						MarginBottom(1).
						MaxWidth(style.Percent(70))

					if role == message.RoleUser {
						align = style.AlignEnd
						bubbleStyle = bubbleStyle.
							Background(t.Color.Surface.Primary).
							Foreground(t.Color.Text.Primary).
							Border(style.SingleBorder().Color(t.Color.Border.Primary))
					} else if role == message.RoleSystem {
						align = style.AlignCenter
						bubbleStyle = bubbleStyle.
							Background(t.Color.Surface.BaseDisabled).
							Foreground(t.Color.Text.Tertiary)
					} else {
						align = style.AlignStart
						bubbleStyle = bubbleStyle.
							Background(t.Color.Surface.Secondary).
							Foreground(t.Color.Text.Primary).
							Border(style.SingleBorder().Color(t.Color.Border.Primary))
					}
				}

				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Width(style.Percent(100)).
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						AlignItems(align),
				},
					kitex.Box(kitex.BoxProps{Style: bubbleStyle},
						kitex.Text(content),
					),
				)
			}),
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
