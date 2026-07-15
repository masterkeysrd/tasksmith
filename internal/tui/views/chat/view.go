package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"maps"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/geom"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/agent/resolver"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/preview"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	"github.com/masterkeysrd/tasksmith/internal/tui/plugin/tips"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/toast"
)

type PendingAuthorizationsWidgetProps struct {
	SessionID             string
	PendingAuthorizations []permissions.AuthorizationRequest
}

var PendingAuthorizationsWidget = kitex.FC("PendingAuthorizationsWidget", func(props PendingAuthorizationsWidgetProps) kitex.Node {
	client := tuiapi.UseClient()
	windClient := wind.UseClient()

	localDecisions, setLocalDecisions := kitex.UseState(make(map[string]permissions.AuthorizationDecision))
	previewOpen, setPreviewOpen := kitex.UseState(false)

	kitex.UseEffect(func() {
		setLocalDecisions(make(map[string]permissions.AuthorizationDecision))
	}, []any{props.SessionID, len(props.PendingAuthorizations)})

	submitBatch := func(decisions []permissions.AuthorizationDecision) {
		promise.New(func(ctx context.Context) (bool, error) {
			_, err := client.SubmitAuthorizationDecision(ctx, api.SubmitAuthorizationDecisionRequest{
				SessionID: props.SessionID,
				Decisions: decisions,
			})
			return err == nil, err
		}).Then(func(success bool) {
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: props.SessionID})
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: props.SessionID})
			windClient.InvalidateQueries(api.GetFileChangesRequest{SessionID: props.SessionID})
		}, func(err error) {
			log.Error(fmt.Sprintf("Failed to submit batch authorization decisions: %v", err))
		})
	}

	onDecision := func(dec permissions.AuthorizationDecision) {
		current := localDecisions()
		newDecisions := make(map[string]permissions.AuthorizationDecision)
		maps.Copy(newDecisions, current)
		newDecisions[dec.ToolCallID] = dec
		setLocalDecisions(newDecisions)

		allDecided := true
		var decisionList []permissions.AuthorizationDecision
		for _, req := range props.PendingAuthorizations {
			if d, ok := newDecisions[req.ToolCallID]; ok {
				decisionList = append(decisionList, d)
			} else {
				allDecided = false
			}
		}

		if allDecided && len(props.PendingAuthorizations) > 0 {
			setPreviewOpen(false)
			submitBatch(decisionList)
		}
	}

	var activeToolCallID string
	for _, req := range props.PendingAuthorizations {
		if _, decided := localDecisions()[req.ToolCallID]; !decided {
			activeToolCallID = req.ToolCallID
			break
		}
	}

	var children []kitex.Node
	for _, req := range props.PendingAuthorizations {
		var localDec *permissions.AuthorizationDecision
		if dec, decided := localDecisions()[req.ToolCallID]; decided {
			localDec = &dec
		}
		isActive := req.ToolCallID == activeToolCallID && localDec == nil
		node := AuthorizationWidget(AuthorizationWidgetProps{
			Request:             req,
			SessionID:           props.SessionID,
			IsActive:            isActive,
			OnDecision:          onDecision,
			LocalDecision:       localDec,
			ShowPreviewModal:    isActive && previewOpen(),
			SetShowPreviewModal: setPreviewOpen,
		})
		children = append(children, node)
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)).
			MinWidth(style.Percent(0)).
			Gap(1).
			PaddingTop(1).
			PaddingBottom(1),
	}, children...)
})

// ViewProps defines the properties for the Chat view.
type ViewProps struct {
	SessionID string
	ReadOnly  bool
}

// View is the main Chat view component.
var View func(props ViewProps) kitex.Node

func init() {
	View = kitex.FC("ChatView", renderChatView)
}

