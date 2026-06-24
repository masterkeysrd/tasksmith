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
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
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

func getTargetOptionForHorizontal(options []permissions.PermissionOption, hIdx int) permissions.PermissionOption {
	if len(options) == 0 {
		return permissions.PermissionOption{}
	}
	if hIdx < 0 {
		hIdx = 0
	}
	if hIdx >= len(options) {
		hIdx = len(options) - 1
	}
	return options[hIdx]
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
	mainAgentName := "main"
	if sessionsQuery.Data != nil {
		for _, s := range sessionsQuery.Data.Sessions {
			if s.ID == sessionID {
				title = s.Title
				mainAgentName = s.AgentName
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
	outerRef := kitex.CreateRef[dom.Element]()

	// Authorization choices state
	selectedIndex, setSelectedIndex := kitex.UseState(0)
	selectedScopeIndex, setSelectedScopeIndex := kitex.UseState(0)
	showPreviewModal, setShowPreviewModal := kitex.UseState(false)
	currentPendingIndex, setCurrentPendingIndex := kitex.UseState(0)
	localDecisions, setLocalDecisions := kitex.UseState(map[string]permissions.AuthorizationDecision{})

	handleSelectVertical := func(idx int) {
		setSelectedIndex(idx)
		mode.Set(mode.Normal)
	}

	handleSelectHorizontal := func(idx int) {
		setSelectedScopeIndex(idx)
		mode.Set(mode.Normal)
	}

	var pendingAuthorizations []permissions.AuthorizationRequest
	if stateQuery.Data != nil {
		pendingAuthorizations = stateQuery.Data.PendingAuthorizations
	}

	var pendingLspSuggestions []api.LspSuggestion
	if stateQuery.Data != nil {
		pendingLspSuggestions = stateQuery.Data.PendingLspSuggestions
	}

	handleConfigureLsp := func(lang string) {
		go func() {
			_, err := client.ConfigureLsp(context.Background(), api.ConfigureLspRequest{Language: lang})
			if err != nil {
				log.Error(fmt.Sprintf("Failed to configure LSP for %s: %v", lang, err))
			}
			stateQuery.Refetch()
		}()
	}

	handleDismissLsp := func(lang string) {
		go func() {
			_, err := client.DismissLspSuggestion(context.Background(), api.DismissLspSuggestionRequest{Language: lang})
			if err != nil {
				log.Error(fmt.Sprintf("Failed to dismiss LSP suggestion for %s: %v", lang, err))
			}
			stateQuery.Refetch()
		}()
	}

	kitex.UseEffect(func() {
		setSelectedIndex(0)
		setSelectedScopeIndex(0)
		setShowPreviewModal(false)
		setCurrentPendingIndex(0)
		setLocalDecisions(map[string]permissions.AuthorizationDecision{})
		if len(pendingAuthorizations) > 0 {
			mode.Set(mode.Normal)
		}
	}, []any{len(pendingAuthorizations)})

	recordDecision := func(toolCallID string, approved bool, target string, scope permissions.PermissionScope) {
		dec := permissions.AuthorizationDecision{
			ToolCallID:     toolCallID,
			Approved:       approved,
			Scope:          scope,
			SelectedTarget: target,
		}

		newDecisions := make(map[string]permissions.AuthorizationDecision)
		for k, v := range localDecisions() {
			newDecisions[k] = v
		}
		newDecisions[toolCallID] = dec
		setLocalDecisions(newDecisions)

		nextIdx := currentPendingIndex() + 1
		if nextIdx < len(pendingAuthorizations) {
			setCurrentPendingIndex(nextIdx)
			setSelectedIndex(0)
			setSelectedScopeIndex(0)
			setShowPreviewModal(false)
		} else {
			if submitting() {
				return
			}
			setSubmitting(true)
			setShowPreviewModal(false)

			promise.New(func(ctx context.Context) (bool, error) {
				var decisionList []permissions.AuthorizationDecision
				for _, d := range pendingAuthorizations {
					if res, ok := newDecisions[d.ToolCallID]; ok {
						decisionList = append(decisionList, res)
					}
				}
				_, err := client.SubmitAuthorizationDecision(ctx, api.SubmitAuthorizationDecisionRequest{
					SessionID: sessionID,
					Decisions: decisionList,
				})
				if err != nil {
					return false, err
				}
				return true, nil
			}).Then(func(success bool) {
				setSubmitting(false)
				windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
				windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
			}, func(err error) {
				setSubmitting(false)
				log.Error(fmt.Sprintf("Failed to submit authorization decisions: %v", err))
			})
		}
	}

	// Focus management: when insert mode is active, focus composer input.
	// When normal mode is active, focus the outer container so we can receive global hotkeys.
	kitex.UseEffect(func() {
		if isInsert {
			kitex.PostMacro(func() {
				if inputRef.Current != nil {
					if doc := inputRef.Current.OwnerDocument(); doc != nil {
						doc.Focus(inputRef.Current)
					}
				}
			})
		} else {
			kitex.PostMacro(func() {
				if outerRef.Current != nil {
					outerRef.Current.SetTabIndex(0)
					if doc := outerRef.Current.OwnerDocument(); doc != nil {
						doc.Focus(outerRef.Current)
					}
				}
			})
		}
	}, []any{isInsert})

	// Refs to bridge latest react states to the single-registration document listener
	pendingAuthsRef := kitex.UseRef[[]permissions.AuthorizationRequest](nil)
	pendingAuthsRef.Current = pendingAuthorizations

	selectedIndexRef := kitex.UseRef(0)
	selectedIndexRef.Current = selectedIndex()

	selectedScopeIndexRef := kitex.UseRef(0)
	selectedScopeIndexRef.Current = selectedScopeIndex()

	showPreviewModalRef := kitex.UseRef(false)
	showPreviewModalRef.Current = showPreviewModal()

	modeRef := kitex.UseRef(mode.Normal)
	modeRef.Current = m

	currentPendingIndexRef := kitex.UseRef(0)
	currentPendingIndexRef.Current = currentPendingIndex()

	recordDecisionRef := kitex.UseRef[func(string, bool, string, permissions.PermissionScope)](nil)
	recordDecisionRef.Current = recordDecision

	handleApprove := func() {
		currIdx := currentPendingIndexRef.Current
		if currIdx >= len(pendingAuthsRef.Current) {
			return
		}
		req := pendingAuthsRef.Current[currIdx]
		vIdx := selectedIndexRef.Current
		hIdx := selectedScopeIndexRef.Current

		if vIdx == 4 { // Deny
			if recordDecisionRef.Current != nil {
				recordDecisionRef.Current(req.ToolCallID, false, "", permissions.ScopeOnce)
			}
			return
		}

		var target string
		if len(req.Options) > 0 {
			target = getTargetOptionForHorizontal(req.Options, hIdx).Target
		}

		var scope permissions.PermissionScope
		switch vIdx {
		case 0:
			scope = permissions.ScopeOnce
			if len(req.Options) > 0 {
				target = req.Options[0].Target
			}
		case 1:
			scope = permissions.ScopeSession
		case 2:
			scope = permissions.ScopeWorkspace
		case 3:
			scope = permissions.ScopeGlobal
		}

		if recordDecisionRef.Current != nil {
			recordDecisionRef.Current(req.ToolCallID, true, target, scope)
		}
	}

	handleDeny := func() {
		currIdx := currentPendingIndexRef.Current
		if currIdx >= len(pendingAuthsRef.Current) {
			return
		}
		req := pendingAuthsRef.Current[currIdx]
		if recordDecisionRef.Current != nil {
			recordDecisionRef.Current(req.ToolCallID, false, "", permissions.ScopeOnce)
		}
	}

	// Document-level KeyDown listener registered when outerRef is available
	kitex.UseEffectCleanup(func() func() {
		if outerRef.Current == nil {
			return nil
		}
		doc := outerRef.Current.OwnerDocument()
		if doc == nil {
			return nil
		}

		sub := doc.AddEventListener(event.EventKeyDown, func(e event.Event) {
			isModalOpen := showPreviewModalRef.Current
			if !isModalOpen && modeRef.Current != mode.Normal {
				return
			}
			if len(pendingAuthsRef.Current) == 0 {
				return
			}

			ke, ok := e.(*event.KeyEvent)
			if !ok {
				return
			}

			currIdx := currentPendingIndexRef.Current
			if currIdx >= len(pendingAuthsRef.Current) {
				return
			}

			req := pendingAuthsRef.Current[currIdx]
			optsCount := len(req.Options)

			// Vertical Scope Index: 0: Once, 1: Session, 2: Workspace, 3: Global, 4: Deny
			if ke.Text == "j" || ke.Code == key.KeyDown {
				e.PreventDefault()
				e.StopPropagation()
				setSelectedIndex((selectedIndexRef.Current + 1) % 5)
				return
			}
			if ke.Text == "k" || ke.Code == key.KeyUp {
				e.PreventDefault()
				e.StopPropagation()
				setSelectedIndex((selectedIndexRef.Current - 1 + 5) % 5)
				return
			}

			// Horizontal Target Index (only active for Session, Workspace, Global)
			hasHorizontal := selectedIndexRef.Current == 1 || selectedIndexRef.Current == 2 || selectedIndexRef.Current == 3
			if hasHorizontal && optsCount > 1 {
				if ke.Text == "h" || ke.Code == key.KeyLeft {
					e.PreventDefault()
					e.StopPropagation()
					setSelectedScopeIndex((selectedScopeIndexRef.Current - 1 + optsCount) % optsCount)
					return
				}
				if ke.Text == "l" || ke.Code == key.KeyRight {
					e.PreventDefault()
					e.StopPropagation()
					setSelectedScopeIndex((selectedScopeIndexRef.Current + 1) % optsCount)
					return
				}
			}

			if ke.Code == key.KeyEnter || ke.Text == "\r" || ke.Text == "\n" {
				e.PreventDefault()
				e.StopPropagation()
				handleApprove()
				return
			}
			if ke.Code == key.KeyEscape || ke.Text == "q" {
				e.PreventDefault()
				e.StopPropagation()
				if showPreviewModalRef.Current {
					setShowPreviewModal(false)
				} else {
					handleDeny()
				}
				return
			}
			if ke.Text == "d" {
				e.PreventDefault()
				e.StopPropagation()
				handleDeny()
				return
			}
			if ke.Text == "p" || ke.Text == "P" {
				if req.Preview != "" {
					e.PreventDefault()
					e.StopPropagation()
					setShowPreviewModal(!showPreviewModalRef.Current)
				}
				return
			}
		})
		return func() {
			sub.Cancel()
		}
	}, []any{outerRef.Current != nil})

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
			windClient.InvalidateQueries(api.ListSessionsRequest{})
		}
	}, 1000*time.Millisecond, []any{sending, hasRunningTasks, sessionID})

	// Invalidate messages query when the agent finishes execution (sending transitions to false)
	kitex.UseEffect(func() {
		if !sending {
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
			windClient.InvalidateQueries(api.ListSessionsRequest{}) // Update sidebar session states (like metrics)
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
			log.Error(fmt.Sprintf("Failed to send message to backend: %v", err))
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

	outerProps := kitex.BoxProps{
		Style: outerStyle,
		Ref:   outerRef,
	}

	return kitex.Box(outerProps,
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
					var isGenerating bool
					if stateQuery.Data != nil {
						isGenerating = stateQuery.Data.IsGenerating
					}
					nodes := renderBubbles(
						messages,
						toolResponses,
						currentDots,
						oneDotCurrentDots,
						mainAgentName,
						isGenerating,
						thinkingTime(),
						pendingAuthorizations,
						selectedIndex(),
						selectedScopeIndex(),
						func() { setShowPreviewModal(true) },
						currentPendingIndex(),
						isInsert,
						handleSelectVertical,
						handleSelectHorizontal,
						handleApprove,
						handleDeny,
					)

					var runPromptTokens, runCompletionTokens, runTotalTokens int
					for i := len(messages) - 1; i >= 0; i-- {
						msg := messages[i]
						if msg.Role() == message.RoleUser {
							break
						}
						if asstMsg, ok := msg.(*message.Assistant); ok {
							if asstMsg.Metrics != nil {
								runPromptTokens += asstMsg.Metrics.Tokens.Input
								runCompletionTokens += asstMsg.Metrics.Tokens.Output
								runTotalTokens += asstMsg.Metrics.TotalTokens
							} else if meta := asstMsg.GetMetadata(); meta != nil {
								if promptToks, ok := meta["prompt_tokens"].(int); ok {
									runPromptTokens += promptToks
								} else if promptToksFloat, ok := meta["prompt_tokens"].(float64); ok {
									runPromptTokens += int(promptToksFloat)
								}
								if compToks, ok := meta["completion_tokens"].(int); ok {
									runCompletionTokens += compToks
								} else if compToksFloat, ok := meta["completion_tokens"].(float64); ok {
									runCompletionTokens += int(compToksFloat)
								}
								if totalToks, ok := meta["total_tokens"].(int); ok {
									runTotalTokens += totalToks
								} else if totalToksFloat, ok := meta["total_tokens"].(float64); ok {
									runTotalTokens += int(totalToksFloat)
								}
							}
						}
					}

					if statusNode := renderAgentStatus(t, sending, thinkingTime(), lastFinishedTime(), currentDots, runPromptTokens, runCompletionTokens, runTotalTokens, isGenerating); statusNode != nil {
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
					if len(pendingLspSuggestions) > 0 {
						nodes = append(nodes, LspSuggestionWidget(LspSuggestionWidgetProps{
							Suggestions: pendingLspSuggestions,
							OnConfigure: handleConfigureLsp,
							OnDismiss:   handleDismissLsp,
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

		// Modal Section for Authorization Preview
		components.Modal(components.ModalProps{
			IsOpen:  showPreviewModal(),
			Title:   kitex.Text("Authorization Preview"),
			OnClose: func() { setShowPreviewModal(false) },
		},
			kitex.If(showPreviewModal() && len(pendingAuthorizations) > 0 && currentPendingIndex() < len(pendingAuthorizations), func() kitex.Node {
				req := pendingAuthorizations[currentPendingIndex()]
				var leftNode kitex.Node
				if isDiff(req.Preview) {
					leftNode = components.DiffBlock(components.DiffBlockProps{
						Diff:  req.Preview,
						Split: false,
					})
				} else {
					leftNode = components.CodeBlock(components.CodeBlockProps{
						Code:            req.Preview,
						HideHeader:      true,
						ShowLineNumbers: false,
					})
				}

				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						Width(style.Percent(100)).
						Height(style.Percent(100)).
						Gap(2),
				},
					// Left Panel: Preview
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Flex(2, 2, style.Cells(0)).
							MinWidth(style.Cells(0)).
							Height(style.Percent(100)).
							Display(style.DisplayFlex).
							FlexDirection(style.FlexColumn).
							Padding(1).
							Overflow(style.OverflowAuto),
					},
						leftNode,
					),
					// Right Panel: Options & Scopes
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Flex(1, 1, style.Cells(0)).
							MinWidth(style.Cells(0)).
							Height(style.Percent(100)).
							Display(style.DisplayFlex).
							FlexDirection(style.FlexColumn).
							BorderLeft(true, style.SingleBorder(), t.Color.Border.Primary).
							Background(t.Color.Surface.BaseFocus).
							Padding(1).
							Gap(1).
							Overflow(style.OverflowAuto),
					},
						// Target Details
						kitex.Box(kitex.BoxProps{Style: style.S().Bold(true).PaddingVertical(1).Foreground(t.Color.Text.Primary)},
							kitex.Text("Authorization Details:"),
						),
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexRow).
								Gap(1).
								PaddingBottom(1),
						},
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text("Tool:")),
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Magenta).Bold(true)}, kitex.Text(req.ToolName)),
						),
						kitex.If(len(req.Options) > 0, func() kitex.Node {
							return kitex.Box(kitex.BoxProps{
								Style: style.S().
									Display(style.DisplayFlex).
									FlexDirection(style.FlexRow).
									Gap(1).
									PaddingBottom(1),
							},
								kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text("Target:")),
								kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Purple).Bold(true)}, kitex.Text(req.Options[0].Target)),
							)
						}),

						// Hybrid Selector
						kitex.Box(kitex.BoxProps{
							Style: style.S().PaddingBottom(0),
						},
							AuthorizationHybridSelector(AuthorizationHybridSelectorProps{
								Options:            req.Options,
								VerticalIndex:      selectedIndex(),
								HorizontalIndex:    selectedScopeIndex(),
								IsActive:           true,
								OnSelectVertical:   handleSelectVertical,
								OnSelectHorizontal: handleSelectHorizontal,
							}),
						),

						// Action Buttons
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexRow).
								AlignItems(style.AlignCenter).
								MarginTop(1).
								MarginBottom(0),
						},
							components.Button(components.ButtonProps{
								Variant: components.ButtonText,
								Color:   components.ButtonSuccess,
								Style:   style.S().MarginRight(1),
								OnClick: func() {
									handleApprove()
								},
							}, kitex.Text("Approve [Enter]")),
							components.Button(components.ButtonProps{
								Variant: components.ButtonText,
								Color:   components.ButtonError,
								OnClick: func() {
									handleDeny()
								},
							}, kitex.Text("Deny [d]")),
						),

						// Instructions
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Border(style.SingleBorder().Color(t.Color.Border.Primary)).
								Padding(1).
								MarginTop(1).
								Foreground(t.Color.Text.Secondary).
								Width(style.Percent(100)),
						},
							func() kitex.Node {
								text := "[j/k] Navigate Scope"
								if len(req.Options) > 1 && (selectedIndex() == 1 || selectedIndex() == 2 || selectedIndex() == 3) {
									text += "\n[h/l] Limit Target"
								}
								text += "\n[Enter] Approve Choice\n[d] Deny request\n[Esc/q] Close Preview"
								return kitex.Text(text)
							}(),
						),
					),
				)
			}),
		),
	)
})

