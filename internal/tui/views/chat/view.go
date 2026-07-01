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
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/preview"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	"github.com/masterkeysrd/tasksmith/internal/tui/plugin/tips"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/toast"
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
	inputRef := kitex.CreateRef[dom.Element]()
	outerRef := kitex.CreateRef[dom.Element]()

	// Authorization choices state
	selectedScopeIndex, setSelectedScopeIndex := kitex.UseState(1) // Default to Session (1)
	currentPageIndex, setCurrentPageIndex := kitex.UseState(0)
	focusedItem, setFocusedItem := kitex.UseState(FocusItemSession)
	selectedOptions, setSelectedOptions := kitex.UseState(map[string]int{})
	selectedDirs, setSelectedDirs := kitex.UseState(map[string]int{})
	showPreviewModal, setShowPreviewModal := kitex.UseState(false)
	showFullOutputModal, setShowFullOutputModal := kitex.UseState(false)
	fullOutputTitle, setFullOutputTitle := kitex.UseState("")
	fullOutputContent, setFullOutputContent := kitex.UseState("")

	showResultPreview, setShowResultPreview := kitex.UseState(false)
	resultPreviewTitle, setResultPreviewTitle := kitex.UseState("")
	resultPreview, setResultPreview := kitex.UseState[preview.ToolPreview](nil)

	localEditingMessages, setLocalEditingMessages := kitex.UseState([]message.Message(nil))
	optimisticMessages, setOptimisticMessages := kitex.UseState([]message.Message(nil))
	scrollAnchorRef := kitex.CreateRef[dom.Element]()

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

	currentPendingIndex, setCurrentPendingIndex := kitex.UseState(0)
	localDecisions, setLocalDecisions := kitex.UseState(map[string]permissions.AuthorizationDecision{})
	isProvidingFeedback, setIsProvidingFeedback := kitex.UseState(false)
	feedbackText, setFeedbackText := kitex.UseState("")
	showResolutionDialog, setShowResolutionDialog := kitex.UseState(false)
	showCancelConfirmDialog, setShowCancelConfirmDialog := kitex.UseState(false)

	focusSelf := func() {
		if outerRef.Current != nil {
			outerRef.Current.SetTabIndex(0)
			if doc := outerRef.Current.OwnerDocument(); doc != nil {
				doc.Focus(outerRef.Current)
			}
		}
	}

	handleSelectVertical := func(item FocusItem) {
		setFocusedItem(item)
		mode.Set(mode.Normal)
		focusSelf()
	}

	var pendingAuthorizations []permissions.AuthorizationRequest
	if stateQuery.Data != nil {
		pendingAuthorizations = stateQuery.Data.PendingAuthorizations
	}

	handleSelectScope := func(idx int) {
		setSelectedScopeIndex(idx)
		switch idx {
		case 0:
			setFocusedItem(FocusItemOnce)
		case 1:
			setFocusedItem(FocusItemSession)
		case 2:
			setFocusedItem(FocusItemWorkspace)
		case 3:
			setFocusedItem(FocusItemGlobal)
		}
		mode.Set(mode.Normal)
		focusSelf()
	}

	handleSelectOption := func(idx int) {
		currIdx := currentPendingIndex()
		if currIdx < len(pendingAuthorizations) {
			req := pendingAuthorizations[currIdx]
			if len(req.GrantRequests) > currentPageIndex() {
				gr := req.GrantRequests[currentPageIndex()]
				newOpts := make(map[string]int)
				maps.Copy(newOpts, selectedOptions())
				newOpts[gr.ID] = idx
				setSelectedOptions(newOpts)
				switch selectedScopeIndex() {
				case 1:
					setFocusedItem(FocusItemSessionCmd)
				case 2:
					setFocusedItem(FocusItemWorkspaceCmd)
				case 3:
					setFocusedItem(FocusItemGlobalCmd)
				}
				mode.Set(mode.Normal)
				focusSelf()
			}
		}
	}

	handleSelectDir := func(idx int) {
		currIdx := currentPendingIndex()
		if currIdx < len(pendingAuthorizations) {
			req := pendingAuthorizations[currIdx]
			if len(req.GrantRequests) > currentPageIndex() {
				gr := req.GrantRequests[currentPageIndex()]
				newDirs := make(map[string]int)
				maps.Copy(newDirs, selectedDirs())
				newDirs[gr.ID] = idx
				setSelectedDirs(newDirs)
				setFocusedItem(FocusItemDirectory)
				mode.Set(mode.Normal)
				focusSelf()
			}
		}
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
		defaultIdx := 1
		defaultFocusedItem := FocusItemSession
		if len(pendingAuthorizations) > 0 {
			defaultIdx = getDefaultScopeIndex(pendingAuthorizations[0], 0)
			allowed := getAllowedScopes(pendingAuthorizations[0], 0)
			if len(allowed) > 0 {
				switch allowed[defaultIdx] {
				case permissions.ScopeOnce:
					defaultFocusedItem = FocusItemOnce
				case permissions.ScopeSession:
					defaultFocusedItem = FocusItemSession
				case permissions.ScopeWorkspace:
					defaultFocusedItem = FocusItemWorkspace
				case permissions.ScopeGlobal:
					defaultFocusedItem = FocusItemGlobal
				}
			}
		}
		setSelectedScopeIndex(defaultIdx)
		setFocusedItem(defaultFocusedItem)
		setCurrentPageIndex(0)
		setSelectedOptions(map[string]int{})
		setSelectedDirs(map[string]int{})
		setShowPreviewModal(false)
		setCurrentPendingIndex(0)
		setLocalDecisions(map[string]permissions.AuthorizationDecision{})
		if len(pendingAuthorizations) > 0 {
			mode.Set(mode.Normal)
		}
	}, []any{len(pendingAuthorizations)})

	recordDecision := func(toolCallID string, approved bool, scope permissions.PermissionScope, grantDecisions []permissions.GrantDecision, reason string) {
		dec := permissions.AuthorizationDecision{
			ToolCallID:     toolCallID,
			Approved:       approved,
			Scope:          scope,
			GrantDecisions: grantDecisions,
			Reason:         reason,
		}
		setIsProvidingFeedback(false)
		setFeedbackText("")

		newDecisions := make(map[string]permissions.AuthorizationDecision)
		maps.Copy(newDecisions, localDecisions())
		newDecisions[toolCallID] = dec
		setLocalDecisions(newDecisions)

		nextIdx := currentPendingIndex() + 1
		if nextIdx < len(pendingAuthorizations) {
			setCurrentPendingIndex(nextIdx)
			nextReq := pendingAuthorizations[nextIdx]
			defaultIdx := getDefaultScopeIndex(nextReq, 0)
			defaultFocusedItem := FocusItemSession
			allowed := getAllowedScopes(nextReq, 0)
			if len(allowed) > 0 {
				switch allowed[defaultIdx] {
				case permissions.ScopeOnce:
					defaultFocusedItem = FocusItemOnce
				case permissions.ScopeSession:
					defaultFocusedItem = FocusItemSession
				case permissions.ScopeWorkspace:
					defaultFocusedItem = FocusItemWorkspace
				case permissions.ScopeGlobal:
					defaultFocusedItem = FocusItemGlobal
				}
			}
			setSelectedScopeIndex(defaultIdx)
			setFocusedItem(defaultFocusedItem)
			setCurrentPageIndex(0)
			setSelectedOptions(map[string]int{})
			setSelectedDirs(map[string]int{})
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
				windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
				windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
				windClient.InvalidateQueries(api.GetFileChangesRequest{SessionID: sessionID})
			}, func(err error) {
				setSubmitting(false)
				log.Error(fmt.Sprintf("Failed to submit authorization decisions: %v", err))
			})
		}
	}

	// Focus management: when insert mode is active, focus composer input.
	// When normal mode is active, focus the outer container so we can receive global hotkeys.
	// If feedback mode is active, block focus theft and let the feedback widget handle its own focus.
	kitex.UseEffect(func() {
		if isProvidingFeedback() {
			return
		}

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
	}, []any{isInsert, isProvidingFeedback()})

	// Refs to bridge latest react states to the single-registration document listener
	pendingAuthsRef := kitex.UseRef[[]permissions.AuthorizationRequest](nil)
	pendingAuthsRef.Current = pendingAuthorizations

	selectedScopeIndexRef := kitex.UseRef(1)
	selectedScopeIndexRef.Current = selectedScopeIndex()

	currentPageIndexRef := kitex.UseRef(0)
	currentPageIndexRef.Current = currentPageIndex()

	focusedItemRef := kitex.UseRef(FocusItemSession)
	focusedItemRef.Current = focusedItem()

	selectedOptionsRef := kitex.UseRef[map[string]int](nil)
	selectedOptionsRef.Current = selectedOptions()

	selectedDirsRef := kitex.UseRef[map[string]int](nil)
	selectedDirsRef.Current = selectedDirs()

	showPreviewModalRef := kitex.UseRef(false)
	showPreviewModalRef.Current = showPreviewModal()

	modeRef := kitex.UseRef(mode.Normal)
	modeRef.Current = m

	queuedMessagesRef := kitex.UseRef[message.MessageList](nil)
	queuedMessagesRef.Current = queuedMessages

	statusRef := kitex.UseRef("")
	statusRef.Current = status

	currentPendingIndexRef := kitex.UseRef(0)
	currentPendingIndexRef.Current = currentPendingIndex()

	isProvidingFeedbackRef := kitex.UseRef(false)
	isProvidingFeedbackRef.Current = isProvidingFeedback()

	feedbackTextRef := kitex.UseRef("")
	feedbackTextRef.Current = feedbackText()

	showCancelConfirmDialogRef := kitex.UseRef(false)
	showCancelConfirmDialogRef.Current = showCancelConfirmDialog()

	recordDecisionRef := kitex.UseRef[func(string, bool, permissions.PermissionScope, []permissions.GrantDecision, string)](nil)
	recordDecisionRef.Current = recordDecision

	handleApprove := func() {
		currIdx := currentPendingIndexRef.Current
		if currIdx >= len(pendingAuthsRef.Current) {
			return
		}
		req := pendingAuthsRef.Current[currIdx]

		scope := permissions.ScopeOnce
		allowed := getAllowedScopes(req, 0)
		idx := selectedScopeIndexRef.Current
		if idx >= 0 && idx < len(allowed) {
			scope = allowed[idx]
		}

		var decisions []permissions.GrantDecision
		for _, gr := range req.GrantRequests {
			optIdx := selectedOptionsRef.Current[gr.ID]
			if optIdx < 0 || optIdx >= len(gr.Options) {
				optIdx = 0
			}
			target := "*"
			if len(gr.Options) > 0 {
				target = gr.Options[optIdx].Target
			}

			dirIdx := selectedDirsRef.Current[gr.ID]
			if dirIdx < 0 || dirIdx >= len(gr.DirectoryOptions) {
				dirIdx = 0
			}
			allowedDir := "*"
			if len(gr.DirectoryOptions) > 0 {
				allowedDir = gr.DirectoryOptions[dirIdx].Target
			}

			decisions = append(decisions, permissions.GrantDecision{
				RequestID:        gr.ID,
				SelectedTarget:   target,
				AllowedDirectory: allowedDir,
				Scope:            scope,
			})
		}

		if recordDecisionRef.Current != nil {
			recordDecisionRef.Current(req.ToolCallID, true, scope, decisions, "")
		}
	}

	handleDeny := func() {
		currIdx := currentPendingIndexRef.Current
		if currIdx >= len(pendingAuthsRef.Current) {
			return
		}
		req := pendingAuthsRef.Current[currIdx]
		if recordDecisionRef.Current != nil {
			recordDecisionRef.Current(req.ToolCallID, false, permissions.ScopeOnce, nil, "")
		}
	}

	handleDenyWithFeedback := func(reason string) {
		currIdx := currentPendingIndexRef.Current
		if currIdx >= len(pendingAuthsRef.Current) {
			return
		}
		req := pendingAuthsRef.Current[currIdx]
		if recordDecisionRef.Current != nil {
			recordDecisionRef.Current(req.ToolCallID, false, permissions.ScopeOnce, nil, reason)
		}
		setIsProvidingFeedback(false)
		setFeedbackText("")
		mode.Set(mode.Normal)
	}

	handleStartFeedback := func() {
		setIsProvidingFeedback(true)
		setFeedbackText("")
		mode.Set(mode.Insert)
	}

	handleCancelFeedback := func() {
		setIsProvidingFeedback(false)
		setFeedbackText("")
		mode.Set(mode.Normal)
	}

	handleHardCancel := func() {
		promise.New(func(ctx context.Context) (bool, error) {
			var decisionList []permissions.AuthorizationDecision
			for _, d := range pendingAuthorizations {
				decisionList = append(decisionList, permissions.AuthorizationDecision{
					ToolCallID:      d.ToolCallID,
					Approved:        false,
					CancelExecution: true,
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
			setShowCancelConfirmDialog(false)
			setShowPreviewModal(false)
			setSubmitting(false)
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
		}, func(err error) {
			setShowCancelConfirmDialog(false)
			setSubmitting(false)
			log.Error(fmt.Sprintf("Failed to submit cancellation decisions: %v", err))
		})
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
			if modeRef.Current != mode.Normal {
				return
			}

			ke, ok := e.(*event.KeyEvent)
			if !ok {
				return
			}

			// Handle queue keys if queue is non-empty and idle
			if len(queuedMessagesRef.Current) > 0 && statusRef.Current == "idle" {
				if ke.Text == "s" {
					e.PreventDefault()
					e.StopPropagation()
					handleSendQueued()
					return
				}
				if ke.Text == "c" {
					e.PreventDefault()
					e.StopPropagation()
					handleClearQueue()
					return
				}
			}

			if len(pendingAuthsRef.Current) == 0 {
				return
			}

			if showCancelConfirmDialogRef.Current {
				if ke.Code == key.KeyEnter || ke.Text == "\r" || ke.Text == "\n" {
					e.PreventDefault()
					e.StopPropagation()
					handleHardCancel()
					return
				}
				if ke.Code == key.KeyEscape || ke.Text == "q" {
					e.PreventDefault()
					e.StopPropagation()
					setShowCancelConfirmDialog(false)
					return
				}
				e.PreventDefault()
				e.StopPropagation()
				return
			}

			if isProvidingFeedbackRef.Current {
				return
			}

			currIdx := currentPendingIndexRef.Current
			if currIdx >= len(pendingAuthsRef.Current) {
				return
			}

			req := pendingAuthsRef.Current[currIdx]

			getVisibleItems := func(scopeIdx int, req permissions.AuthorizationRequest, page int) []FocusItem {
				var items []FocusItem
				allowed := getAllowedScopes(req, page)

				for idx, scope := range allowed {
					switch scope {
					case permissions.ScopeOnce:
						items = append(items, FocusItemOnce)
					case permissions.ScopeSession:
						items = append(items, FocusItemSession)
						if len(req.GrantRequests) > page {
							gr := req.GrantRequests[page]
							if scopeIdx == idx && len(gr.Options) > 0 {
								items = append(items, FocusItemSessionCmd)
							}
						}
					case permissions.ScopeWorkspace:
						items = append(items, FocusItemWorkspace)
						if len(req.GrantRequests) > page {
							gr := req.GrantRequests[page]
							if scopeIdx == idx && len(gr.Options) > 0 {
								items = append(items, FocusItemWorkspaceCmd)
							}
						}
					case permissions.ScopeGlobal:
						items = append(items, FocusItemGlobal)
						if len(req.GrantRequests) > page {
							gr := req.GrantRequests[page]
							if scopeIdx == idx && len(gr.Options) > 0 {
								items = append(items, FocusItemGlobalCmd)
							}
						}
					}
				}

				if len(req.GrantRequests) > page {
					gr := req.GrantRequests[page]
					onceIdx := -1
					for idx, scope := range allowed {
						if scope == permissions.ScopeOnce {
							onceIdx = idx
							break
						}
					}
					if scopeIdx != onceIdx && len(gr.DirectoryOptions) > 0 {
						items = append(items, FocusItemDirectory)
					}
				}
				return items
			}

			// j/k vertical row navigation
			currPage := currentPageIndexRef.Current
			if ke.Text == "j" || ke.Code == key.KeyDown {
				e.PreventDefault()
				e.StopPropagation()
				items := getVisibleItems(selectedScopeIndexRef.Current, req, currPage)
				currItem := focusedItemRef.Current
				currIdx := 0
				for idx, it := range items {
					if it == currItem {
						currIdx = idx
						break
					}
				}
				newIdx := (currIdx + 1) % len(items)
				newItem := items[newIdx]
				setFocusedItem(newItem)

				var targetScope permissions.PermissionScope
				switch newItem {
				case FocusItemOnce:
					targetScope = permissions.ScopeOnce
				case FocusItemSession:
					targetScope = permissions.ScopeSession
				case FocusItemWorkspace:
					targetScope = permissions.ScopeWorkspace
				case FocusItemGlobal:
					targetScope = permissions.ScopeGlobal
				}
				if targetScope != "" {
					targetScopeIdx := getScopeIndex(req, currPage, targetScope)
					if targetScopeIdx != -1 {
						setSelectedScopeIndex(targetScopeIdx)
					}
				}
				return
			}
			if ke.Text == "k" || ke.Code == key.KeyUp {
				e.PreventDefault()
				e.StopPropagation()
				items := getVisibleItems(selectedScopeIndexRef.Current, req, currPage)
				currItem := focusedItemRef.Current
				currIdx := 0
				for idx, it := range items {
					if it == currItem {
						currIdx = idx
						break
					}
				}
				newIdx := (currIdx - 1 + len(items)) % len(items)
				newItem := items[newIdx]
				setFocusedItem(newItem)

				var targetScope permissions.PermissionScope
				switch newItem {
				case FocusItemOnce:
					targetScope = permissions.ScopeOnce
				case FocusItemSession:
					targetScope = permissions.ScopeSession
				case FocusItemWorkspace:
					targetScope = permissions.ScopeWorkspace
				case FocusItemGlobal:
					targetScope = permissions.ScopeGlobal
				}
				if targetScope != "" {
					targetScopeIdx := getScopeIndex(req, currPage, targetScope)
					if targetScopeIdx != -1 {
						setSelectedScopeIndex(targetScopeIdx)
					}
				}
				return
			}

			// h/l horizontal option navigation based on focused row
			if currPage < len(req.GrantRequests) {
				currReq := req.GrantRequests[currPage]
				currItem := focusedItemRef.Current

				switch currItem {
				case FocusItemSessionCmd, FocusItemWorkspaceCmd, FocusItemGlobalCmd:
					optsCount := len(currReq.Options)
					if optsCount > 1 {
						currentOptIdx := selectedOptionsRef.Current[currReq.ID]
						if ke.Text == "h" || ke.Code == key.KeyLeft {
							e.PreventDefault()
							e.StopPropagation()
							newOpts := make(map[string]int)
							maps.Copy(newOpts, selectedOptionsRef.Current)
							newOpts[currReq.ID] = (currentOptIdx - 1 + optsCount) % optsCount
							setSelectedOptions(newOpts)
							return
						}
						if ke.Text == "l" || ke.Code == key.KeyRight {
							e.PreventDefault()
							e.StopPropagation()
							newOpts := make(map[string]int)
							maps.Copy(newOpts, selectedOptionsRef.Current)
							newOpts[currReq.ID] = (currentOptIdx + 1) % optsCount
							setSelectedOptions(newOpts)
							return
						}
					}
				case FocusItemDirectory:
					dirOptsCount := len(currReq.DirectoryOptions)
					if dirOptsCount > 1 {
						currentDirIdx := selectedDirsRef.Current[currReq.ID]
						if ke.Text == "h" || ke.Code == key.KeyLeft {
							e.PreventDefault()
							e.StopPropagation()
							newDirs := make(map[string]int)
							maps.Copy(newDirs, selectedDirsRef.Current)
							newDirs[currReq.ID] = (currentDirIdx - 1 + dirOptsCount) % dirOptsCount
							setSelectedDirs(newDirs)
							return
						}
						if ke.Text == "l" || ke.Code == key.KeyRight {
							e.PreventDefault()
							e.StopPropagation()
							newDirs := make(map[string]int)
							maps.Copy(newDirs, selectedDirsRef.Current)
							newDirs[currReq.ID] = (currentDirIdx + 1) % dirOptsCount
							setSelectedDirs(newDirs)
							return
						}
					}
				}
			}

			// Enter next/allow
			if ke.Code == key.KeyEnter || ke.Text == "\r" || ke.Text == "\n" {
				e.PreventDefault()
				e.StopPropagation()

				currPage := currentPageIndexRef.Current
				totalPages := len(req.GrantRequests)
				if totalPages == 0 {
					totalPages = 1
				}

				if currPage < totalPages-1 {
					setCurrentPageIndex(currPage + 1)
					setFocusedItem(FocusItemSession)
				} else {
					handleApprove()
				}
				return
			}

			// Prev/Back b/Backspace
			if ke.Text == "b" || ke.Code == key.KeyBackspace {
				currPage := currentPageIndexRef.Current
				if currPage > 0 {
					e.PreventDefault()
					e.StopPropagation()
					setCurrentPageIndex(currPage - 1)
					setFocusedItem(FocusItemSession)
					return
				}
			}

			if ke.Text == "x" || ke.Code == key.KeyEscape {
				e.PreventDefault()
				e.StopPropagation()
				setShowCancelConfirmDialog(true)
				return
			}
			if ke.Text == "q" {
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
			if ke.Text == "D" {
				e.PreventDefault()
				e.StopPropagation()
				handleStartFeedback()
				return
			}
			if ke.Text == "p" || ke.Text == "P" {
				if req.Preview != nil {
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

	// Autoscroll history to bottom if already at bottom
	historyRef := kitex.UseRef[dom.Element](nil)
	lastMaxScrollY := kitex.UseRef(0)

	// 5. Reactive state for tracking the last completed session's thinking time
	lastFinishedTime, setLastFinishedTime := kitex.UseState(-1) // -1 represents null/unset
	thinkingTime, setThinkingTime := kitex.UseState(0)
	spinnerFrame, setSpinnerFrame := kitex.UseState(0)

	// Reset thinking time and other transient states when switching sessions
	kitex.UseEffect(func() {
		setLastFinishedTime(-1)
		setThinkingTime(0)
		setInputValue("")
		setSubmitting(false)
		setShowFullOutputModal(false)
		setSelectedScopeIndex(1) // Default to Session (1)
		setFocusedItem(FocusItemSession)
		setCurrentPageIndex(0)
		setSelectedOptions(map[string]int{})
		setSelectedDirs(map[string]int{})
		setShowPreviewModal(false)
		setCurrentPendingIndex(0)
		setLocalDecisions(map[string]permissions.AuthorizationDecision{})
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
		// we scroll to either the scroll anchor or the absolute bottom.
		if currentY >= lastMaxScrollY.Current-2 {
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
				el.ScrollTo(0, 99999)
			}
		}

		lastMaxScrollY.Current = maxScrollY
	}, []any{messagesKey})

	sendMessage := func(text string, force ...bool) {
		if text == "" || submitting() {
			return
		}

		if strings.HasPrefix(text, "/mode ") {
			modeStr := strings.TrimSpace(strings.TrimPrefix(text, "/mode "))
			var mode permissions.PermissionMode
			switch modeStr {
			case "auto":
				mode = permissions.ModeAuto
			case "default":
				mode = permissions.ModeDefault
			case "strict":
				mode = permissions.ModeStrict
			default:
				toast.AddErrorMessage("Invalid Mode", fmt.Sprintf("Unknown permission mode %q. Use 'auto', 'default', or 'strict'.", modeStr))
				setInputValue("")
				return
			}

			setInputValue("")
			setSubmitting(true)
			promise.New(func(ctx context.Context) (bool, error) {
				_, err := client.SetPermissionMode(ctx, api.SetPermissionModeRequest{
					SessionID: sessionID,
					Mode:      mode,
					Scope:     permissions.ScopeSession,
				})
				if err != nil {
					return false, err
				}
				return true, nil
			}).Then(func(success bool) {
				setSubmitting(false)
				windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
			}, func(err error) {
				setSubmitting(false)
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
				sendMessage(inputValue(), true)
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
				sendMessage(inputValue(), true)
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
		PaddingBottom(1).
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
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

	outerProps := kitex.BoxProps{
		Style: outerStyle,
		Ref:   outerRef,
		OnClick: func(e event.Event) {
			if !isInsert {
				focusSelf()
			}
		},
	}

	return kitex.Box(outerProps,
		// Session Title Bar
		kitex.Box(kitex.BoxProps{Style: sessionTitleBarStyle},
			kitex.Text(title),
		),

		// Message History Section
		kitex.Box(kitex.BoxProps{Style: messagesContainerStyle, Ref: historyRef},
			kitex.Fragment(
				renderBubbles(
					messages,
					toolResponses,
					currentDots,
					oneDotCurrentDots,
					mainAgentName,
					isGenerating,
					thinkingTime(),
					pendingAuthorizations,
					currentPageIndex(),
					focusedItem(),
					selectedScopeIndex(),
					selectedOptions(),
					selectedDirs(),
					func() { setShowPreviewModal(true) },
					currentPendingIndex(),
					isInsert,
					localDecisions(),
					submitting(),
					handleSelectVertical,
					handleSelectScope,
					handleSelectOption,
					handleSelectDir,
					handleApprove,
					handleDeny,
					handleHardCancel,
					openFullOutputModal,
					onViewPreview,
					isProvidingFeedback(),
					feedbackText(),
					func(val string) { setFeedbackText(val) },
					handleDenyWithFeedback,
					handleCancelFeedback,
					handleStartFeedback,
				)...,
			),

			// Agent Status Widget
			kitex.If(sending || lastFinishedTime() >= 0, func() kitex.Node {
				return AgentStatus(AgentStatusProps{
					Sending:             sending,
					ThinkingTime:        thinkingTime(),
					LastFinishedTime:    lastFinishedTime(),
					CurrentDots:         currentDots,
					RunPromptTokens:     runPromptTokens,
					RunCompletionTokens: runCompletionTokens,
					RunTotalTokens:      runTotalTokens,
					IsGenerating:        isGenerating,
					Phase:               phase,
					ActiveTip:           activeTip,
				})
			}),

			// Scroll Anchor Box — keeps auto-scroll focused on the stream, above queued messages
			kitex.Box(kitex.BoxProps{
				Ref:   scrollAnchorRef,
				Style: style.S().Height(style.Cells(0)),
			}),

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

			// Queued messages
			kitex.Fragment(
				renderQueuedBubbles(
					t,
					append(queuedMessages, optimisticMessages()...),
					handleEditQueuedMessage,
					handleRemoveQueuedMessage,
				)...,
			),
		),

		// Composer Section
		kitex.Box(kitex.BoxProps{Style: composerContainerStyle},
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
		AuthorizationPreviewModal(AuthorizationPreviewModalProps{
			IsOpen:              showPreviewModal(),
			PendingRequests:     pendingAuthorizations,
			CurrentPendingIndex: currentPendingIndex(),
			CurrentPageIndex:    currentPageIndex(),
			FocusedItem:         focusedItem(),
			SelectedScopeIndex:  selectedScopeIndex(),
			SelectedOptions:     selectedOptions(),
			SelectedDirs:        selectedDirs(),
			IsSubmitting:        submitting(),
			OnClose:             func() { setShowPreviewModal(false) },
			OnApprove:           handleApprove,
			OnDeny:              handleDeny,
			OnHardCancel:        func() { setShowCancelConfirmDialog(true) },
			OnSelectVertical:    handleSelectVertical,
			OnSelectScope:       handleSelectScope,
			OnSelectOption:      handleSelectOption,
			OnSelectDir:         handleSelectDir,
			OnSetCurrentPage:    setCurrentPageIndex,
			IsProvidingFeedback: isProvidingFeedback(),
			FeedbackText:        feedbackText(),
			OnFeedbackChange:    func(val string) { setFeedbackText(val) },
			OnDenyWithFeedback:  handleDenyWithFeedback,
			OnCancelFeedback:    handleCancelFeedback,
			OnStartFeedback:     handleStartFeedback,
		}),

		// Cancel Confirmation Dialog
		kitex.If(showCancelConfirmDialog(), func() kitex.Node {
			return components.ConfirmDialog(components.ConfirmDialogProps{
				Message:      "Are you sure you want to cancel all tool calls and stop execution?",
				ConfirmLabel: "Confirm",
				ConfirmColor: components.ButtonError,
				OnConfirm:    handleHardCancel,
				CancelLabel:  "Cancel",
				OnCancel:     func() { setShowCancelConfirmDialog(false) },
			})
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
		components.Modal(components.ModalProps{
			IsOpen:  showFullOutputModal(),
			Title:   kitex.Text(fullOutputTitle()),
			OnClose: func() { setShowFullOutputModal(false) },
		},
			kitex.If(showFullOutputModal(), func() kitex.Node {
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
			}),
		),

		// Modal Section for Generic Result Preview
		GenericPreviewModal(GenericPreviewModalProps{
			IsOpen:  showResultPreview(),
			Title:   resultPreviewTitle(),
			Preview: resultPreview(),
			OnClose: func() { setShowResultPreview(false) },
		}),
	)
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
	currentPageIndex int,
	focusedItem FocusItem,
	selectedScopeIndex int,
	selectedOptions map[string]int,
	selectedDirs map[string]int,
	onPreview func(),
	currentPendingIndex int,
	isInsert bool,
	localDecisions map[string]permissions.AuthorizationDecision,
	isSubmitting bool,
	onSelectVertical func(FocusItem),
	onSelectScope func(int),
	onSelectOption func(int),
	onSelectDir func(int),
	onApprove func(),
	onDeny func(),
	onHardCancel func(),
	onViewFullOutput func(title, cachedPath string),
	onViewPreview func(title string, p preview.ToolPreview),
	isProvidingFeedback bool,
	feedbackText string,
	onFeedbackChange func(string),
	onDenyWithFeedback func(string),
	onCancelFeedback func(),
	onStartFeedback func(),
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
			nodes = append(nodes, BubbleGroup(BubbleGroupProps{
				Key:                   fmt.Sprintf("group-%s-%s", currentGroupRole, currentGroup[0].GetID()),
				Role:                  currentGroupRole,
				Msgs:                  currentGroup,
				ToolResponses:         toolResponses,
				CurrentDots:           currentDots,
				OneDotCurrentDots:     oneDotCurrentDots,
				MainAgentName:         mainAgentName,
				IsGenerating:          groupIsGenerating,
				LiveThinkingTime:      liveThinkingTime,
				PendingAuthorizations: pendingAuthorizations,
				CurrentPageIndex:      currentPageIndex,
				FocusedItem:           focusedItem,
				SelectedScopeIndex:    selectedScopeIndex,
				SelectedOptions:       selectedOptions,
				SelectedDirs:          selectedDirs,
				OnPreview:             onPreview,
				CurrentPendingIndex:   currentPendingIndex,
				IsInsert:              isInsert,
				LocalDecisions:        localDecisions,
				IsSubmitting:          isSubmitting,
				OnSelectVertical:      onSelectVertical,
				OnSelectScope:         onSelectScope,
				OnSelectOption:        onSelectOption,
				OnSelectDir:           onSelectDir,
				OnApprove:             onApprove,
				OnDeny:                onDeny,
				OnHardCancel:          onHardCancel,
				OnViewFullOutput:      onViewFullOutput,
				OnViewPreview:         onViewPreview,
				IsProvidingFeedback:   isProvidingFeedback,
				FeedbackText:          feedbackText,
				OnFeedbackChange:      onFeedbackChange,
				OnDenyWithFeedback:    onDenyWithFeedback,
				OnCancelFeedback:      onCancelFeedback,
				OnStartFeedback:       onStartFeedback,
			}))
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

func renderQueuedBubbles(
	_ *theme.Scheme,
	queuedMessages message.MessageList,
	onEdit func(string),
	onRemove func(string),
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

		var texts []string
		for _, block := range msg.GetContent() {
			if tb, ok := block.(*message.TextBlock); ok {
				cleaned := tryExtractTextFromJSON(tb.Text)
				texts = append(texts, cleaned)
			}
		}
		if len(texts) == 0 {
			continue
		}

		nodes = append(nodes, QueuedBubble(QueuedBubbleProps{
			Key:      "queued-" + msgID,
			ID:       msgID,
			Text:     strings.Join(texts, "\n"),
			OnEdit:   onEdit,
			OnRemove: onRemove,
		}))
	}

	return nodes
}

func getScopeIndex(req permissions.AuthorizationRequest, page int, scope permissions.PermissionScope) int {
	allowed := getAllowedScopes(req, page)
	for idx, s := range allowed {
		if s == scope {
			return idx
		}
	}
	return -1
}

func getDefaultScopeIndex(req permissions.AuthorizationRequest, page int) int {
	allowed := getAllowedScopes(req, page)
	for idx, s := range allowed {
		if s == permissions.ScopeSession {
			return idx
		}
	}
	return 0
}