func renderChatView(props ViewProps) kitex.Node {
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
				mainAgentName = s.Settings.AgentName
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

	// Invalidate session list to capture title changes reactively when messages change
	kitex.UseEffect(func() {
		if title == "New Chat" || title == "" {
			windClient.InvalidateQueries(api.ListSessionsRequest{})
		}
	}, []any{msgsQuery.Data, title})

	// Trigger a toast notification if the background agent execution fails
	kitex.UseEffect(func() {
		if stateQuery.Data != nil && stateQuery.Data.Error != "" {
			toast.AddErrorMessage("Agent Run Failed", stateQuery.Data.Error)
		}
	}, []any{stateQuery.Data != nil, stateQuery.Data != nil && stateQuery.Data.Error != ""})

	activeTip := tips.Use(sending)

	// 3. Reactive state for input composer and submitting state
	inputValue, setInputValue := kitex.UseState("")
	submitting, setSubmitting := kitex.UseState(false)

	// Mode handling & Focus management
	m := mode.Use()
	isInsert := m == mode.Insert
	inputRef := kitex.UseRef[dom.Element](nil)
	outerRef := kitex.UseRef[dom.Element](nil)
	showFullOutputModal, setShowFullOutputModal := kitex.UseState(false)
	fullOutputTitle, setFullOutputTitle := kitex.UseState("")
	fullOutputContent, setFullOutputContent := kitex.UseState("")

	showResultPreview, setShowResultPreview := kitex.UseState(false)
	resultPreviewTitle, setResultPreviewTitle := kitex.UseState("")
	resultPreview, setResultPreview := kitex.UseState[preview.ToolPreview](nil)

	showSubagentModal, setShowSubagentModal := kitex.UseState(false)
	subagentSessionID, setSubagentSessionID := kitex.UseState("")
	subagentTitle, setSubagentTitle := kitex.UseState("")

	localEditingMessages, setLocalEditingMessages := kitex.UseState([]message.Message(nil))
	optimisticMessages, setOptimisticMessages := kitex.UseState([]message.Message(nil))
	scrollAnchorRef := kitex.UseRef[dom.Element](nil)

	handleRemoveQueuedMessage := func(messageID string) {
		promise.New(func(ctx context.Context) (bool, error) {
			_, err := client.RemoveQueuedMessage(ctx, api.RemoveQueuedMessageRequest{
				SessionID: props.SessionID,
				MessageID: messageID,
			})
			return err == nil, err
		}).Then(func(success bool) {
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: props.SessionID})
		}, func(err error) {
			log.Error(fmt.Sprintf("Failed to remove queued message: %v", err))
		})
	}

	handleEditQueuedMessage := func(messageID string) {
		promise.New(func(ctx context.Context) ([]message.Message, error) {
			resp, err := client.DequeueFrom(ctx, api.DequeueFromRequest{
				SessionID: props.SessionID,
				MessageID: messageID,
			})
			if err != nil {
				return nil, err
			}
			var list message.MessageList
			if len(resp.Messages) > 0 {
				rawArray := "[" + strings.Join(resp.Messages, ",") + "]"
				if err := json.Unmarshal([]byte(rawArray), &list); err != nil {
					return nil, err
				}
			}
			return list, nil
		}).Then(func(msgs []message.Message) {
			if len(msgs) == 0 {
				return
			}
			setLocalEditingMessages(msgs)
			var text strings.Builder
			for _, block := range msgs[0].GetContent() {
				if tb, ok := block.(*message.TextBlock); ok {
					text.WriteString(tb.Text)
				}
			}
			setInputValue(text.String())
			if inputRef.Current != nil {
				if doc := inputRef.Current.OwnerDocument(); doc != nil {
					doc.Focus(inputRef.Current)
				}
			}
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: props.SessionID})
		}, func(err error) {
			toast.AddErrorMessage("Failed to Edit", "Message might have already been processed by the agent.")
			log.Error(fmt.Sprintf("Failed to dequeue message for edit: %v", err))
		})
	}

	handleCancelQueuedEdit := func() {
		msgs := localEditingMessages()
		if len(msgs) == 0 {
			setInputValue("")
			return
		}
		promise.New(func(ctx context.Context) (bool, error) {
			var serialized []string
			for _, m := range msgs {
				list := message.MessageList{m}
				data, err := json.Marshal(list)
				if err != nil {
					return false, err
				}
				s := string(data)
				if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
					s = s[1 : len(s)-1]
				}
				serialized = append(serialized, s)
			}
			_, err := client.EnqueueMessages(ctx, api.EnqueueMessagesRequest{
				SessionID: props.SessionID,
				Messages:  serialized,
			})
			return err == nil, err
		}).Then(func(success bool) {
			setLocalEditingMessages(nil)
			setInputValue("")
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: props.SessionID})
		}, func(err error) {
			setLocalEditingMessages(nil)
			setInputValue("")
			log.Error(fmt.Sprintf("Failed to cancel queued edit: %v", err))
		})
	}

	handleClearQueue := func() {
		promise.New(func(ctx context.Context) (bool, error) {
			_, err := client.ClearQueue(ctx, api.ClearQueueRequest{
				SessionID: props.SessionID,
			})
			return err == nil, err
		}).Then(func(success bool) {
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: props.SessionID})
		}, func(err error) {
			log.Error(fmt.Sprintf("Failed to clear queue: %v", err))
		})
	}

	handleSendQueued := func() {
		promise.New(func(ctx context.Context) (bool, error) {
			_, err := client.SendQueued(ctx, api.SendQueuedRequest{
				SessionID: props.SessionID,
			})
			return err == nil, err
		}).Then(func(success bool) {
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: props.SessionID})
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: props.SessionID})
		}, func(err error) {
			log.Error(fmt.Sprintf("Failed to send queued messages: %v", err))
		})
	}

	onViewPreview := func(title string, p preview.ToolPreview) {
		setResultPreviewTitle(title)
		setResultPreview(p)
		setShowResultPreview(true)
	}

	onViewSubagent := func(title, subagentTaskID string) {
		setSubagentSessionID(subagentTaskID)
		setSubagentTitle(title)
		setShowSubagentModal(true)
	}

	openFullOutputModal := func(title, cachedPath string) {
		go func() {
			resp, err := client.GetCachedFile(context.Background(), api.GetCachedFileRequest{
				SessionID: props.SessionID,
				Path:      cachedPath,
			})
			if err == nil {
				setFullOutputTitle(title)
				setFullOutputContent(resp.Content)
				setShowFullOutputModal(true)
			}
		}()
	}

	showResolutionDialog, setShowResolutionDialog := kitex.UseState(false)

	focusSelf := func() {
		if outerRef.Current != nil {
			outerRef.Current.SetTabIndex(0)
			if doc := outerRef.Current.OwnerDocument(); doc != nil {
				doc.Focus(outerRef.Current)
			}
		}
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

	// Focus management: when insert mode is active, focus composer input.
	// When normal mode is active, focus the outer container so we can receive global hotkeys.
	kitex.UseEffect(func() {
		if isInsert {
			if IsFeedbackActive {
				return
			}
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

	// Autoscroll history to bottom if already at bottom
	historyRef := kitex.UseRef[dom.Element](nil)
	lastMaxScrollY := kitex.UseRef(0)
	isFirstRenderOfSession := kitex.UseRef(true)

	// Bind handlers to the persistent static controller
	if !props.ReadOnly {
		Controller.SendQueued = func() {
			if len(queuedMessages) > 0 && status == "idle" {
				handleSendQueued()
			}
		}
		Controller.ClearQueue = func() {
			if len(queuedMessages) > 0 && status == "idle" {
				handleClearQueue()
			}
		}
	} else {
		Controller.SendQueued = nil
		Controller.ClearQueue = nil
	}
	Controller.ScrollDown = func() {
		log.Info("ScrollDown invoked", log.Bool("historyRef_nil", historyRef.Current == nil))
		if historyRef.Current != nil {
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
			x, y := el.Scroll()
			log.Info("ScrollDown current position", log.Int("x", x), log.Int("y", y), log.Int("maxScrollY", maxScrollY))

			if y > maxScrollY {
				y = maxScrollY
			}
			targetY := min(y+3, maxScrollY)

			el.ScrollTo(x, targetY)
			newX, newY := el.Scroll()
			log.Info("ScrollDown new position", log.Int("x", newX), log.Int("y", newY))
		}
	}
	Controller.ScrollUp = func() {
		log.Info("ScrollUp invoked", log.Bool("historyRef_nil", historyRef.Current == nil))
		if historyRef.Current != nil {
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
			x, y := el.Scroll()
			log.Info("ScrollUp current position", log.Int("x", x), log.Int("y", y), log.Int("maxScrollY", maxScrollY))

			if y > maxScrollY {
				y = maxScrollY
			}

			targetY := max(y-3, 0)
			el.ScrollTo(x, targetY)
			newX, newY := el.Scroll()
			log.Info("ScrollUp new position", log.Int("x", newX), log.Int("y", newY))
		}
	}

	hasRunningTasks := stateQuery.Data != nil && len(stateQuery.Data.RunningTasks) > 0
	kitex.UseInterval(func() {
		if sending || hasRunningTasks {
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
		}
	}, 5000*time.Millisecond, []any{sending, hasRunningTasks, sessionID})

	kitex.UseEffect(func() {
		if !sending {
			windClient.InvalidateQueries(api.ListSessionsRequest{}) // Update sidebar session states (like metrics)
			windClient.InvalidateQueries(api.GetFileChangesRequest{SessionID: sessionID})
		}
	}, []any{sending, sessionID})

	// 5. Reactive state for tracking the last completed session's thinking time
	lastFinishedTime, setLastFinishedTime := kitex.UseState(-1) // -1 represents null/unset
	thinkingTime, setThinkingTime := kitex.UseState(0)

	// Reset thinking time and other transient states when switching sessions
	kitex.UseEffect(func() {
		setLastFinishedTime(-1)
		setThinkingTime(0)
		setInputValue("")
		setSubmitting(false)
		setShowFullOutputModal(false)
		isFirstRenderOfSession.Current = true
		lastMaxScrollY.Current = 0
		windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
	}, []any{sessionID})

	// Sync local thinking time with the backend's official timing
	kitex.UseEffect(func() {
		if stateQuery.Data != nil {
			setThinkingTime(int(stateQuery.Data.ThinkingDuration))
		}
	}, []any{stateQuery.Data})

	// Increment thinking time locally every 1 second when running
	kitex.UseInterval(func() {
		if sending {
			setThinkingTime(thinkingTime() + 1)
		}
	}, 1000*time.Millisecond, []any{sending})

	// Save the most recent non-zero thinkingTime while the agent is running
	prevSending := kitex.UseRef(false)
	kitex.UseEffect(func() {
		if sending {
			if thinkingTime() > 0 {
				setLastFinishedTime(thinkingTime())
			}
		} else if prevSending.Current && lastFinishedTime() == -1 {
			// Completed immediately (0 seconds)
			setLastFinishedTime(0)
		}
		prevSending.Current = sending
	}, []any{sending, thinkingTime()})

	// Calculate a simple integer key of the messages state to trigger the effect reactively.
	// Only calculate the length of the last message to avoid O(N) traversal of all message blocks on every render.
	messagesKey := len(messages)
	if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		for _, block := range lastMsg.GetContent() {
			if tb, ok := block.(*message.TextBlock); ok {
				messagesKey += len(tb.Text)
			} else if tb, ok := block.(*message.ThinkingBlock); ok {
				messagesKey += len(tb.Thinking)
			}
		}
	}

	// Refetch session state query when messages receive updates (e.g. subagent bubbles up prompts)
	kitex.UseEffect(func() {
		stateQuery.Refetch()
	}, []any{messagesKey})

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

		if isFirstRenderOfSession.Current {
			isFirstRenderOfSession.Current = false
			el.ScrollBy(0, 9999)
		} else if currentY >= lastMaxScrollY.Current-2 {
			rectParent, okParent := view.GetBoundingClientRect(el)
			var rectAnchor geom.Rect
			var okAnchor bool
			if scrollAnchorRef.Current != nil {
				rectAnchor, okAnchor = view.GetBoundingClientRect(scrollAnchorRef.Current)
			}
			if okParent && okAnchor {
				deltaY := (rectAnchor.Origin.Y - rectParent.Origin.Y) - (rectParent.Size.Height - 1)
				targetY := currentY + deltaY
				if targetY != currentY {
					el.ScrollTo(0, targetY)
				}
			} else {
				el.ScrollBy(0, 9999)
			}
		}

		lastMaxScrollY.Current = maxScrollY
	}, nil)

	sendMessage := func(text string, refs []resolver.Reference, force ...bool) {
		if text == "" || submitting() {
			return
		}

		if text == "/compact" {
			setInputValue("")
			setSubmitting(true)
			promise.New(func(ctx context.Context) (bool, error) {
				_, err := client.ForceCompaction(ctx, api.ForceCompactionRequest{
					SessionID: sessionID,
				})
				if err != nil {
					return false, err
				}
				return true, nil
			}).Then(func(success bool) {
				setSubmitting(false)
				active.SetStatusMessage("Compaction triggered successfully.")
				if active.InvalidateSessionMessages != nil {
					active.InvalidateSessionMessages(sessionID)
				}
			}, func(err error) {
				setSubmitting(false)
				active.SetStatusMessage("Compaction failed: " + err.Error())
				if active.InvalidateSessionMessages != nil {
					active.InvalidateSessionMessages(sessionID)
				}
			})
			return
		}

		isForced := len(force) > 0 && force[0]

		if status == "pending_auth" && !isForced {
			setInputValue(text)
			setShowResolutionDialog(true)
			return
		}

		setInputValue("")
		setSubmitting(true)

		optMsgID := "opt_" + fmt.Sprintf("%d", time.Now().UnixNano())
		optMsg := message.NewUserText(text)
		optMsg.SetID(optMsgID)
		setOptimisticMessages(append(optimisticMessages(), optMsg))

		promise.New(func(ctx context.Context) (bool, error) {
			var payloadRefs []resolver.ReferencePayload
			if len(refs) > 0 {
				payloadRefs = make([]resolver.ReferencePayload, len(refs))
				for i, r := range refs {
					payloadRefs[i] = r.ToPayload()
				}
			}
			_, err := client.SendMessage(ctx, api.SendMessageRequest{
				SessionID:  sessionID,
				Text:       text,
				References: payloadRefs,
			})
			if err != nil {
				return false, err
			}
			return true, nil
		}).Then(func(success bool) {
			filtered := make([]message.Message, 0)
			for _, m := range optimisticMessages() {
				if m.GetID() != optMsgID {
					filtered = append(filtered, m)
				}
			}
			setOptimisticMessages(filtered)

			// Re-enqueue any tail messages from a queued edit (msgs after the one being edited)
			if tail := localEditingMessages(); len(tail) > 1 {
				var serialized []string
				for _, m := range tail[1:] {
					list := message.MessageList{m}
					data, err := json.Marshal(list)
					if err == nil {
						s := string(data)
						if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
							s = s[1 : len(s)-1]
						}
						serialized = append(serialized, s)
					}
				}
				if len(serialized) > 0 {
					go func() {
						_, err := client.EnqueueMessages(context.Background(), api.EnqueueMessagesRequest{
							SessionID: sessionID,
							Messages:  serialized,
						})
						if err != nil {
							log.Error(fmt.Sprintf("Failed to re-enqueue tail messages after edit: %v", err))
						}
						windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
					}()
				}
			}
			setLocalEditingMessages(nil)

			setSubmitting(false)
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
			windClient.InvalidateQueries(api.GetFileChangesRequest{SessionID: sessionID})
		}, func(err error) {
			filtered := make([]message.Message, 0)
			for _, m := range optimisticMessages() {
				if m.GetID() != optMsgID {
					filtered = append(filtered, m)
				}
			}
			setOptimisticMessages(filtered)

			setSubmitting(false)
			log.Error(fmt.Sprintf("Failed to send message to backend: %v", err))
		})
	}

	// Resolve pending authorizations and optionally send the queued message
	handleAuthorizeAll := func() {
		promise.New(func(ctx context.Context) (bool, error) {
			var decisionList []permissions.AuthorizationDecision
			for _, d := range pendingAuthorizations {
				decisionList = append(decisionList, permissions.AuthorizationDecision{
					ToolCallID: d.ToolCallID,
					Approved:   true,
					Scope:      permissions.ScopeOnce,
				})
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
			setShowResolutionDialog(false)
			setSubmitting(false)
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
			windClient.InvalidateQueries(api.GetFileChangesRequest{SessionID: sessionID})
			if inputValue() != "" {
				sendMessage(inputValue(), nil, true)
			}
		}, func(err error) {
			setShowResolutionDialog(false)
			log.Error(fmt.Sprintf("Failed to submit authorization decisions: %v", err))
		})
	}

	handleRejectAll := func() {
		promise.New(func(ctx context.Context) (bool, error) {
			var decisionList []permissions.AuthorizationDecision
			for _, d := range pendingAuthorizations {
				decisionList = append(decisionList, permissions.AuthorizationDecision{
					ToolCallID: d.ToolCallID,
					Approved:   false,
				})
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
			setShowResolutionDialog(false)
			setSubmitting(false)
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
			windClient.InvalidateQueries(api.GetFileChangesRequest{SessionID: sessionID})
			if inputValue() != "" {
				sendMessage(inputValue(), nil, true)
			}
		}, func(err error) {
			setShowResolutionDialog(false)
			log.Error(fmt.Sprintf("Failed to submit authorization decisions: %v", err))
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
		MinWidth(style.Percent(0)).
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
		PaddingTop(0).
		PaddingBottom(0).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		AlignItems(style.AlignStretch).
		Background(bgDark)

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

	phase := "processing"
	if sending {
		if len(messages) > 0 {
			lastMsg := messages[len(messages)-1]
			if lastMsg.Role() == message.RoleAssistant {
				hasThinking := false
				hasText := false
				for _, block := range lastMsg.GetContent() {
					if tb, ok := block.(*message.ThinkingBlock); ok && len(tb.Thinking) > 0 {
						hasThinking = true
					} else if tb, ok := block.(*message.TextBlock); ok && len(tb.Text) > 0 {
						hasText = true
					}
				}
				if hasText {
					phase = "answering"
				} else if hasThinking {
					phase = "thinking"
				}
			}
		}
	}

	// Filter out pending authorizations that are already present/rendered inline in the message blocks
	var unrenderedAuths []permissions.AuthorizationRequest
	renderedIDs := make(map[string]bool)
	for _, msg := range messages {
		if asst, ok := msg.(*message.Assistant); ok {
			for _, block := range asst.Content {
				if tc, ok := block.(*message.ToolCall); ok {
					renderedIDs[tc.ID] = true
				}
			}
		}
	}
	for _, req := range pendingAuthorizations {
		if !renderedIDs[req.ToolCallID] {
			unrenderedAuths = append(unrenderedAuths, req)
		}
	}

	// Memoize the history scroller content to optimize typing performance
	historyContent := kitex.UseMemo(func() kitex.Node {
		return kitex.Fragment(
			renderBubbles(
				messages,
				toolResponses,
				mainAgentName,
				isGenerating,
				thinkingTime(),
				pendingAuthorizations,
				sessionID,
				openFullOutputModal,
				onViewPreview,
				onViewSubagent,
			)...,
		)
	}, []any{
		messages,
		toolResponses,
		isGenerating,
		thinkingTime(),
		pendingAuthorizations,
	})

	// Memoize the subagent view to avoid re-rendering the entire nested chat view
	// unless the active subagent session ID changes.
	subagentView := kitex.UseMemo(func() kitex.Node {
		if subagentSessionID() == "" {
			return nil
		}
		return View(ViewProps{
			SessionID: subagentSessionID(),
			ReadOnly:  true,
		})
	}, []any{subagentSessionID()})

	// Memoize the full output content box
	fullOutputContentNode := kitex.UseMemo(func() kitex.Node {
		if !showFullOutputModal() {
			return nil
		}
		var textCol color.Color
		if t != nil {
			textCol = t.Color.Text.Secondary
		}
		outputStyle := style.S().
			Width(style.Percent(100)).
			Height(style.Percent(100)).
			Foreground(textCol).
			WhiteSpace(style.WhiteSpacePreWrap).
			OverflowWrap(style.OverflowWrapBreakWord).
			OverflowY(style.OverflowAuto)

		return kitex.Box(kitex.BoxProps{Style: outputStyle},
			kitex.Text(fullOutputContent()),
		)
	}, []any{showFullOutputModal(), fullOutputContent()})

	outerProps := kitex.BoxProps{
		Style:      outerStyle,
		Ref:        outerRef,
		Attributes: map[string]string{"data-context": "chat"},
		OnClick: func(e event.Event) {
			if !isInsert {
				focusSelf()
			}
		},
	}

	return kitex.Box(outerProps,
		// Session Title Bar
		kitex.If(!props.ReadOnly, func() kitex.Node {
			return kitex.Box(kitex.BoxProps{Style: sessionTitleBarStyle},
				kitex.Text(title),
			)
		}),

		// Message History Section
		kitex.Box(kitex.BoxProps{
			Style:      messagesContainerStyle,
			Ref:        historyRef,
			Attributes: map[string]string{"data-id": "history-scroller"},
		},
			historyContent,

			// Standalone Pending Authorizations Widget for subagents
			kitex.If(len(unrenderedAuths) > 0, func() kitex.Node {
				return PendingAuthorizationsWidget(PendingAuthorizationsWidgetProps{
					SessionID:             sessionID,
					PendingAuthorizations: unrenderedAuths,
				})
			}),

			// Agent Status Widget
			kitex.If(sending || lastFinishedTime() >= 0, func() kitex.Node {
				return AgentStatus(AgentStatusProps{
					Sending:             sending,
					ThinkingTime:        thinkingTime(),
					LastFinishedTime:    lastFinishedTime(),
					RunPromptTokens:     runPromptTokens,
					RunCompletionTokens: runCompletionTokens,
					RunTotalTokens:      runTotalTokens,
					IsGenerating:        isGenerating,
					Phase:               phase,
					ActiveTip:           activeTip,
				})
			}),

			// Queued messages
			kitex.Fragment(
				renderQueuedBubbles(
					t,
					append(queuedMessages, optimisticMessages()...),
					handleEditQueuedMessage,
					handleRemoveQueuedMessage,
					onViewPreview,
				)...,
			),

			// Running Tasks Widget
			kitex.If(stateQuery.Data != nil && len(stateQuery.Data.RunningTasks) > 0, func() kitex.Node {
				return RunningTasksWidget(RunningTasksWidgetProps{
					Tasks: stateQuery.Data.RunningTasks,
				})
			}),

			// LSP Suggestion Widget
			kitex.If(len(pendingLspSuggestions) > 0, func() kitex.Node {
				return LspSuggestionWidget(LspSuggestionWidgetProps{
					Suggestions: pendingLspSuggestions,
					OnConfigure: handleConfigureLsp,
					OnDismiss:   handleDismissLsp,
				})
			}),

			// MCP Request Widget
			kitex.If(stateQuery.Data != nil && len(stateQuery.Data.PendingMcpRequests) > 0, func() kitex.Node {
				return McpRequestWidget(McpRequestWidgetProps{
					Requests: stateQuery.Data.PendingMcpRequests,
					OnResolve: func(reqID string, action string, code string, content map[string]any) {
						go func() {
							_, err := client.ResolveMcpRequest(context.Background(), api.ResolveMcpRequest{
								RequestID: reqID,
								Action:    action,
								Code:      code,
								Content:   content,
							})
							if err != nil {
								log.Error(fmt.Sprintf("Failed to resolve MCP request: %v", err))
							}
							stateQuery.Refetch()
						}()
					},
				})
			}),

			// Scroll Anchor Box — keeps auto-scroll focused on the stream
			kitex.Box(kitex.BoxProps{
				Ref:   scrollAnchorRef,
				Style: style.S().Height(style.Cells(0)),
			}),
		),

		// Composer Section
		kitex.If(!props.ReadOnly, func() kitex.Node {
			return kitex.Box(kitex.BoxProps{
				Style:      composerContainerStyle,
				Attributes: map[string]string{"data-context": "composer"},
			},
				// Queue actions row (above Composer)
				kitex.If((len(queuedMessages) > 0 || len(localEditingMessages()) > 0) && status == "idle", func() kitex.Node {
					isEditing := len(localEditingMessages()) > 0
					children := []kitex.Node{
						kitex.Text("Queue Actions:"),
					}
					if !isEditing {
						children = append(children,
							kitex.Span(kitex.SpanProps{
								Style: style.S().Foreground(t.Color.Surface.Success).Bold(true).Underline(true),
								OnClick: func(e event.Event) {
									handleSendQueued()
								},
							}, kitex.Text("s: Send Queued")),
							kitex.Span(kitex.SpanProps{
								Style: style.S().Foreground(t.Color.Text.Secondary).Underline(true),
								OnClick: func(e event.Event) {
									handleClearQueue()
								},
							}, kitex.Text("c: Clear Queue")),
						)
					} else {
						children = append(children,
							kitex.Span(kitex.SpanProps{
								Style: style.S().Foreground(t.Color.Text.Secondary).Underline(true),
								OnClick: func(e event.Event) {
									handleCancelQueuedEdit()
								},
							}, kitex.Text("x: Cancel Edit")),
						)
					}
					return kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							Gap(2).
							PaddingHorizontal(1).
							MarginBottom(1),
					}, children...)
				}),
				Composer(ComposerProps{
					Value:     inputValue(),
					Disabled:  submitting(),
					IsInsert:  isInsert,
					Ref:       inputRef,
					SessionID: sessionID,
					OnChange: func(val string) {
						setInputValue(val)
					},
					OnSubmit: func(text string, trackedRefs []resolver.Reference) {
						sendMessage(text, trackedRefs)
					},
				}),
				func() kitex.Node {
					var tipText string
					if status == "running" {
						tipText = "Press Ctrl+C to cancel execution"
					} else if isInsert {
						tipText = "Press <Enter> to send, <Esc> to exit"
					} else {
						tipText = "Press i to write, J/K to scroll, s to send queue"
					}

					var textStyle style.Style
					if t != nil {
						textStyle = style.S().Foreground(t.Color.Text.Tertiary)
					}

					return kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							AlignItems(style.AlignCenter).
							PaddingVertical(0).
							PaddingHorizontal(1).
							Height(style.Cells(1)).
							Background(bgDark),
					},
						kitex.Span(kitex.SpanProps{Style: textStyle}, kitex.Text(tipText)),
					)
				}(),
			)
		}),

		// Resolution Dialog for Pending Authorizations
		kitex.If(showResolutionDialog(), func() kitex.Node {
			return components.ConfirmDialog(components.ConfirmDialogProps{
				Message:        "There are pending tool authorizations. How would you like to proceed?",
				ConfirmLabel:   "Authorize All",
				ConfirmColor:   components.ButtonSuccess,
				OnConfirm:      handleAuthorizeAll,
				SecondaryLabel: "Reject All",
				SecondaryColor: components.ButtonError,
				OnSecondary:    handleRejectAll,
				CancelLabel:    "Cancel",
				OnCancel:       func() { setShowResolutionDialog(false) },
			})
		}),

		// Modal Section for Full Output View
		kitex.If(!props.ReadOnly, func() kitex.Node {
			return components.Modal(components.ModalProps{
				IsOpen: showFullOutputModal(),
				Title:  kitex.Text(fullOutputTitle()),
				OnClose: func() {
					setShowFullOutputModal(false)
					setFullOutputContent("")
					setFullOutputTitle("")
				},
			},
				fullOutputContentNode,
			)
		}),

		// Modal Section for Generic Result Preview
		kitex.If(!props.ReadOnly, func() kitex.Node {
			return GenericPreviewModal(GenericPreviewModalProps{
				IsOpen:  showResultPreview(),
				Title:   resultPreviewTitle(),
				Preview: resultPreview(),
				OnClose: func() {
					setShowResultPreview(false)
					setResultPreview(nil)
					setResultPreviewTitle("")
				},
			})
		}),

		// Modal Section for Subagent Peek View
		kitex.If(!props.ReadOnly, func() kitex.Node {
			return components.Modal(components.ModalProps{
				IsOpen: showSubagentModal(),
				Title:  kitex.Text(subagentTitle()),
				OnClose: func() {
					setShowSubagentModal(false)
					setSubagentSessionID("")
					setSubagentTitle("")
				},
			},
				kitex.If(showSubagentModal(), func() kitex.Node {
					modalContentStyle := style.S().
						Width(style.Percent(100)).
						Height(style.Percent(100)).
						Overflow(style.OverflowHidden)

					return kitex.Box(kitex.BoxProps{Style: modalContentStyle},
						subagentView,
					)
				}),
			)
		}),
	)
}

