package chat

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type FocusItem string

const (
	FocusItemOnce         FocusItem = "once"
	FocusItemSession      FocusItem = "session"
	FocusItemSessionCmd   FocusItem = "session_cmd"
	FocusItemWorkspace    FocusItem = "workspace"
	FocusItemWorkspaceCmd FocusItem = "workspace_cmd"
	FocusItemGlobal       FocusItem = "global"
	FocusItemGlobalCmd    FocusItem = "global_cmd"
	FocusItemDirectory    FocusItem = "directory"
)

type AuthorizationHybridSelectorProps struct {
	Options             []permissions.PermissionOption
	DirectoryOptions    []permissions.PermissionOption
	FocusedItem         FocusItem
	SelectedScopeIndex  int // Index in AllowedScopes slice
	SelectedOptionIndex int // Index of selected command option
	SelectedDirIndex    int // Index of selected directory option
	IsActive            bool
	AllowedScopes       []permissions.PermissionScope
	OnSelectVertical    func(FocusItem)
	OnSelectScope       func(int)
	OnSelectOption      func(int)
	OnSelectDir         func(int)
}

var AuthorizationHybridSelector = kitex.FC("AuthorizationHybridSelector", func(props AuthorizationHybridSelectorProps) kitex.Node {
	t := theme.UseTheme()
	if t == nil {
		return nil
	}

	allScopes := []struct {
		Name        string
		Description string
		Scope       permissions.PermissionScope
		Icon        kitex.Node
		FocusType   FocusItem
		CmdFocus    FocusItem
	}{
		{Name: "Once", Description: "Only this one time", Scope: permissions.ScopeOnce, FocusType: FocusItemOnce},
		{Name: "Session", Description: "For this session only", Scope: permissions.ScopeSession, FocusType: FocusItemSession, CmdFocus: FocusItemSessionCmd},
		{Name: "Workspace", Description: "Save to workspace config", Scope: permissions.ScopeWorkspace, Icon: icon.Cog, FocusType: FocusItemWorkspace, CmdFocus: FocusItemWorkspaceCmd},
		{Name: "Global", Description: "Save to global config", Scope: permissions.ScopeGlobal, Icon: icon.Warning, FocusType: FocusItemGlobal, CmdFocus: FocusItemGlobalCmd},
	}

	var scopesList []struct {
		Name        string
		Description string
		Scope       permissions.PermissionScope
		Icon        kitex.Node
		FocusType   FocusItem
		CmdFocus    FocusItem
	}

	if len(props.AllowedScopes) > 0 {
		for _, allowed := range props.AllowedScopes {
			for _, s := range allScopes {
				if s.Scope == allowed {
					scopesList = append(scopesList, s)
					break
				}
			}
		}
	} else {
		scopesList = allScopes
	}

	var rows []kitex.Node
	for idx, s := range scopesList {
		rowIdx := idx
		isScopeSelected := props.SelectedScopeIndex == rowIdx
		isScopeFocused := props.FocusedItem == s.FocusType && props.IsActive

		// Risk-aware colors
		var rowFg = t.Color.Text.Secondary
		var selectedFg color.Color
		var selectedBg color.Color

		switch s.Scope {
		case permissions.ScopeOnce:
			selectedFg = t.Color.Surface.Success
			selectedBg = color.RGBA{R: 0, G: 255, B: 0, A: 30}
		case permissions.ScopeSession:
			selectedFg = t.Color.Surface.Info
			selectedBg = t.Color.Surface.BaseHover
		case permissions.ScopeWorkspace:
			selectedFg = color.RGBA{R: 224, G: 153, B: 36, A: 255}
			selectedBg = color.RGBA{R: 224, G: 153, B: 36, A: 40}
		case permissions.ScopeGlobal:
			selectedFg = t.Color.Surface.Error
			selectedBg = color.RGBA{R: 255, G: 0, B: 0, A: 30}
		}

		rowStyle := style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Padding(0, 1)

		lblStyle := style.S()
		if isScopeFocused {
			lblStyle = lblStyle.Foreground(selectedFg).Bold(true)
			rowStyle = rowStyle.Background(selectedBg)
		} else if isScopeSelected {
			lblStyle = lblStyle.Foreground(selectedFg).Bold(true)
		} else {
			lblStyle = lblStyle.Foreground(rowFg)
		}

		checkbox := "○"
		if isScopeSelected {
			checkbox = "●"
		}

		// --- NESTED COMMAND SUB-ROW ---
		var cmdSubRow kitex.Node
		hasHorizontal := rowIdx == 1 || rowIdx == 2 || rowIdx == 3
		if isScopeSelected && hasHorizontal && len(props.Options) > 0 {
			isCmdFocused := props.FocusedItem == s.CmdFocus && props.IsActive
			var cmdPills []kitex.Node

			for hIdx, opt := range props.Options {
				pillIdx := hIdx
				isHSelected := props.SelectedOptionIndex == pillIdx
				label := formatTargetLabel(opt)

				pillStyle := style.S().
					MarginRight(2).
					MaxWidth(style.Percent(100)).
					MinWidth(style.Percent(0)).
					WhiteSpace(style.WhiteSpacePreWrap).
					OverflowWrap(style.OverflowWrapBreakWord)
				if isCmdFocused && isHSelected {
					pillStyle = pillStyle.Background(color.RGBA{R: 0, G: 255, B: 0, A: 20}).Padding(0, 1)
				}

				if isHSelected {
					cmdPills = append(cmdPills, kitex.Box(kitex.BoxProps{
						Style: pillStyle.Foreground(t.Color.Surface.Success).Bold(true),
						OnClick: func(e event.Event) {
							if props.OnSelectOption != nil {
								props.OnSelectOption(pillIdx)
							}
						},
					}, kitex.Text(fmt.Sprintf("[%s]", label))))
				} else {
					cmdPills = append(cmdPills, kitex.Box(kitex.BoxProps{
						Style: pillStyle.Foreground(t.Color.Text.Secondary),
						OnClick: func(e event.Event) {
							if props.OnSelectOption != nil {
								props.OnSelectOption(pillIdx)
							}
						},
					}, kitex.Text(label)))
				}
			}

			cmdSubRow = kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					FlexWrap(style.FlexWrapOn).
					AlignItems(style.AlignCenter).
					PaddingLeft(4).
					Width(style.Percent(100)).
					MaxWidth(style.Percent(100)).
					MinWidth(style.Percent(0)),
				OnClick: func(e event.Event) {
					if props.OnSelectVertical != nil {
						props.OnSelectVertical(s.CmdFocus)
					}
				},
			}, cmdPills...)
		}

		rows = append(rows, kitex.Box(kitex.BoxProps{
			Style: rowStyle,
			OnClick: func(e event.Event) {
				if props.OnSelectVertical != nil {
					props.OnSelectVertical(s.FocusType)
				}
			},
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					Gap(1),
			},
				kitex.Span(kitex.SpanProps{Style: lblStyle}, kitex.Text(fmt.Sprintf("%s %s", checkbox, s.Name))),
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text(s.Description)),
				kitex.If(s.Icon != nil, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(selectedFg)}, s.Icon)
				}),
			),
			kitex.If(cmdSubRow != nil, func() kitex.Node { return cmdSubRow }),
		))
	}

	// --- FLAT DIRECTORY LIMIT SECTION AT THE BOTTOM ---
	var dirSection kitex.Node
	if props.SelectedScopeIndex > 0 && len(props.DirectoryOptions) > 0 {
		isDirFocused := props.FocusedItem == FocusItemDirectory && props.IsActive
		var dirPills []kitex.Node

		for hIdx, opt := range props.DirectoryOptions {
			pillIdx := hIdx
			isHSelected := props.SelectedDirIndex == pillIdx
			label := opt.Label
			if label == "" {
				label = opt.Target
			}

			pillStyle := style.S().
				MarginRight(2).
				MaxWidth(style.Percent(100)).
				MinWidth(style.Percent(0)).
				WhiteSpace(style.WhiteSpacePreWrap).
				OverflowWrap(style.OverflowWrapBreakWord)
			if isDirFocused && isHSelected {
				pillStyle = pillStyle.Background(color.RGBA{R: 255, G: 255, B: 0, A: 20}).Padding(0, 1)
			}

			if isHSelected {
				dirPills = append(dirPills, kitex.Box(kitex.BoxProps{
					Style: pillStyle.Foreground(t.Color.Text.Primary).Bold(true),
					OnClick: func(e event.Event) {
						if props.OnSelectDir != nil {
							props.OnSelectDir(pillIdx)
						}
					},
				}, kitex.Text(fmt.Sprintf("[%s]", label))))
			} else {
				dirPills = append(dirPills, kitex.Box(kitex.BoxProps{
					Style: pillStyle.Foreground(t.Color.Text.Secondary),
					OnClick: func(e event.Event) {
						if props.OnSelectDir != nil {
							props.OnSelectDir(pillIdx)
						}
					},
				}, kitex.Text(label)))
			}
		}

		dirSection = kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				MarginTop(1).
				PaddingLeft(1).
				Width(style.Percent(100)).
				MaxWidth(style.Percent(100)).
				MinWidth(style.Percent(0)),
			OnClick: func(e event.Event) {
				if props.OnSelectVertical != nil {
					props.OnSelectVertical(FocusItemDirectory)
				}
			},
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().Bold(true).Foreground(t.Color.Text.Primary).PaddingBottom(0),
			}, kitex.Text("Limit to:")),
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					FlexWrap(style.FlexWrapOn).
					AlignItems(style.AlignCenter).
					PaddingLeft(2).
					Width(style.Percent(100)).
					MaxWidth(style.Percent(100)).
					MinWidth(style.Percent(0)),
			}, dirPills...),
		)
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Gap(0).
			Width(style.Percent(100)).
			MaxWidth(style.Percent(100)).
			MinWidth(style.Percent(0)).
			Overflow(style.OverflowHidden),
	},
		append(rows, kitex.If(dirSection != nil, func() kitex.Node { return dirSection }))...,
	)
})