func isDiff(preview string) bool {
	return strings.Contains(preview, "@@ ") || strings.HasPrefix(preview, "--- ") || strings.HasPrefix(preview, "+++ ")
}

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
	Content  string
	Duration time.Duration
	Tokens   int
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
			tokenStr = fmt.Sprintf("↑ %s ↓ %s", formatTokens(props.TokensInput), formatTokens(props.TokensOutput))
		} else {
			tokenStr = fmt.Sprintf("%s TOTAL", formatTokens(props.TokensTotal))
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
	Role                  message.Role
	Content               message.Content
	ToolResponses         map[string]*message.Tool
	CurrentDots           string
	OneDotCurrentDots     string
	ReasoningTokens       int
	ThinkingDuration      time.Duration
	PendingAuthorizations []permissions.AuthorizationRequest
	SelectedIndex         int
	SelectedScopeIndex    int
	OnPreview             func()
	CurrentPendingIndex   int
	IsInsert              bool
	OnSelectVertical      func(int)
	OnSelectHorizontal    func(int)
	OnApprove             func()
	OnDeny                func()
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

// parseStructuredOutput converts a generic interface to a structured type T.
// Handles both same-process typed assertions and cross-process JSON fallback.
func parseStructuredOutput[T any](structured any) (T, bool) {
	if structured == nil {
		var zero T
		return zero, false
	}
	if val, ok := structured.(T); ok {
		return val, true
	}
	if val, ok := structured.(*T); ok && val != nil {
		return *val, true
	}
	data, err := json.Marshal(structured)
	if err != nil {
		var zero T
		return zero, false
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		var zero T
		return zero, false
	}
	return out, true
}

func parseViewStructuredOutput(structured any) (tools.ViewOutput, bool) {
	return parseStructuredOutput[tools.ViewOutput](structured)
}

func parseWriteStructuredOutput(structured any) (tools.WriteOutput, bool) {
	return parseStructuredOutput[tools.WriteOutput](structured)
}

func parseEditStructuredOutput(structured any) (tools.EditOutput, bool) {
	return parseStructuredOutput[tools.EditOutput](structured)
}

func parseMultiEditStructuredOutput(structured any) (tools.MultiEditOutput, bool) {
	return parseStructuredOutput[tools.MultiEditOutput](structured)
}

func parseRemoveStructuredOutput(structured any) (tools.RemoveOutput, bool) {
	return parseStructuredOutput[tools.RemoveOutput](structured)
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
	out, ok := parseStructuredOutput[tools.LsOutput](structured)
	if !ok {
		return
	}
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

// parseGlobOutput extracts structured file lists, count, and truncation from a glob tool result.
func parseGlobOutput(structured any) (matches []string, totalCount int, truncated bool) {
	out, ok := parseStructuredOutput[tools.GlobOutput](structured)
	if ok {
		return out.Matches, out.TotalCount, out.Truncated
	}
	return
}

// parseGrepOutput extracts structured matches, count, and truncation from a grep tool result.
func parseGrepOutput(structured any) (matches []tools.GrepOutputMatchesItem, totalCount int, truncated bool) {
	out, ok := parseStructuredOutput[tools.GrepOutput](structured)
	if ok {
		return out.Matches, out.TotalCount, out.Truncated
	}
	return
}

// parseWebSearchOutput extracts structured results from a web_search tool result.
func parseWebSearchOutput(structured any) (results []tools.WebSearchOutputResultsItem) {
	out, ok := parseStructuredOutput[tools.WebSearchOutput](structured)
	if ok {
		return out.Results
	}
	return nil
}

// parseWebFetchStructuredOutput extracts structured WebFetchOutput fields from a web_fetch tool result.
func parseWebFetchStructuredOutput(structured any) (out tools.WebFetchOutput, ok bool) {
	return parseStructuredOutput[tools.WebFetchOutput](structured)
}

// parseDownloadOutput extracts structured DownloadOutput fields from a download tool result.
func parseDownloadOutput(structured any) (out tools.DownloadOutput, ok bool) {
	return parseStructuredOutput[tools.DownloadOutput](structured)
}

// parseFetchOutput extracts structured FetchOutput fields from a fetch tool result.
func parseFetchOutput(structured any) (out tools.FetchOutput, ok bool) {
	return parseStructuredOutput[tools.FetchOutput](structured)
}

// parseTasksOutput extracts structured TasksOutput fields from a tasks tool result.
func parseTasksOutput(structured any) (out tools.TasksOutput, ok bool) {
	return parseStructuredOutput[tools.TasksOutput](structured)
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
	if props.ToolCall != nil && props.ToolCall.Name == "web_search" {
		return WebSearchToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "web_fetch" {
		return WebFetchToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "download" {
		return DownloadToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "fetch" {
		return FetchToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "activate_skill" {
		return ActivateSkillToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "todos" {
		return TodosToolWidget(props)
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
					node = CollapsibleThinking(CollapsibleThinkingProps{
						Content:  b.Thinking,
						Duration: props.ThinkingDuration,
						Tokens:   props.ReasoningTokens,
					})
				}
			case *message.ToolCall:
				var pendingReq *permissions.AuthorizationRequest
				for _, req := range props.PendingAuthorizations {
					if req.ToolCallID == b.ID {
						pendingReq = &req
						break
					}
				}

				if pendingReq != nil {
					isActive := len(props.PendingAuthorizations) > 0 &&
						props.CurrentPendingIndex < len(props.PendingAuthorizations) &&
						pendingReq.ToolCallID == props.PendingAuthorizations[props.CurrentPendingIndex].ToolCallID
					node = AuthorizationWidget(AuthorizationWidgetProps{
						Request:            *pendingReq,
						SelectedIndex:      props.SelectedIndex,
						SelectedScopeIndex: props.SelectedScopeIndex,
						OnPreview:          props.OnPreview,
						IsActive:           isActive,
						IsFocused:          isActive && !props.IsInsert,
						OnSelectVertical:   props.OnSelectVertical,
						OnSelectHorizontal: props.OnSelectHorizontal,
						OnApprove:          props.OnApprove,
						OnDeny:             props.OnDeny,
					})
				} else {
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

func renderBubbles(
	messages message.MessageList,
	toolResponses map[string]*message.Tool,
	currentDots string,
	oneDotCurrentDots string,
	mainAgentName string,
	isGenerating bool,
	liveThinkingTime int,
	pendingAuthorizations []permissions.AuthorizationRequest,
	selectedIndex int,
	selectedScopeIndex int,
	onPreview func(),
	currentPendingIndex int,
	isInsert bool,
	onSelectVertical func(int),
	onSelectHorizontal func(int),
	onApprove func(),
	onDeny func(),
) []kitex.Node {
	if len(messages) == 0 {
		return nil
	}

	var nodes []kitex.Node
	var currentGroup []message.Message
	var currentGroupRole message.Role

	flush := func() {
		if len(currentGroup) > 0 {
			groupIsGenerating := false
			if isGenerating && len(messages) > 0 && currentGroup[len(currentGroup)-1] == messages[len(messages)-1] {
				groupIsGenerating = true
			}
			if node := createBubbleNode(
				currentGroupRole,
				currentGroup,
				toolResponses,
				currentDots,
				oneDotCurrentDots,
				mainAgentName,
				groupIsGenerating,
				liveThinkingTime,
				pendingAuthorizations,
				selectedIndex,
				selectedScopeIndex,
				onPreview,
				currentPendingIndex,
				isInsert,
				onSelectVertical,
				onSelectHorizontal,
				onApprove,
				onDeny,
			); node != nil {
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

func createBubbleNode(
	role message.Role,
	msgs []message.Message,
	toolResponses map[string]*message.Tool,
	currentDots string,
	oneDotCurrentDots string,
	mainAgentName string,
	isGenerating bool,
	liveThinkingTime int,
	pendingAuthorizations []permissions.AuthorizationRequest,
	selectedIndex int,
	selectedScopeIndex int,
	onPreview func(),
	currentPendingIndex int,
	isInsert bool,
	onSelectVertical func(int),
	onSelectHorizontal func(int),
	onApprove func(),
	onDeny func(),
) kitex.Node {
	timestamp := ""
	var msgAgentName string
	if len(msgs) > 0 {
		meta := msgs[0].GetMetadata()
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
	for _, msg := range msgs {
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

	var children []kitex.Node
	for i, msg := range msgs {
		if msg.Role() == message.RoleTool {
			continue // Do not render tool messages as separate children. They are rendered inline in the assistant message.
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
			if isGenerating && i == len(msgs)-1 {
				thinkingDuration = time.Duration(liveThinkingTime) * time.Second
			}
		}

		node := Message(MessageProps{
			Role:                  msg.Role(),
			Content:               msg.GetContent(),
			ToolResponses:         toolResponses,
			CurrentDots:           currentDots,
			OneDotCurrentDots:     oneDotCurrentDots,
			ReasoningTokens:       reasoningTokens,
			ThinkingDuration:      thinkingDuration,
			PendingAuthorizations: pendingAuthorizations,
			SelectedIndex:         selectedIndex,
			SelectedScopeIndex:    selectedScopeIndex,
			OnPreview:             onPreview,
			CurrentPendingIndex:   currentPendingIndex,
			IsInsert:              isInsert,
			OnSelectVertical:      onSelectVertical,
			OnSelectHorizontal:    onSelectHorizontal,
			OnApprove:             onApprove,
			OnDeny:                onDeny,
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
		AgentName:            msgAgentName,
		MainAgentName:        mainAgentName,
		TokensInput:          tokensInput,
		TokensOutput:         tokensOutput,
		TokensTotal:          tokensTotal,
	})
}

func formatTokens(count int) string {
	if count < 1000 {
		return fmt.Sprintf("%d", count)
	}
	return fmt.Sprintf("%.1fk", float64(count)/1000.0)
}

func renderAgentStatus(t *theme.Scheme, sending bool, thinkingTime int, lastFinishedTime int, currentDots string, runPromptTokens, runCompletionTokens, runTotalTokens int, isGenerating bool) kitex.Node {
	if t == nil {
		return nil
	}
	blueColor := t.Color.Surface.Info
	greenColor := t.Color.Surface.Success
	timeStr := fmt.Sprintf("[%02d:%02d]", thinkingTime/60, thinkingTime%60)

	upColor := t.Color.Text.Tertiary
	downColor := t.Color.Text.Tertiary
	if sending {
		if isGenerating {
			downColor = t.Color.Surface.Success // highlight down when streaming text
		} else {
			upColor = t.Color.Surface.Info // highlight up when processing/waiting
		}
	}

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

		var cumNodes []kitex.Node
		if runPromptTokens > 0 || runCompletionTokens > 0 || runTotalTokens > 0 {
			if runPromptTokens > 0 || runCompletionTokens > 0 {
				cumNodes = append(cumNodes, kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).Foreground(t.Color.Text.Tertiary),
				},
					kitex.Text("("),
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(upColor)}, kitex.Text(fmt.Sprintf("↑ %s", formatTokens(runPromptTokens)))),
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(downColor)}, kitex.Text(fmt.Sprintf("↓ %s", formatTokens(runCompletionTokens)))),
					kitex.Text(")"),
				))
			} else {
				cumNodes = append(cumNodes, kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text(fmt.Sprintf("(%s TOTAL)", formatTokens(runTotalTokens)))))
			}
		}

		return kitex.Box(kitex.BoxProps{Style: containerStyle},
			kitex.Box(kitex.BoxProps{Style: dotsStyle}, kitex.Text(currentDots)),
			kitex.Box(kitex.BoxProps{Style: labelStyle}, kitex.Text("Thinking")),
			kitex.Box(kitex.BoxProps{Style: timeStyle}, kitex.Text(timeStr)),
			kitex.If(len(cumNodes) > 0, func() kitex.Node { return cumNodes[0] }),
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

		var cumNodes []kitex.Node
		if runPromptTokens > 0 || runCompletionTokens > 0 || runTotalTokens > 0 {
			var tokenStr string
			if runPromptTokens > 0 || runCompletionTokens > 0 {
				tokenStr = fmt.Sprintf("(↑ %s ↓ %s)", formatTokens(runPromptTokens), formatTokens(runCompletionTokens))
			} else {
				tokenStr = fmt.Sprintf("(%s TOTAL)", formatTokens(runTotalTokens))
			}
			cumNodes = append(cumNodes, kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(" "+tokenStr)))
		}

		return kitex.Box(kitex.BoxProps{Style: containerStyle},
			kitex.Box(kitex.BoxProps{Style: checkStyle}, icon.Checkmark),
			kitex.Box(kitex.BoxProps{Style: labelStyle}, kitex.Text("Agent completed in")),
			kitex.Box(kitex.BoxProps{Style: timeStyle}, kitex.Text(finishedTimeStr)),
			kitex.If(len(cumNodes) > 0, func() kitex.Node { return cumNodes[0] }),
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

type LspSuggestionWidgetProps struct {
	Suggestions []api.LspSuggestion
	OnConfigure func(lang string)
	OnDismiss   func(lang string)
}

var LspSuggestionWidget = kitex.FC("LspSuggestionWidget", func(props LspSuggestionWidgetProps) kitex.Node {
	if len(props.Suggestions) == 0 {
		return nil
	}

	var boxes []kitex.Node
	for _, sug := range props.Suggestions {
		sugLang := sug.Language // capture loop variable
		boxes = append(boxes, kitex.Box(kitex.BoxProps{
			Style: style.S().
				MarginBottom(1).
				Width(style.Percent(100)).
				MaxWidth(style.Percent(90)),
		},
			components.Alert(components.AlertProps{
				Severity: components.AlertInfo,
				Variant:  components.AlertOutlined,
				ShowIcon: true,
				Action: kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
				},
					components.Button(components.ButtonProps{
						Variant: components.ButtonSolid,
						Color:   components.ButtonInfo,
						OnClick: func() { props.OnConfigure(sugLang) },
					}, kitex.Text("Configure")),
					components.Button(components.ButtonProps{
						Variant: components.ButtonText,
						Color:   components.ButtonBase,
						OnClick: func() { props.OnDismiss(sugLang) },
					}, kitex.Text("Dismiss")),
				),
			}, kitex.Text(fmt.Sprintf("Enable %s language server for %s?", sug.ServerName, sug.Language))),
		))
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).MarginTop(1).MarginBottom(1).AlignSelf(style.AlignStart).Width(style.Percent(100)),
	}, boxes...)
})
