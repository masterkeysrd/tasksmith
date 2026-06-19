package chat

import (
	"context"
	"fmt"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	loomollama "github.com/masterkeysrd/loom/llm/ollama"
	"github.com/masterkeysrd/loom/message"
	agentgraph "github.com/masterkeysrd/tasksmith/internal/agent/graph"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// ViewProps defines the properties for the Chat view.
type ViewProps struct{}

// View is the main Chat view component.
var View = kitex.FC("ChatView", func(props ViewProps) kitex.Node {
	t := theme.UseTheme()

	// 1. Reactive state for messages list
	messages, setMessages := kitex.UseState(message.MessageList{
		message.NewSystemText("Welcome to TaskSmith Chat. Type your message below."),
	})

	// 2. Reactive state for input composer
	inputValue, setInputValue := kitex.UseState("")

	// 3. Keep track of model loading or sending state
	sending, setSending := kitex.UseState(false)

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

	// 4. Ollama caller setup
	ollamaCaller := func(ctx context.Context, msgs []message.Message) (*message.Assistant, error) {
		provider, err := loomollama.NewDefaultProvider()
		if err != nil {
			return nil, fmt.Errorf("failed to create ollama provider: %w", err)
		}
		// Hardcoded model "qwen3.6:35b-a3b-coding-nvfp4", but it will work with whatever is run in ollama.
		model, err := llm.NewModel(provider, "qwen3.6:35b-a3b-coding-nvfp4", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create model: %w", err)
		}
		return model.Invoke(ctx, msgs)
	}

	// 5. Send message action
	sendMessage := func(text string) {
		if text == "" || sending() {
			return
		}

		// Exit insert mode upon sending
		mode.Set(mode.Normal)

		userMsg := message.NewUserText(text)
		placeholderAsst := &message.Assistant{}

		// Append message to history list
		old := messages()
		newList := make(message.MessageList, len(old))
		copy(newList, old)
		newList = append(newList, userMsg, placeholderAsst)

		setMessages(newList)
		setInputValue("")
		setSending(true)

		// Create a Promise to handle the LLM call asynchronously in a background goroutine
		promise.New(func(ctx context.Context) (*message.Assistant, error) {
			ag := agentgraph.New(ollamaCaller)
			g, err := ag.Build(nil)
			if err != nil {
				return nil, err
			}

			hist := messages()
			historyForGraph := make(message.MessageList, len(hist)-1)
			copy(historyForGraph, hist[:len(hist)-1])

			initialState := agentgraph.AgentState{
				Messages: historyForGraph,
			}

			initCmd := graph.Update[agentgraph.AgentState](func(s agentgraph.AgentState) agentgraph.AgentState {
				return initialState
			})

			seq, err := g.Stream(ctx, initCmd, nil)
			if err != nil {
				return nil, err
			}

			var accumulatedText string

			// Consume the generator sequence asynchronously
			for ev, err := range seq {
				if err != nil {
					return nil, err
				}

				if ev.Event == graph.EventLLMChunk {
					switch d := ev.Data.(type) {
					case message.AssistantChunk:
						accumulatedText += message.Content(d.Content).Text()
					case *message.AssistantChunk:
						accumulatedText += message.Content(d.Content).Text()
					case string:
						accumulatedText += d
					}

					current := messages()
					updatedList := make(message.MessageList, len(current))
					copy(updatedList, current)
					if len(updatedList) > 0 {
						lastIdx := len(updatedList) - 1
						if asst, ok := updatedList[lastIdx].(*message.Assistant); ok {
							asst.Content = message.Content{
								&message.TextBlock{Text: accumulatedText},
							}
						}
					}
					setMessages(updatedList)
				}
			}

			finalAsst := &message.Assistant{
				Content: message.Content{
					&message.TextBlock{Text: accumulatedText},
				},
			}
			return finalAsst, nil
		}).Then(func(finalMsg *message.Assistant) {
			current := messages()
			updatedList := make(message.MessageList, len(current))
			copy(updatedList, current)
			if len(updatedList) > 0 {
				updatedList[len(updatedList)-1] = finalMsg
			}
			setMessages(updatedList)
			setSending(false)
		}, func(err error) {
			errorMsg := &message.Assistant{
				Content: message.Content{
					&message.TextBlock{Text: fmt.Sprintf("Error: %v", err)},
				},
			}
			current := messages()
			updatedList := make(message.MessageList, len(current))
			copy(updatedList, current)
			if len(updatedList) > 0 {
				updatedList[len(updatedList)-1] = errorMsg
			}
			setMessages(updatedList)
			setSending(false)
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
			kitex.Map(messages(), func(msg message.Message, idx int) kitex.Node {
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
				Disabled:    sending(),
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