type AuthorizationWidgetProps struct {
	Request             permissions.AuthorizationRequest
	SessionID           string
	IsActive            bool
	OnDecision          func(permissions.AuthorizationDecision)
	LocalDecision       *permissions.AuthorizationDecision
	ShowPreviewModal    bool
	SetShowPreviewModal func(bool)
}

var AuthorizationWidget = kitex.FC("AuthorizationWidget", func(props AuthorizationWidgetProps) kitex.Node {
	t := theme.UseTheme()
	if t == nil {
		return nil
	}

	client := tuiapi.UseClient()
	windClient := wind.UseClient()

	req := props.Request

	// Local states for interactive selection
	selectedScopeIndex, setSelectedScopeIndex := kitex.UseState(0) // Default to Once (0)
	focusedItem, setFocusedItem := kitex.UseState(FocusItemOnce)
	selectedOptionIndex, setSelectedOptionIndex := kitex.UseState(0)
	selectedDirIndex, setSelectedDirIndex := kitex.UseState(0)
	var showPreviewModal func() bool
	var setShowPreviewModal func(bool)
	if props.SetShowPreviewModal != nil {
		showPreviewModal = func() bool { return props.ShowPreviewModal }
		setShowPreviewModal = props.SetShowPreviewModal
	} else {
		localShow, localSet := kitex.UseState(false)
		showPreviewModal = localShow
		setShowPreviewModal = localSet
	}
	showCancelConfirmDialog, setShowCancelConfirmDialog := kitex.UseState(false)
	isProvidingFeedback, setIsProvidingFeedback := kitex.UseState(false)
	feedbackText, setFeedbackText := kitex.UseState("")
	submitting, setSubmitting := kitex.UseState(false)

	currentMode := mode.Use()
	isVisuallyActive := props.IsActive && (currentMode == mode.Normal || isProvidingFeedback())

	// Ref and Layout Effect to center the active widget vertically in the chat history container
	elRef := kitex.UseRef[dom.Element](nil)
	kitex.UseLayoutEffect(func() {
		if !props.IsActive {
			return
		}
		node := elRef.Current
		if node == nil {
			return
		}
		doc := node.OwnerDocument()
		if doc == nil {
			return
		}
		view := doc.DefaultView()
		if view == nil {
			return
		}

		var scroller dom.Element
		curr := node.Parent()
		for curr != nil {
			if e, ok := curr.(dom.Element); ok {
				if val, okAttr := e.Attribute("data-id"); okAttr && val == "history-scroller" {
					scroller = e
					break
				}
			}
			curr = curr.Parent()
		}

		if scroller != nil {
			rectScroller, okScroller := view.GetBoundingClientRect(scroller)
			rectWidget, okWidget := view.GetBoundingClientRect(node)

			if okScroller && okWidget {
				// Align the vertical centers of both the widget and the scroller
				scrollerCenterY := rectScroller.Origin.Y + rectScroller.Size.Height/2
				widgetCenterY := rectWidget.Origin.Y + rectWidget.Size.Height/2

				diffY := widgetCenterY - scrollerCenterY

				scrollX, scrollY := scroller.Scroll()
				targetY := scrollY + diffY

				_, maxScrollY := view.GetMaxScroll(scroller)
				if targetY < 0 {
					targetY = 0
				}
				if targetY > maxScrollY {
					targetY = maxScrollY
				}

				if targetY != scrollY {
					scroller.ScrollTo(scrollX, targetY)
				}
			}
		}
	}, []any{props.IsActive, focusedItem(), isProvidingFeedback()})

	// Autofocus inline feedback textarea when opened
	textareaRef := kitex.CreateRef[dom.Element]()
	kitex.UseEffect(func() {
		if isProvidingFeedback() {
			kitex.PostMacro(func() {
				if textareaRef.Current != nil {
					if doc := textareaRef.Current.OwnerDocument(); doc != nil {
						doc.Focus(textareaRef.Current)
					}
				}
			})
		}
	}, []any{isProvidingFeedback()})

	// Decision recorder & submitter
	submitDecision := func(approved bool, scope permissions.PermissionScope, grantDecisions []permissions.GrantDecision, reason string) {
		setIsProvidingFeedback(false)
		IsFeedbackActive = false
		setFeedbackText("")

		if props.OnDecision != nil {
			props.OnDecision(permissions.AuthorizationDecision{
				ToolCallID:      req.ToolCallID,
				Approved:        approved,
				Scope:           scope,
				GrantDecisions:  grantDecisions,
				Reason:          reason,
				CancelExecution: false,
			})
			return
		}

		setSubmitting(true)
		promise.New(func(ctx context.Context) (bool, error) {
			_, err := client.SubmitAuthorizationDecision(ctx, api.SubmitAuthorizationDecisionRequest{
				SessionID: props.SessionID,
				Decisions: []permissions.AuthorizationDecision{
					{
						ToolCallID:      req.ToolCallID,
						Approved:        approved,
						Scope:           scope,
						GrantDecisions:  grantDecisions,
						Reason:          reason,
						CancelExecution: false,
					},
				},
			})
			return err == nil, err
		}).Then(func(success bool) {
			setSubmitting(false)
			setShowPreviewModal(false)
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: props.SessionID})
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: props.SessionID})
			windClient.InvalidateQueries(api.GetFileChangesRequest{SessionID: props.SessionID})
		}, func(err error) {
			setSubmitting(false)
			log.Error(fmt.Sprintf("Failed to submit authorization decision: %v", err))
		})
	}

	handleApprove := func() {
		scope := permissions.ScopeOnce
		allowed := getAllowedScopes(req, 0)
		idx := selectedScopeIndex()
		if idx >= 0 && idx < len(allowed) {
			scope = allowed[idx]
		}

		var decisions []permissions.GrantDecision
		for _, gr := range req.GrantRequests {
			optIdx := selectedOptionIndex()
			if optIdx < 0 || optIdx >= len(gr.Options) {
				optIdx = 0
			}
			target := "*"
			if len(gr.Options) > 0 {
				target = gr.Options[optIdx].Target
			}

			dirIdx := selectedDirIndex()
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

		submitDecision(true, scope, decisions, "")
	}

	handleDeny := func() {
		submitDecision(false, permissions.ScopeOnce, nil, "")
	}

	handleDenyWithFeedback := func(reason string) {
		submitDecision(false, permissions.ScopeOnce, nil, reason)
	}

	handleHardCancel := func() {
		setSubmitting(true)
		promise.New(func(ctx context.Context) (bool, error) {
			_, err := client.SubmitAuthorizationDecision(ctx, api.SubmitAuthorizationDecisionRequest{
				SessionID: props.SessionID,
				Decisions: []permissions.AuthorizationDecision{
					{
						ToolCallID:      req.ToolCallID,
						Approved:        false,
						CancelExecution: true,
					},
				},
			})
			return err == nil, err
		}).Then(func(success bool) {
			setShowCancelConfirmDialog(false)
			setShowPreviewModal(false)
			setSubmitting(false)
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: props.SessionID})
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: props.SessionID})
		}, func(err error) {
			setShowCancelConfirmDialog(false)
			setSubmitting(false)
			log.Error(fmt.Sprintf("Failed to submit cancellation decision: %v", err))
		})
	}

	// Bind handlers to the persistent static AuthCtrl if this widget is active
	if props.IsActive {
		AuthCtrl.ActiveToolCallID = req.ToolCallID
		AuthCtrl.MoveDown = func() {
			if isProvidingFeedback() || showCancelConfirmDialog() {
				return
			}
			items := getVisibleItems(selectedScopeIndex(), req, 0)
			currItem := focusedItem()
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
			case FocusItemSession, FocusItemSessionCmd:
				targetScope = permissions.ScopeSession
			case FocusItemWorkspace, FocusItemWorkspaceCmd:
				targetScope = permissions.ScopeWorkspace
			case FocusItemGlobal, FocusItemGlobalCmd:
				targetScope = permissions.ScopeGlobal
			}
			if targetScope != "" {
				targetScopeIdx := getScopeIndex(req, 0, targetScope)
				if targetScopeIdx != -1 {
					setSelectedScopeIndex(targetScopeIdx)
				}
			}
		}

		AuthCtrl.MoveUp = func() {
			if isProvidingFeedback() || showCancelConfirmDialog() {
				return
			}
			items := getVisibleItems(selectedScopeIndex(), req, 0)
			currItem := focusedItem()
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
			case FocusItemSession, FocusItemSessionCmd:
				targetScope = permissions.ScopeSession
			case FocusItemWorkspace, FocusItemWorkspaceCmd:
				targetScope = permissions.ScopeWorkspace
			case FocusItemGlobal, FocusItemGlobalCmd:
				targetScope = permissions.ScopeGlobal
			}
			if targetScope != "" {
				targetScopeIdx := getScopeIndex(req, 0, targetScope)
				if targetScopeIdx != -1 {
					setSelectedScopeIndex(targetScopeIdx)
				}
			}
		}

		AuthCtrl.SelectPrevOption = func() {
			if isProvidingFeedback() || showCancelConfirmDialog() {
				return
			}
			if len(req.GrantRequests) > 0 {
				currReq := req.GrantRequests[0]
				currItem := focusedItem()

				switch currItem {
				case FocusItemSessionCmd, FocusItemWorkspaceCmd, FocusItemGlobalCmd:
					optsCount := len(currReq.Options)
					if optsCount > 1 {
						setSelectedOptionIndex((selectedOptionIndex() - 1 + optsCount) % optsCount)
					}
				case FocusItemDirectory:
					dirOptsCount := len(currReq.DirectoryOptions)
					if dirOptsCount > 1 {
						setSelectedDirIndex((selectedDirIndex() - 1 + dirOptsCount) % dirOptsCount)
					}
				}
			}
		}

		AuthCtrl.SelectNextOption = func() {
			if isProvidingFeedback() || showCancelConfirmDialog() {
				return
			}
			if len(req.GrantRequests) > 0 {
				currReq := req.GrantRequests[0]
				currItem := focusedItem()

				switch currItem {
				case FocusItemSessionCmd, FocusItemWorkspaceCmd, FocusItemGlobalCmd:
					optsCount := len(currReq.Options)
					if optsCount > 1 {
						setSelectedOptionIndex((selectedOptionIndex() + 1) % optsCount)
					}
				case FocusItemDirectory:
					dirOptsCount := len(currReq.DirectoryOptions)
					if dirOptsCount > 1 {
						setSelectedDirIndex((selectedDirIndex() + 1) % dirOptsCount)
					}
				}
			}
		}

		AuthCtrl.Approve = func() {
			if isProvidingFeedback() {
				return
			}
			if showCancelConfirmDialog() {
				handleHardCancel()
				return
			}
			handleApprove()
		}

		AuthCtrl.Deny = func() {
			if isProvidingFeedback() || showCancelConfirmDialog() {
				return
			}
			handleDeny()
		}

		AuthCtrl.StartFeedback = func() {
			if isProvidingFeedback() || showCancelConfirmDialog() {
				return
			}
			IsFeedbackActive = true
			setIsProvidingFeedback(true)
			setFeedbackText("")
			mode.Set(mode.Insert)
		}

		AuthCtrl.ToggleCancelDialog = func() {
			if isProvidingFeedback() {
				return
			}
			if showCancelConfirmDialog() {
				setShowCancelConfirmDialog(false)
				return
			}
			setShowCancelConfirmDialog(true)
		}

		AuthCtrl.ShowPreview = func() {
			if isProvidingFeedback() || showCancelConfirmDialog() {
				return
			}
			if req.Preview != nil {
				setShowPreviewModal(true)
			}
		}

	}

	kitex.UseEffectCleanup(func() func() {
		return func() {
			if props.IsActive {
				if AuthCtrl.ActiveToolCallID == req.ToolCallID {
					AuthCtrl.ActiveToolCallID = ""
					AuthCtrl.MoveDown = nil
					AuthCtrl.MoveUp = nil
					AuthCtrl.SelectPrevOption = nil
					AuthCtrl.SelectNextOption = nil
					AuthCtrl.Approve = nil
					AuthCtrl.Deny = nil
					AuthCtrl.StartFeedback = nil
					AuthCtrl.ToggleCancelDialog = nil
					AuthCtrl.ShowPreview = nil
				}
			}
		}
	}, []any{props.IsActive})

	borderColor := t.Color.Border.Primary
	if isVisuallyActive {
		borderColor = t.Color.Surface.Info
	}

	containerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100)).
		Border(true, style.SingleBorder(), borderColor).
		Background(t.Color.Surface.BaseHover)

	titleColor := t.Color.Text.Secondary
	if isVisuallyActive {
		titleColor = color.Color(color.RGBA{R: 224, G: 153, B: 36, A: 255})
	}

	var currReq *permissions.PermissionGrantRequest
	var currReqOptions []permissions.PermissionOption
	var currReqDirOptions []permissions.PermissionOption
	if len(req.GrantRequests) > 0 {
		currReq = &req.GrantRequests[0]
		currReqOptions = currReq.Options
		currReqDirOptions = currReq.DirectoryOptions
	}

	return kitex.Box(kitex.BoxProps{Style: containerStyle, Ref: elRef},
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				PaddingTop(0).
				PaddingHorizontal(1).
				PaddingBottom(1).
				Width(style.Percent(100)).
				MaxWidth(style.Percent(100)).
				MinWidth(style.Percent(0)).
				Overflow(style.OverflowHidden),
		},
			// Header
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).JustifyContent(style.JustifyBetween).PaddingBottom(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1)},
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(titleColor)}, icon.Alert),
					kitex.Span(kitex.SpanProps{Style: style.S().Bold(true).Foreground(titleColor)}, kitex.Text("Authorization Required")),
				),
				func() kitex.Node {
					if props.LocalDecision != nil {
						statusText := "● APPROVED"
						statusColor := t.Color.Surface.Success
						if !props.LocalDecision.Approved {
							statusText = "● DENIED"
							statusColor = t.Color.Text.Error
						}
						return kitex.Span(kitex.SpanProps{
							Style: style.S().Foreground(statusColor).Bold(true),
						}, kitex.Text(statusText))
					}
					return kitex.Span(kitex.SpanProps{
						Style: style.S().Foreground(t.Color.Surface.Info).Bold(isVisuallyActive),
					}, kitex.Text(func() string {
						if isVisuallyActive {
							return "● ACTIVE"
						}
						return "○ UNFOCUSED"
					}()))
				}(),
			),

			// Identity details
			kitex.Box(kitex.BoxProps{Style: style.S().MarginTop(0)}, renderIdentity(t, req)),

			// Context details
			kitex.If(req.Description != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						Gap(1).
						MarginBottom(1).
						Width(style.Percent(100)).
						MaxWidth(style.Percent(100)).
						MinWidth(style.Percent(0)),
				},
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).MinWidth(style.Cells(9))}, kitex.Text("Context")),
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Foreground(t.Color.Text.Tertiary).
							Italic(true).
							Flex(1, 1, style.Cells(0)).
							Width(style.Percent(100)).
							MaxWidth(style.Percent(100)).
							MinWidth(style.Percent(0)).
							WhiteSpace(style.WhiteSpacePreWrap).
							OverflowWrap(style.OverflowWrapBreakWord),
					}, kitex.Text(req.Description)),
				)
			}),

			// Action details
			kitex.If(currReq != nil, func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						Gap(1).
						MarginBottom(1).
						Width(style.Percent(100)).
						MaxWidth(style.Percent(100)).
						MinWidth(style.Percent(0)),
				},
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).MinWidth(style.Cells(9))}, kitex.Text("Action")),
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Foreground(t.Color.Text.Purple).
							Bold(true).
							Flex(1, 1, style.Cells(0)).
							Width(style.Percent(100)).
							MaxWidth(style.Percent(100)).
							MinWidth(style.Percent(0)).
							WhiteSpace(style.WhiteSpacePreWrap).
							OverflowWrap(style.OverflowWrapBreakWord),
					}, kitex.Text(currReq.Description)),
				)
			}),

			// System Warnings
			kitex.If(len(req.SystemHints) > 0, func() kitex.Node {
				var hintNodes []kitex.Node
				for _, hint := range req.SystemHints {
					hintNodes = append(hintNodes, components.Alert(components.AlertProps{
						Severity: components.AlertWarning,
						ShowIcon: true,
						Style:    style.S(),
					}, kitex.Text(hint)))
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1).MarginBottom(1),
				},
					hintNodes...,
				)
			}),

			// Interactive Selector Area (only if active)
			kitex.If(isVisuallyActive, func() kitex.Node {
				enterLabel := "Allow"
				buttons := []kitex.Node{
					components.Button(components.ButtonProps{
						Variant:   components.ButtonText,
						Color:     components.ButtonSuccess,
						StartIcon: icon.Check,
						OnClick:   handleApprove,
					}, kitex.Text(fmt.Sprintf("%s (Enter)", enterLabel))),
					components.Button(components.ButtonProps{
						Variant:   components.ButtonText,
						Color:     components.ButtonError,
						StartIcon: icon.Error,
						OnClick:   handleDeny,
					}, kitex.Text("Deny (d)")),
					components.Button(components.ButtonProps{
						Variant:   components.ButtonText,
						Color:     components.ButtonError,
						StartIcon: icon.Pencil,
						OnClick: func() {
							setIsProvidingFeedback(true)
							setFeedbackText("")
							mode.Set(mode.Insert)
						},
					}, kitex.Text("Deny with Feedback (D)")),
					components.Button(components.ButtonProps{
						Variant:   components.ButtonText,
						Color:     components.ButtonError,
						StartIcon: icon.Exit,
						OnClick:   func() { setShowCancelConfirmDialog(true) },
					}, kitex.Text("Hard Cancel (Ctrl+C)")),
				}

				if req.Preview != nil {
					buttons = append(buttons, components.Button(components.ButtonProps{
						Variant:   components.ButtonText,
						Color:     components.ButtonPrimary,
						StartIcon: icon.History,
						OnClick:   func() { setShowPreviewModal(true) },
					}, kitex.Text("Preview (p)")))
				}

				return kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1).MarginTop(1),
				},
					// Scope Selection & Restrictions
					kitex.If(isProvidingFeedback(), func() kitex.Node {
						textareaStyle := style.S().
							Width(style.Percent(100)).
							Height(style.Cells(3)).
							Background(color.Transparent).
							Foreground(t.Color.Text.Primary).
							Border(true, style.SingleBorder(), t.Color.Border.Primary).
							Padding(0, 1)

						return kitex.Box(kitex.BoxProps{
							Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
						},
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).Bold(true)}, kitex.Text("Reason for denial (optional):")),
							kitex.TextArea(kitex.TextAreaProps{
								Name:             "deny-feedback-textarea-inline",
								Ref:              textareaRef,
								Value:            feedbackText(),
								Placeholder:      "Type why you are denying this request...",
								PlaceholderStyle: style.S().Foreground(t.Color.Text.Tertiary),
								Style:            textareaStyle,
								OnChange: func(e event.Event) {
									val := ""
									if ie, ok := e.(*event.ChangeEvent); ok {
										val = ie.Value
									} else if ie, ok := e.(*event.InputEvent); ok {
										val = ie.Value
									}
									setFeedbackText(val)
								},
								OnKeyDown: func(e event.Event) {
									ke, ok := e.(*event.KeyEvent)
									if !ok {
										return
									}
									if ke.Code == key.KeyEscape {
										e.PreventDefault()
										e.StopPropagation()
										setIsProvidingFeedback(false)
										IsFeedbackActive = false
										setFeedbackText("")
										mode.Set(mode.Normal)
										return
									}
									if (ke.Code == key.KeyEnter || ke.Text == "\r" || ke.Text == "\n") && (ke.Mod&key.ModShift) == 0 {
										e.PreventDefault()
										e.StopPropagation()
										handleDenyWithFeedback(feedbackText())
										IsFeedbackActive = false
										mode.Set(mode.Normal)
										return
									}
								},
							}),
							kitex.Box(kitex.BoxProps{
								Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
							},
								components.Button(components.ButtonProps{
									Variant:   components.ButtonText,
									Color:     components.ButtonError,
									StartIcon: icon.Error,
									OnClick: func() {
										handleDenyWithFeedback(feedbackText())
										IsFeedbackActive = false
										mode.Set(mode.Normal)
									},
								}, kitex.Text("Submit Denial (Enter)")),
								components.Button(components.ButtonProps{
									Variant: components.ButtonText,
									Color:   components.ButtonPrimary,
									OnClick: func() {
										setIsProvidingFeedback(false)
										setFeedbackText("")
										mode.Set(mode.Normal)
									},
								}, kitex.Text("Cancel (Esc)")),
							),
						)
					}),

					kitex.If(!isProvidingFeedback(), func() kitex.Node {
						return kitex.Fragment(
							AuthorizationHybridSelector(AuthorizationHybridSelectorProps{
								Options:             currReqOptions,
								DirectoryOptions:    currReqDirOptions,
								FocusedItem:         focusedItem(),
								SelectedScopeIndex:  selectedScopeIndex(),
								SelectedOptionIndex: selectedOptionIndex(),
								SelectedDirIndex:    selectedDirIndex(),
								IsActive:            isVisuallyActive,
								AllowedScopes:       getAllowedScopes(req, 0),
								OnSelectVertical:    setFocusedItem,
								OnSelectScope:       setSelectedScopeIndex,
								OnSelectOption:      setSelectedOptionIndex,
								OnSelectDir:         setSelectedDirIndex,
							}),
							// Action Buttons Bar
							kitex.Box(kitex.BoxProps{
								Style: style.S().
									Display(style.DisplayFlex).
									FlexDirection(style.FlexRow).
									Gap(1).
									BorderTop(true, style.SingleBorder(), t.Color.Border.Primary).
									PaddingTop(1).
									MarginTop(1),
							},
								buttons...,
							),
						)
					}),
				)
			}),

			// Overlay Modal
			kitex.If(showPreviewModal(), func() kitex.Node {
				return AuthorizationPreviewModal(AuthorizationPreviewModalProps{
					IsOpen:              true,
					Request:             req,
					FocusedItem:         focusedItem(),
					SelectedScopeIndex:  selectedScopeIndex(),
					SelectedOptionIndex: selectedOptionIndex(),
					SelectedDirIndex:    selectedDirIndex(),
					IsSubmitting:        submitting(),
					OnClose: func() {
						setShowPreviewModal(false)
						setIsProvidingFeedback(false)
						IsFeedbackActive = false
					},
					OnApprove:           handleApprove,
					OnDeny:              handleDeny,
					OnHardCancel:        handleHardCancel,
					OnSelectVertical:    setFocusedItem,
					OnSelectScope:       setSelectedScopeIndex,
					OnSelectOption:      setSelectedOptionIndex,
					OnSelectDir:         setSelectedDirIndex,
					IsProvidingFeedback: isProvidingFeedback(),
					FeedbackText:        feedbackText(),
					OnFeedbackChange:    setFeedbackText,
					OnDenyWithFeedback:  handleDenyWithFeedback,
					OnCancelFeedback: func() {
						setIsProvidingFeedback(false)
						IsFeedbackActive = false
						setFeedbackText("")
					},
					OnStartFeedback: func() {
						setIsProvidingFeedback(true)
						IsFeedbackActive = true
						setFeedbackText("")
					},
				})
			}),

			// Inline Hard Cancel Dialog
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
		),
	)
})