func getBubbleRole(role message.Role, msg message.Message) message.Role {
	if msg != nil {
		meta := msg.GetMetadata()
		if meta != nil {
			if name, ok := meta["agent_name"].(string); ok && name != "" {
				return message.RoleAssistant
			}
		}
	}

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

func renderBubbles(
	messages message.MessageList,
	toolResponses map[string]*message.Tool,
	mainAgentName string,
	isGenerating bool,
	liveThinkingTime int,
	pendingAuthorizations []permissions.AuthorizationRequest,
	sessionID string,
	onViewFullOutput func(title, cachedPath string),
	onViewPreview func(title string, p preview.ToolPreview),
	onViewSubagent func(title, subagentTaskID string),
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
			groupPendingAuths := []permissions.AuthorizationRequest{}
			if len(messages) > 0 && currentGroup[len(currentGroup)-1] == messages[len(messages)-1] {
				groupPendingAuths = pendingAuthorizations
			}
			nodes = append(nodes, BubbleGroup(BubbleGroupProps{
				Key:                   fmt.Sprintf("group-%s-%s", currentGroupRole, currentGroup[0].GetID()),
				Role:                  currentGroupRole,
				Msgs:                  currentGroup,
				ToolResponses:         toolResponses,
				MainAgentName:         mainAgentName,
				IsGenerating:          groupIsGenerating,
				LiveThinkingTime:      liveThinkingTime,
				PendingAuthorizations: groupPendingAuths,
				SessionID:             sessionID,
				OnViewFullOutput:      onViewFullOutput,
				OnViewPreview:         onViewPreview,
				OnViewSubagent:        onViewSubagent,
			}))
		}
	}

	for _, msg := range messages {
		isSys := isSystemNotification(msg)
		role := msg.Role()
		groupRole := getBubbleRole(role, msg)

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

func renderQueuedBubbles(
	_ *theme.Scheme,
	queuedMessages message.MessageList,
	onEdit func(string),
	onRemove func(string),
	onViewPreview func(title string, p preview.ToolPreview),
) []kitex.Node {
	if len(queuedMessages) == 0 {
		return nil
	}

	var nodes []kitex.Node

	for _, msg := range queuedMessages {
		if meta := msg.GetMetadata(); meta != nil {
			if isSys, ok := meta["is_system_notification"]; ok {
				if b, ok := isSys.(bool); ok && b {
					continue
				}
			}
		}

		msgID := msg.GetID()

		nodes = append(nodes, QueuedBubble(QueuedBubbleProps{
			Key:           "queued-" + msgID,
			ID:            msgID,
			Content:       msg.GetContent(),
			OnEdit:        onEdit,
			OnRemove:      onRemove,
			OnViewPreview: onViewPreview,
		}))
	}

	return nodes
}
