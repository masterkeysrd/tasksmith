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
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	"github.com/masterkeysrd/tasksmith/internal/tui/plugin/tips"
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
	showResolutionDialog, setShowResolutionDialog := kitex.UseState(false)

	handleSelectVertical := func(item FocusItem) {
		setFocusedItem(item)
		mode.Set(mode.Normal)
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
		setSelectedScopeIndex(1) // Default to Session (1)
		setFocusedItem(FocusItemSession)
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

	recordDecision := func(toolCallID string, approved bool, scope permissions.PermissionScope, grantDecisions []permissions.GrantDecision) {
		dec := permissions.AuthorizationDecision{
			ToolCallID:     toolCallID,
			Approved:       approved,
			Scope:          scope,
			GrantDecisions: grantDecisions,
		}

		newDecisions := make(map[string]permissions.AuthorizationDecision)
		maps.Copy(newDecisions, localDecisions())
		newDecisions[toolCallID] = dec
		setLocalDecisions(newDecisions)

		nextIdx := currentPendingIndex() + 1
		if nextIdx < len(pendingAuthorizations) {
			setCurrentPendingIndex(nextIdx)
			setSelectedScopeIndex(1) // default to Session
			setFocusedItem(FocusItemSession)
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

	currentPendingIndexRef := kitex.UseRef(0)
	currentPendingIndexRef.Current = currentPendingIndex()

	recordDecisionRef := kitex.UseRef[func(string, bool, permissions.PermissionScope, []permissions.GrantDecision)](nil)
	recordDecisionRef.Current = recordDecision

	handleApprove := func() {
		currIdx := currentPendingIndexRef.Current
		if currIdx >= len(pendingAuthsRef.Current) {
			return
		}
		req := pendingAuthsRef.Current[currIdx]

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
			})
		}

		scope := permissions.ScopeOnce
		switch selectedScopeIndexRef.Current {
		case 0:
			scope = permissions.ScopeOnce
		case 1:
			scope = permissions.ScopeSession
		case 2:
			scope = permissions.ScopeWorkspace
		case 3:
			scope = permissions.ScopeGlobal
		}

		if recordDecisionRef.Current != nil {
			recordDecisionRef.Current(req.ToolCallID, true, scope, decisions)
		}
	}

	handleDeny := func() {
		currIdx := currentPendingIndexRef.Current
		if currIdx >= len(pendingAuthsRef.Current) {
			return
		}
		req := pendingAuthsRef.Current[currIdx]
		if recordDecisionRef.Current != nil {
			recordDecisionRef.Current(req.ToolCallID, false, permissions.ScopeOnce, nil)
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

			getVisibleItems := func(scopeIdx int, req permissions.AuthorizationRequest, page int) []FocusItem {
				items := []FocusItem{FocusItemOnce, FocusItemSession}
				if len(req.GrantRequests) > page {
					gr := req.GrantRequests[page]
					if scopeIdx == 1 && len(gr.Options) > 0 {
						items = append(items, FocusItemSessionCmd)
					}
					items = append(items, FocusItemWorkspace)
					if scopeIdx == 2 && len(gr.Options) > 0 {
						items = append(items, FocusItemWorkspaceCmd)
					}
					items = append(items, FocusItemGlobal)
					if scopeIdx == 3 && len(gr.Options) > 0 {
						items = append(items, FocusItemGlobalCmd)
					}
					if scopeIdx > 0 && len(gr.DirectoryOptions) > 0 {
						items = append(items, FocusItemDirectory)
					}
				} else {
					items = append(items, FocusItemWorkspace, FocusItemGlobal)
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

				switch newItem {
				case FocusItemOnce:
					setSelectedScopeIndex(0)
				case FocusItemSession:
					setSelectedScopeIndex(1)
				case FocusItemWorkspace:
					setSelectedScopeIndex(2)
				case FocusItemGlobal:
					setSelectedScopeIndex(3)
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

				switch newItem {
				case FocusItemOnce:
					setSelectedScopeIndex(0)
				case FocusItemSession:
					setSelectedScopeIndex(1)
				case FocusItemWorkspace:
					setSelectedScopeIndex(2)
				case FocusItemGlobal:
					setSelectedScopeIndex(3)
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
		// we scroll to the absolute bottom using a large value (99999).
		// Kite's paint engine will synchronously clamp this value to the fresh scroll extent at paint time in the current frame.
		if currentY >= lastMaxScrollY.Current-2 {
			el.ScrollTo(0, 99999)
		}

		lastMaxScrollY.Current = maxScrollY
	}, []any{messagesKey})

	sendMessage := func(text string, force ...bool) {
		if text == "" || submitting() {
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
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: sessionID})
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
			windClient.InvalidateQueries(api.GetFileChangesRequest{SessionID: sessionID})
		}, func(err error) {
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
	}

	return kitex.Box(outerProps,
		// Session Title Bar
		kitex.Box(kitex.BoxProps{Style: sessionTitleBarStyle},
			kitex.Text(title),
		),

		// Message History Section
		kitex.Box(kitex.BoxProps{Style: messagesContainerStyle, Ref: historyRef},
			// Bubbles
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
					openFullOutputModal,
					onViewPreview,
				)...,
			),

			// Agent Status Widget
			kitex.If(sending || lastFinishedTime() >= 0, func() kitex.Node {
				return renderAgentStatus(t, sending, thinkingTime(), lastFinishedTime(), currentDots, runPromptTokens, runCompletionTokens, runTotalTokens, isGenerating, phase, activeTip)
			}),

			// Queued Messages Widget
			kitex.If(len(queuedMessages) > 0, func() kitex.Node {
				return renderQueuedMessages(t, queuedMessages)
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
			OnSelectVertical:    handleSelectVertical,
			OnSelectScope:       handleSelectScope,
			OnSelectOption:      handleSelectOption,
			OnSelectDir:         handleSelectDir,
			OnSetCurrentPage:    setCurrentPageIndex,
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

type ToolExecutionProps struct {
	ToolCall         *message.ToolCall
	ToolMessage      *message.Tool
	CurrentDots      string
	OnViewFullOutput func(title, cachedPath string)
	OnViewPreview    func(title string, p preview.ToolPreview)
}

var ToolExecution = kitex.FC("ToolExecution", func(props ToolExecutionProps) kitex.Node {
	if props.ToolCall != nil && props.ToolCall.Name == "bash" {
		return BashToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "view" {
		return ViewToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "ls" {
		return LsToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "glob" {
		return GlobToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "lsp_diagnostics" {
		return LspDiagnosticsToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "lsp_restart" {
		return LspRestartToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "lsp_symbols" {
		return LspSymbolsToolWidget(props)
	}
	if props.ToolCall != nil && props.ToolCall.Name == "lsp_inspect" {
		return LspInspectToolWidget(props)
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
	if props.ToolCall != nil || props.ToolCall == nil {
		return nil
	}

	t := theme.UseTheme()
	isOpen, setIsOpen := kitex.UseState(true)
	hasAutoCollapsed, setHasAutoCollapsed := kitex.UseState(false)
	showFullOutput, setShowFullOutput := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	if tm != nil && !tm.IsError && !hasAutoCollapsed() {
		setIsOpen(false)
		setHasAutoCollapsed(true)
	}

	var outText string
	if tm != nil {
		outText = getToolOutput(tm.Content)
	}

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

	return kitex.Fragment(
		kitex.Box(kitex.BoxProps{Style: containerStyle},
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

						meta := tm.GetMetadata()
						isTruncated := false
						var cachedPath string
						if meta != nil {
							if tr, ok := meta["truncated"].(bool); ok && tr {
								isTruncated = true
							}
							if cp, ok := meta["full_content_path"].(string); ok {
								cachedPath = cp
							}
						}
						return kitex.Fragment(
							kitex.If(isTruncated && cachedPath != "" && props.OnViewFullOutput != nil, func() kitex.Node {
								return kitex.Box(kitex.BoxProps{
									Style: style.S().MarginBottom(1),
								},
									components.Button(components.ButtonProps{
										Variant: components.ButtonSolid,
										Color:   components.ButtonPrimary,
										OnClick: func() {
											props.OnViewFullOutput(fmt.Sprintf("Full Output: %s", tc.Name), cachedPath)
										},
									}, kitex.Box(kitex.BoxProps{
										Style: style.S().
											Display(style.DisplayFlex).
											FlexDirection(style.FlexRow).
											AlignItems(style.AlignCenter).
											Gap(1),
									},
										icon.Search,
										kitex.Text("VIEW FULL OUTPUT IN MODAL"),
									)),
								)
							}),
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

								// Count lines and check length
								lines := strings.Split(outText, "\n")
								isInlineTruncated := len(lines) > 15 || len(outText) > 1000
								var displayText string
								if isInlineTruncated {
									if len(lines) > 15 {
										displayText = strings.Join(lines[:15], "\n") + "\n\n... (truncated for display, click button below to view full output)"
									} else {
										displayText = outText[:1000] + "\n\n... (truncated for display, click button below to view full output)"
									}
								} else {
									displayText = outText
								}

								cleanText := strings.ReplaceAll(displayText, "\t", "    ")
								return kitex.Fragment(
									kitex.Box(kitex.BoxProps{Style: outputContainerStyle},
										kitex.Text(cleanText),
									),
									kitex.If(isInlineTruncated, func() kitex.Node {
										return kitex.Box(kitex.BoxProps{
											Style: style.S().MarginTop(1),
										},
											components.Button(components.ButtonProps{
												Variant: components.ButtonSolid,
												Color:   components.ButtonPrimary,
												OnClick: func() {
													setShowFullOutput(true)
												},
											}, kitex.Box(kitex.BoxProps{
												Style: style.S().
													Display(style.DisplayFlex).
													FlexDirection(style.FlexRow).
													AlignItems(style.AlignCenter).
													Gap(1),
											},
												icon.Search,
												kitex.Text(" VIEW ENTIRE OUTPUT"),
											)),
										)
									}),
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
		),
		components.Modal(components.ModalProps{
			IsOpen:  showFullOutput(),
			Title:   kitex.Text(fmt.Sprintf("Full Output: %s", tc.Name)),
			OnClose: func() { setShowFullOutput(false) },
		},
			kitex.If(showFullOutput(), func() kitex.Node {
				var borderCol color.Color
				var textCol color.Color
				if t != nil {
					borderCol = t.Color.Border.Primary
					textCol = t.Color.Text.Secondary
				}
				outputStyle := style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Border(true, style.SingleBorder(), borderCol).
					Background(t.Color.Surface.BaseHover).
					Padding(1).
					Width(style.Percent(100)).
					MaxWidth(style.Percent(100)).
					Foreground(textCol).
					WhiteSpace(style.WhiteSpacePreWrap)

				cleanText := strings.ReplaceAll(outText, "\t", "    ")
				return kitex.Box(kitex.BoxProps{Style: outputStyle},
					kitex.Text(cleanText),
				)
			}),
		),
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
	onViewFullOutput func(title, cachedPath string),
	onViewPreview func(title string, p preview.ToolPreview),
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
				Key:                   fmt.Sprintf("group-%s-%d", currentGroupRole, len(nodes)),
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
				OnViewFullOutput:      onViewFullOutput,
				OnViewPreview:         onViewPreview,
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

		var text strings.Builder
		for _, block := range msg.GetContent() {
			if tb, ok := block.(*message.TextBlock); ok {
				text.WriteString(tb.Text)
			}
		}
		if text.String() == "" {
			continue
		}

		msgStyle := style.S().
			Foreground(t.Color.Text.Secondary).
			MarginLeft(2)

		msgNodes = append(msgNodes, kitex.Box(kitex.BoxProps{Style: msgStyle}, kitex.Text("󰑮  "+text.String())))
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