type AuthorizationPreviewModalProps struct {
	IsOpen              bool
	Request             permissions.AuthorizationRequest
	FocusedItem         FocusItem
	SelectedScopeIndex  int
	SelectedOptionIndex int
	SelectedDirIndex    int
	IsSubmitting        bool
	OnClose             func()
	OnApprove           func()
	OnDeny              func()
	OnHardCancel        func()
	OnSelectVertical    func(FocusItem)
	OnSelectScope       func(int)
	OnSelectOption      func(int)
	OnSelectDir         func(int)
	IsProvidingFeedback bool
	FeedbackText        string
	OnFeedbackChange    func(string)
	OnDenyWithFeedback  func(string)
	OnCancelFeedback    func()
	OnStartFeedback     func()
}

var AuthorizationPreviewModal = kitex.FCC("AuthorizationPreviewModal", func(props AuthorizationPreviewModalProps) kitex.Node {
	t := theme.UseTheme()
	localInputRef := kitex.UseRef[dom.Element](nil)
	previewRef := kitex.UseRef[dom.Element](nil)

	kitex.UseEffect(func() {
		if props.IsProvidingFeedback {
			kitex.PostMacro(func() {
				if localInputRef.Current != nil {
					if doc := localInputRef.Current.OwnerDocument(); doc != nil {
						doc.Focus(localInputRef.Current)
					}
				}
			})
		}
	}, []any{props.IsProvidingFeedback})

	kitex.UseEffect(func() {
		if !props.IsProvidingFeedback {
			kitex.PostMacro(func() {
				if previewRef.Current != nil {
					previewRef.Current.SetTabIndex(0)
					if doc := previewRef.Current.OwnerDocument(); doc != nil {
						doc.Focus(previewRef.Current)
					}
				}
			})
		}
	}, []any{props.IsProvidingFeedback})

	textRef := kitex.UseRef("")
	textRef.Current = props.FeedbackText

	req := props.Request

	leftNode := kitex.UseMemo(func() kitex.Node {
		return PreviewPanel(PreviewPanelProps{
			Preview: req.Preview,
			Payload: req.Payload,
			Border:  false,
		})
	}, []any{req.Preview, req.Payload})

	var currReq *permissions.PermissionGrantRequest
	var currReqOptions []permissions.PermissionOption
	var currReqDirOptions []permissions.PermissionOption
	if len(req.GrantRequests) > 0 {
		currReq = &req.GrantRequests[0]
		currReqOptions = currReq.Options
		currReqDirOptions = currReq.DirectoryOptions
	}

	// Refs to bridge latest states to keymap callbacks
	selectedScopeIndexRef := kitex.UseRef(1)
	selectedScopeIndexRef.Current = props.SelectedScopeIndex

	focusedItemRef := kitex.UseRef(FocusItemSession)
	focusedItemRef.Current = props.FocusedItem

	selectedOptionIndexRef := kitex.UseRef(0)
	selectedOptionIndexRef.Current = props.SelectedOptionIndex

	selectedDirIndexRef := kitex.UseRef(0)
	selectedDirIndexRef.Current = props.SelectedDirIndex

	isProvidingFeedbackRef := kitex.UseRef(false)
	isProvidingFeedbackRef.Current = props.IsProvidingFeedback

	// Bind handlers to the persistent static ModalAuthCtrl if this modal is open
	if props.IsOpen {
		ModalAuthCtrl.ActiveToolCallID = req.ToolCallID
		ModalAuthCtrl.MoveDown = func() {
			if props.IsProvidingFeedback {
				return
			}
			items := getVisibleItems(props.SelectedScopeIndex, req, 0)
			currItem := props.FocusedItem
			currIdxItem := 0
			for idx, it := range items {
				if it == currItem {
					currIdxItem = idx
					break
				}
			}
			newIdx := (currIdxItem + 1) % len(items)
			newItem := items[newIdx]
			if props.OnSelectVertical != nil {
				props.OnSelectVertical(newItem)
			}

			var targetScope permissions.PermissionScope
			switch newItem {
			case FocusItemOnce:
				targetScope = permissions.ScopeOnce
			case FocusItemSession, FocusItemSessionCmd:
				targetScope = permissions.ScopeSession
			case FocusItemWorkspace, FocusItemWorkspaceCmd:
				targetScope = permissions.ScopeWorkspace
			case FocusItemGlobal, FocusItemGlobalCmd:
				targetScope = permissions.ScopeGlobal
			}
			if targetScope != "" {
				targetScopeIdx := getScopeIndex(req, 0, targetScope)
				if targetScopeIdx != -1 && props.OnSelectScope != nil {
					props.OnSelectScope(targetScopeIdx)
				}
			}
		}

		ModalAuthCtrl.MoveUp = func() {
			if props.IsProvidingFeedback {
				return
			}
			items := getVisibleItems(props.SelectedScopeIndex, req, 0)
			currItem := props.FocusedItem
			currIdxItem := 0
			for idx, it := range items {
				if it == currItem {
					currIdxItem = idx
					break
				}
			}
			newIdx := (currIdxItem - 1 + len(items)) % len(items)
			newItem := items[newIdx]
			if props.OnSelectVertical != nil {
				props.OnSelectVertical(newItem)
			}

			var targetScope permissions.PermissionScope
			switch newItem {
			case FocusItemOnce:
				targetScope = permissions.ScopeOnce
			case FocusItemSession, FocusItemSessionCmd:
				targetScope = permissions.ScopeSession
			case FocusItemWorkspace, FocusItemWorkspaceCmd:
				targetScope = permissions.ScopeWorkspace
			case FocusItemGlobal, FocusItemGlobalCmd:
				targetScope = permissions.ScopeGlobal
			}
			if targetScope != "" {
				targetScopeIdx := getScopeIndex(req, 0, targetScope)
				if targetScopeIdx != -1 && props.OnSelectScope != nil {
					props.OnSelectScope(targetScopeIdx)
				}
			}
		}

		ModalAuthCtrl.SelectPrevOption = func() {
			if props.IsProvidingFeedback {
				return
			}
			if len(req.GrantRequests) > 0 {
				currReq := req.GrantRequests[0]
				currItem := props.FocusedItem

				switch currItem {
				case FocusItemSessionCmd, FocusItemWorkspaceCmd, FocusItemGlobalCmd:
					optsCount := len(currReq.Options)
					if optsCount > 1 && props.OnSelectOption != nil {
						props.OnSelectOption((props.SelectedOptionIndex - 1 + optsCount) % optsCount)
					}
				case FocusItemDirectory:
					dirOptsCount := len(currReq.DirectoryOptions)
					if dirOptsCount > 1 && props.OnSelectDir != nil {
						props.OnSelectDir((props.SelectedDirIndex - 1 + dirOptsCount) % dirOptsCount)
					}
				}
			}
		}

		ModalAuthCtrl.SelectNextOption = func() {
			if props.IsProvidingFeedback {
				return
			}
			if len(req.GrantRequests) > 0 {
				currReq := req.GrantRequests[0]
				currItem := props.FocusedItem

				switch currItem {
				case FocusItemSessionCmd, FocusItemWorkspaceCmd, FocusItemGlobalCmd:
					optsCount := len(currReq.Options)
					if optsCount > 1 && props.OnSelectOption != nil {
						props.OnSelectOption((props.SelectedOptionIndex + 1) % optsCount)
					}
				case FocusItemDirectory:
					dirOptsCount := len(currReq.DirectoryOptions)
					if dirOptsCount > 1 && props.OnSelectDir != nil {
						props.OnSelectDir((props.SelectedDirIndex + 1) % dirOptsCount)
					}
				}
			}
		}

		ModalAuthCtrl.Approve = func() {
			if props.IsProvidingFeedback {
				return
			}
			if props.OnApprove != nil {
				props.OnApprove()
			}
		}

		ModalAuthCtrl.Deny = func() {
			if props.IsProvidingFeedback {
				return
			}
			if props.OnDeny != nil {
				props.OnDeny()
			}
		}

		ModalAuthCtrl.StartFeedback = func() {
			if props.IsProvidingFeedback {
				return
			}
			IsFeedbackActive = true
			if props.OnStartFeedback != nil {
				props.OnStartFeedback()
			}
		}

		ModalAuthCtrl.ToggleCancelDialog = func() {
			if props.IsProvidingFeedback {
				return
			}
			if props.OnHardCancel != nil {
				props.OnHardCancel()
			}
		}

		ModalAuthCtrl.ScrollDown = func() {
			if previewRef.Current != nil {
				previewRef.Current.ScrollBy(0, 3)
			}
		}

		ModalAuthCtrl.ScrollUp = func() {
			if previewRef.Current != nil {
				previewRef.Current.ScrollBy(0, -3)
			}
		}
	}

	kitex.UseEffectCleanup(func() func() {
		return func() {
			if props.IsOpen {
				if ModalAuthCtrl.ActiveToolCallID == req.ToolCallID {
					ModalAuthCtrl.ActiveToolCallID = ""
					ModalAuthCtrl.MoveDown = nil
					ModalAuthCtrl.MoveUp = nil
					ModalAuthCtrl.SelectPrevOption = nil
					ModalAuthCtrl.SelectNextOption = nil
					ModalAuthCtrl.Approve = nil
					ModalAuthCtrl.Deny = nil
					ModalAuthCtrl.StartFeedback = nil
					ModalAuthCtrl.ToggleCancelDialog = nil
					ModalAuthCtrl.ScrollDown = nil
					ModalAuthCtrl.ScrollUp = nil
				}
			}
		}
	}, []any{props.IsOpen})

	if !props.IsOpen {
		return nil
	}

	return components.Modal(components.ModalProps{
		IsOpen:     props.IsOpen,
		Title:      kitex.Text("Authorization Preview"),
		OnClose:    props.OnClose,
		Attributes: map[string]string{"data-context": "modal:auth"},
		BodyStyle:  style.S().OverflowY(style.OverflowHidden),
		Footer: kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				JustifyContent(style.JustifyBetween).
				AlignItems(style.AlignCenter).
				Width(style.Percent(100)).
				Height(style.Percent(100)),
		},
			// Action Buttons
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				components.Button(components.ButtonProps{
					Variant:   components.ButtonText,
					Color:     components.ButtonSuccess,
					StartIcon: icon.Check,
					OnClick:   props.OnApprove,
				}, kitex.Text("Allow (Enter)")),
				components.Button(components.ButtonProps{
					Variant:   components.ButtonText,
					Color:     components.ButtonError,
					StartIcon: icon.Error,
					OnClick:   props.OnDeny,
				}, kitex.Text("Deny (d)")),
				components.Button(components.ButtonProps{
					Variant:   components.ButtonText,
					Color:     components.ButtonError,
					StartIcon: icon.Pencil,
					OnClick:   props.OnStartFeedback,
				}, kitex.Text("Deny with Feedback (D)")),
				components.Button(components.ButtonProps{
					Variant:   components.ButtonText,
					Color:     components.ButtonError,
					StartIcon: icon.Exit,
					OnClick:   props.OnHardCancel,
				}, kitex.Text("Hard Cancel (x)")),
			),

			// Hint Bar
			kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(t.Color.Text.Secondary),
			},
				renderHint(t, "j/k", "navigate"),
				kitex.Text(" · "),
				renderHint(t, "h/l", "select"),
				kitex.Text(" · "),
				renderHint(t, "d", "deny"),
				kitex.Text(" · "),
				renderHint(t, "D", "deny with feedback"),
				kitex.Text(" · "),
				renderHint(t, "Ctrl+C", "hard cancel"),
				kitex.Text(" · "),
				renderHint(t, "Esc", "close"),
			),
		),
	},
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				Width(style.Percent(100)).
				Flex(1, 1, style.Cells(0)).
				MinHeight(style.Cells(0)).
				Gap(2),
		},
			// Left Preview Panel
			kitex.Box(kitex.BoxProps{
				Ref: previewRef,
				Attributes: map[string]string{
					"tabindex": "0",
				},
				Style: style.S().
					Flex(7, 7, style.Cells(0)).
					Width(style.Percent(100)).
					MaxWidth(style.Percent(100)).
					MinWidth(style.Percent(0)).
					MinHeight(style.Cells(0)).
					OverflowX(style.OverflowAuto).
					OverflowY(style.OverflowAuto).
					BorderRight(true, style.SingleBorder(), t.Color.Border.Primary).
					PaddingRight(2),
			},
				leftNode,
			),

			// Right Detail Panel
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Flex(5, 5, style.Cells(0)).
					Width(style.Percent(100)).
					MaxWidth(style.Percent(100)).
					MinWidth(style.Percent(0)).
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					MinHeight(style.Cells(0)).
					OverflowX(style.OverflowAuto),
			},
				// Details Body Scroller
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Flex(1, 1, style.Cells(0)).
						MinHeight(style.Cells(0)).
						Width(style.Percent(100)).
						MaxWidth(style.Percent(100)).
						MinWidth(style.Percent(0)).
						OverflowY(style.OverflowAuto),
				},
					kitex.If(currReq != nil, func() kitex.Node {
						var pendingNodes []kitex.Node
						pendingNodes = append(pendingNodes, renderMetadataRow(t, "Request ID", currReq.ID))
						pendingNodes = append(pendingNodes, renderMetadataRow(t, "Description", currReq.Description))

						if len(currReq.Options) > 0 {
							opt := currReq.Options[props.SelectedOptionIndex]
							pendingNodes = append(pendingNodes, renderMetadataRow(t, "Action", string(opt.Action)))
							pendingNodes = append(pendingNodes, renderMetadataRow(t, "Target", opt.Target))
						}

						if len(currReq.DirectoryOptions) > 0 {
							dirOpt := currReq.DirectoryOptions[props.SelectedDirIndex]
							pendingNodes = append(pendingNodes, renderMetadataRow(t, "Scope Dir", dirOpt.Target))
						}

						return kitex.Box(kitex.BoxProps{
							Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0),
						},
							append([]kitex.Node{
								kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).Bold(true)}, kitex.Text("Pending Grants:")),
							}, pendingNodes...)...,
						)
					}),

					// System Warnings (e.g. Destructive)
					kitex.If(len(req.SystemHints) > 0, func() kitex.Node {
						var hintNodes []kitex.Node
						for _, hint := range req.SystemHints {
							hintNodes = append(hintNodes, components.Alert(components.AlertProps{
								Severity: components.AlertWarning,
								ShowIcon: true,
								Style:    style.S(),
							}, kitex.Text(hint)))
						}
						return kitex.Box(kitex.BoxProps{
							Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1).MarginBottom(1),
						},
							hintNodes...,
						)
					}),

					// Divider line
					kitex.Box(kitex.BoxProps{
						Style: style.S().BorderBottom(true, style.SingleBorder(), t.Color.Border.Primary).MarginVertical(1),
					}),

					// Scope Hybrid Selector OR Feedback Text Area
					kitex.If(props.IsProvidingFeedback, func() kitex.Node {
						textareaStyle := style.S().
							Width(style.Percent(100)).
							Height(style.Cells(4)).
							Background(color.Transparent).
							Foreground(t.Color.Text.Primary).
							Border(true, style.SingleBorder(), t.Color.Border.Primary).
							Padding(0, 1)

						return kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexColumn).
								Gap(1).
								MarginTop(1),
						},
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).Bold(true)}, kitex.Text("Reason for denial (optional):")),
							kitex.TextArea(kitex.TextAreaProps{
								Name:             "deny-feedback-textarea-modal",
								Value:            props.FeedbackText,
								Placeholder:      "Type why you are denying this request...",
								PlaceholderStyle: style.S().Foreground(t.Color.Text.Tertiary),
								Style:            textareaStyle,
								Ref:              localInputRef,
								OnChange: func(e event.Event) {
									val := ""
									if ie, ok := e.(*event.ChangeEvent); ok {
										val = ie.Value
									} else if ie, ok := e.(*event.InputEvent); ok {
										val = ie.Value
									}
									if props.OnFeedbackChange != nil {
										props.OnFeedbackChange(val)
									}
								},
								OnKeyDown: func(e event.Event) {
									ke, ok := e.(*event.KeyEvent)
									if !ok {
										return
									}
									if ke.Code == key.KeyEscape {
										e.PreventDefault()
										e.StopPropagation()
										if props.OnCancelFeedback != nil {
											props.OnCancelFeedback()
										}
										return
									}
									if (ke.Code == key.KeyEnter || ke.Text == "\r" || ke.Text == "\n") && (ke.Mod&key.ModShift) == 0 {
										e.PreventDefault()
										e.StopPropagation()
										if props.OnDenyWithFeedback != nil {
											props.OnDenyWithFeedback(textRef.Current)
										}
										return
									}
								},
							}),
							kitex.Box(kitex.BoxProps{
								Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).MarginTop(1),
							},
								components.Button(components.ButtonProps{
									Variant:   components.ButtonText,
									Color:     components.ButtonError,
									StartIcon: icon.Error,
									OnClick: func() {
										if props.OnDenyWithFeedback != nil {
											props.OnDenyWithFeedback(textRef.Current)
										}
									},
								}, kitex.Text("Submit Denial (Enter)")),
								components.Button(components.ButtonProps{
									Variant: components.ButtonText,
									Color:   components.ButtonPrimary,
									OnClick: func() {
										if props.OnCancelFeedback != nil {
											props.OnCancelFeedback()
										}
									},
								}, kitex.Text("Cancel (Esc)")),
							),
						)
					}),
					kitex.If(!props.IsProvidingFeedback, func() kitex.Node {
						return AuthorizationHybridSelector(AuthorizationHybridSelectorProps{
							Options:             currReqOptions,
							DirectoryOptions:    currReqDirOptions,
							FocusedItem:         props.FocusedItem,
							SelectedScopeIndex:  props.SelectedScopeIndex,
							SelectedOptionIndex: props.SelectedOptionIndex,
							SelectedDirIndex:    props.SelectedDirIndex,
							IsActive:            true,
							AllowedScopes:       getAllowedScopes(req, 0),
							OnSelectVertical:    props.OnSelectVertical,
							OnSelectScope:       props.OnSelectScope,
							OnSelectOption:      props.OnSelectOption,
							OnSelectDir:         props.OnSelectDir,
						})
					}),
				),
			),
		),
	)
})

func renderHint(t *theme.Scheme, keys, action string) kitex.Node {
	return kitex.Fragment(
		kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary)}, kitex.Text(keys)),
		kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text(" "+action)),
	)
}

func renderMetadataRow(t *theme.Scheme, label, val string) kitex.Node {
	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Gap(1).
			Width(style.Percent(100)).
			MaxWidth(style.Percent(100)).
			MinWidth(style.Percent(0)),
	},
		kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).MinWidth(style.Cells(12))}, kitex.Text(label)),
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Foreground(t.Color.Text.Primary).
				Flex(1, 1, style.Cells(0)).
				Width(style.Percent(100)).
				MaxWidth(style.Percent(100)).
				MinWidth(style.Percent(0)).
				WhiteSpace(style.WhiteSpacePreWrap).
				OverflowWrap(style.OverflowWrapBreakWord),
		}, kitex.Text(val)),
	)
}

func renderIdentity(t *theme.Scheme, req permissions.AuthorizationRequest) kitex.Node {
	return kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn)},
		kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).PaddingBottom(0),
		},
			kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).MinWidth(style.Cells(9))}, kitex.Text("Tool")),
			kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Magenta).Bold(true)}, kitex.Text(req.ToolName)),
		),
	)
}

func formatTargetLabel(opt permissions.PermissionOption) string {
	if opt.Target == "*" {
		return "All"
	}
	if strings.HasPrefix(opt.Target, "http://") || strings.HasPrefix(opt.Target, "https://") {
		return opt.Target
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(opt.Target, home) {
		return "~" + strings.TrimPrefix(opt.Target, home)
	}
	return opt.Target
}

func getAllowedScopes(req permissions.AuthorizationRequest, page int) []permissions.PermissionScope {
	if page > 0 && len(req.GrantRequests) >= page {
		return req.GrantRequests[page-1].AllowedScopes
	}
	if len(req.GrantRequests) > 0 {
		return req.GrantRequests[0].AllowedScopes
	}
	return []permissions.PermissionScope{
		permissions.ScopeOnce,
		permissions.ScopeSession,
		permissions.ScopeWorkspace,
		permissions.ScopeGlobal,
	}
}

func getVisibleItems(scopeIdx int, req permissions.AuthorizationRequest, page int) []FocusItem {
	var items []FocusItem
	allowed := getAllowedScopes(req, page)

	for _, scope := range allowed {
		switch scope {
		case permissions.ScopeOnce:
			items = append(items, FocusItemOnce)
		case permissions.ScopeSession:
			if len(req.GrantRequests) > page && len(req.GrantRequests[page].Options) > 0 {
				items = append(items, FocusItemSessionCmd)
			} else {
				items = append(items, FocusItemSession)
			}
		case permissions.ScopeWorkspace:
			if len(req.GrantRequests) > page && len(req.GrantRequests[page].Options) > 0 {
				items = append(items, FocusItemWorkspaceCmd)
			} else {
				items = append(items, FocusItemWorkspace)
			}
		case permissions.ScopeGlobal:
			if len(req.GrantRequests) > page && len(req.GrantRequests[page].Options) > 0 {
				items = append(items, FocusItemGlobalCmd)
			} else {
				items = append(items, FocusItemGlobal)
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

func getScopeIndex(req permissions.AuthorizationRequest, page int, scope permissions.PermissionScope) int {
	allowed := getAllowedScopes(req, page)
	for idx, s := range allowed {
		if s == scope {
			return idx
		}
	}
	return -1
}
