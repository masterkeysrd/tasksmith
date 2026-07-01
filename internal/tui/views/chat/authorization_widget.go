package chat

import (
	"fmt"
	"image/color"
	"os"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
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

type AuthorizationWidgetProps struct {
	Request             permissions.AuthorizationRequest
	CurrentPageIndex    int
	FocusedItem         FocusItem
	SelectedScopeIndex  int            // 0 = Once, 1 = Session, 2 = Workspace, 3 = Global
	SelectedOptions     map[string]int // Maps GrantRequestID -> OptionIndex
	SelectedDirs        map[string]int // Maps GrantRequestID -> Directory Option Index
	OnPreview           func()
	IsActive            bool
	IsFocused           bool
	IsDecided           bool
	Decision            permissions.AuthorizationDecision // valid when IsDecided == true
	IsSubmitting        bool                              // true while the batch API call is in flight
	OnSelectVertical    func(FocusItem)
	OnSelectScope       func(int)
	OnSelectOption      func(int)
	OnSelectDir         func(int)
	OnApprove           func()
	OnDeny              func()
	OnHardCancel        func()
	IsProvidingFeedback bool
	FeedbackText        string
	OnFeedbackChange    func(string)
	OnDenyWithFeedback  func(string)
	OnCancelFeedback    func()
	OnStartFeedback     func()
}

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
		var rowFg color.Color = t.Color.Text.Secondary
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

				pillStyle := style.S().MarginRight(2)
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
					AlignItems(style.AlignCenter).
					PaddingLeft(4),
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

			pillStyle := style.S().MarginRight(2)
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
				PaddingLeft(1),
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
					AlignItems(style.AlignCenter).
					PaddingLeft(2),
			}, dirPills...),
		)
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Gap(0),
	},
		append(rows, kitex.If(dirSection != nil, func() kitex.Node { return dirSection }))...,
	)
})

var AuthorizationWidget = kitex.FC("AuthorizationWidget", func(props AuthorizationWidgetProps) kitex.Node {
	t := theme.UseTheme()
	if t == nil {
		return nil
	}

	localInputRef := kitex.CreateRef[dom.Element]()
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

	textRef := kitex.UseRef("")
	textRef.Current = props.FeedbackText

	req := props.Request
	warningColor := color.Color(color.RGBA{R: 224, G: 153, B: 36, A: 255})

	borderColor := t.Color.Border.Primary
	if props.IsDecided {
		if props.Decision.Approved {
			borderColor = t.Color.Surface.Success
		} else {
			borderColor = t.Color.Surface.Error
		}
	} else if props.IsActive {
		if props.IsFocused {
			borderColor = t.Color.Surface.Info
		} else {
			borderColor = t.Color.Border.Primary
		}
	}

	containerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100)).
		Border(true, style.SingleBorder(), borderColor).
		Background(t.Color.Surface.BaseHover)

	titleColor := t.Color.Text.Secondary
	if props.IsActive && props.IsFocused {
		titleColor = warningColor
	}

	// 1. Decided State
	if props.IsDecided {
		statusText := "APPROVED"
		statusCol := t.Color.Surface.Success
		if !props.Decision.Approved {
			statusText = "DENIED"
			statusCol = t.Color.Surface.Error
		}

		return kitex.Box(kitex.BoxProps{Style: containerStyle},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					PaddingTop(0).
					PaddingHorizontal(1).
					PaddingBottom(1).
					Gap(1).
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn),
			},
				// Header row
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).JustifyContent(style.JustifyBetween),
				},
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Authorization request has been recorded.")),
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(statusCol).Bold(true)}, kitex.Text(statusText)),
				),
				// Identity
				renderIdentity(t, req),
				// Decision summary for each grant
				kitex.If(props.Decision.Approved, func() kitex.Node {
					var decisionNodes []kitex.Node
					scopeNames := []string{"Once", "Session", "Workspace", "Global"}
					scopeName := string(props.Decision.Scope)
					for _, name := range scopeNames {
						if strings.EqualFold(name, string(props.Decision.Scope)) {
							scopeName = name
							break
						}
					}

					for _, dec := range props.Decision.GrantDecisions {
						summary := scopeName + " · " + dec.SelectedTarget
						if dec.AllowedDirectory != "" && dec.AllowedDirectory != "*" {
							summary += " (in " + dec.AllowedDirectory + ")"
						}
						decisionNodes = append(decisionNodes, kitex.Span(kitex.SpanProps{
							Style: style.S().Foreground(t.Color.Text.Tertiary).Italic(true),
						}, kitex.Text("+ "+summary)))
					}

					if len(decisionNodes) == 0 {
						summary := scopeName
						decisionNodes = append(decisionNodes, kitex.Span(kitex.SpanProps{
							Style: style.S().Foreground(t.Color.Text.Tertiary).Italic(true),
						}, kitex.Text("+ "+summary)))
					}

					return kitex.Box(kitex.BoxProps{
						Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0),
					}, decisionNodes...)
				}),
				kitex.If(props.IsSubmitting, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{
						Style: style.S().Foreground(t.Color.Text.Tertiary).Italic(true).MarginTop(1),
					}, kitex.Text("Sending..."))
				}),
			),
		)
	}

	// 2. Interactive Wizard Flow
	totalPages := len(req.GrantRequests)
	if totalPages == 0 {
		totalPages = 1
	}

	currentPage := props.CurrentPageIndex
	if currentPage >= totalPages {
		currentPage = 0
	}

	stateText := "○ QUEUED"
	stateCol := t.Color.Text.Tertiary
	if props.IsActive {
		if props.IsFocused {
			stateText = fmt.Sprintf("● ACTIVE [%d/%d]", currentPage+1, totalPages)
			stateCol = t.Color.Surface.Info
		} else {
			stateText = "○ UNFOCUSED"
		}
	}

	var currReq *permissions.PermissionGrantRequest
	var currReqOptions []permissions.PermissionOption
	var currReqDirOptions []permissions.PermissionOption
	if len(req.GrantRequests) > 0 {
		currReq = &req.GrantRequests[currentPage]
		currReqOptions = currReq.Options
		currReqDirOptions = currReq.DirectoryOptions
	}

	return kitex.Box(kitex.BoxProps{Style: containerStyle},
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				PaddingTop(0).
				PaddingHorizontal(1).
				PaddingBottom(1).
				Width(style.Percent(100)),
		},
			// Header
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).JustifyContent(style.JustifyBetween).PaddingBottom(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1)},
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(titleColor)}, icon.Alert),
					kitex.Span(kitex.SpanProps{Style: style.S().Bold(true).Foreground(titleColor)}, kitex.Text("Authorization Required")),
				),
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(stateCol).Bold(props.IsFocused)}, kitex.Text(stateText)),
			),

			// Identity details
			kitex.Box(kitex.BoxProps{Style: style.S().MarginTop(0)}, renderIdentity(t, req)),

			// Context (surfacing req.Description - tool hint or default)
			kitex.If(req.Description != "", func() kitex.Node {
				var actionDesc string
				if currReq != nil {
					actionDesc = currReq.Description
				}
				if strings.Contains(strings.ToLower(actionDesc), strings.ToLower(req.Description)) ||
					strings.Contains(strings.ToLower(req.Description), strings.ToLower(actionDesc)) {
					return nil
				}

				return kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).MarginBottom(1),
				},
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).MinWidth(style.Cells(9))}, kitex.Text("Context")),
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Italic(true)}, kitex.Text(req.Description)),
				)
			}),

			// Action (current page grant details)
			kitex.If(currReq != nil, func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).MarginBottom(1),
				},
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).MinWidth(style.Cells(9))}, kitex.Text("Action")),
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Purple).Bold(true)}, kitex.Text(currReq.Description)),
				)
			}),

			// Pending Grants (if we are on page > 0)
			kitex.If(currentPage > 0 && len(req.GrantRequests) > 0, func() kitex.Node {
				var pendingNodes []kitex.Node
				scopeNames := getScopeNames(props.Request, currentPage)
				scopeName := "Once"
				if props.SelectedScopeIndex >= 0 && props.SelectedScopeIndex < len(scopeNames) {
					scopeName = scopeNames[props.SelectedScopeIndex]
				}

				for p := 0; p < currentPage; p++ {
					gr := req.GrantRequests[p]
					optIdx := props.SelectedOptions[gr.ID]
					if optIdx < 0 || optIdx >= len(gr.Options) {
						optIdx = 0
					}
					target := "*"
					if len(gr.Options) > 0 {
						target = gr.Options[optIdx].Target
					}

					dirIdx := props.SelectedDirs[gr.ID]
					if dirIdx < 0 || dirIdx >= len(gr.DirectoryOptions) {
						dirIdx = 0
					}
					dirTarget := "*"
					if len(gr.DirectoryOptions) > 0 {
						dirTarget = gr.DirectoryOptions[dirIdx].Target
					}

					summary := fmt.Sprintf("+ %s · %s", scopeName, target)
					if dirTarget != "" && dirTarget != "*" {
						summary += fmt.Sprintf(" (in %s)", dirTarget)
					}

					pendingNodes = append(pendingNodes, kitex.Span(kitex.SpanProps{
						Style: style.S().Foreground(t.Color.Surface.Success),
					}, kitex.Text(summary)))
				}

				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Gap(0).
						MarginBottom(1).
						Padding(0, 1).
						Border(true, style.SingleBorder(), t.Color.Border.Primary),
				},
					append([]kitex.Node{
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).Bold(true)}, kitex.Text("Pending Grants:")),
					}, pendingNodes...)...,
				)
			}),

			// System Hints / Warnings
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
			kitex.If(props.IsActive, func() kitex.Node {
				if !props.IsFocused {
					// Collapsed summary for unfocused state
					scopeNames := getScopeNames(props.Request, currentPage)
					summary := "Once"
					if props.SelectedScopeIndex >= 0 && props.SelectedScopeIndex < len(scopeNames) {
						summary = scopeNames[props.SelectedScopeIndex]
					}
					if currReq != nil && len(currReq.Options) > props.SelectedOptions[currReq.ID] && props.SelectedScopeIndex > 0 {
						summary += " · [" + formatTargetLabel(currReq.Options[props.SelectedOptions[currReq.ID]]) + "]"
					}
					return kitex.Box(kitex.BoxProps{Style: style.S().MarginTop(1)},
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text("● "+summary)),
						kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).PaddingTop(1)},
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary)}, kitex.Text("Composer focused")),
							kitex.Text("    "),
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary)}, kitex.Text("[Esc]")),
							kitex.Text(" Focus widget"),
						),
					)
				}

				var activeOptIdx int
				var activeDirIdx int
				if currReq != nil {
					activeOptIdx = props.SelectedOptions[currReq.ID]
					activeDirIdx = props.SelectedDirs[currReq.ID]
				}

				// Buttons Node
				enterLabel := "Allow"
				if currentPage == totalPages-1 {
					enterLabel = "Allow All"
				}

				buttons := []kitex.Node{
					components.Button(components.ButtonProps{
						Variant:   components.ButtonText,
						Color:     components.ButtonSuccess,
						StartIcon: icon.Check,
						OnClick:   props.OnApprove,
					}, kitex.Text(fmt.Sprintf("%s (Enter)", enterLabel))),
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
				}

				// Back Button (if currentPage > 0)
				if currentPage > 0 {
					buttons = append(buttons, components.Button(components.ButtonProps{
						Variant: components.ButtonText,
						Color:   components.ButtonPrimary,
					}, kitex.Text("Prev (b)")))
				}

				if req.Preview != nil {
					buttons = append(buttons, components.Button(components.ButtonProps{
						Variant: components.ButtonText,
						Color:   components.ButtonPrimary,
						OnClick: props.OnPreview,
					}, kitex.Text("Preview (p)")))
				}

				buttonsRow := kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).MarginTop(1),
				}, buttons...)

				// Hint Bar
				hintNode := kitex.Box(kitex.BoxProps{Style: style.S().PaddingTop(1)},
					renderHint(t, "j/k", "navigate"),
					kitex.Text(" · "),
					renderHint(t, "h/l", "select"),
					kitex.If(req.Preview != nil, func() kitex.Node {
						return kitex.Fragment(kitex.Text(" · "), renderHint(t, "p", "preview"))
					}),
					kitex.Text(" · "),
					renderHint(t, "d", "deny"),
					kitex.Text(" · "),
					renderHint(t, "D", "deny with feedback"),
					kitex.Text(" · "),
					renderHint(t, "x", "hard cancel"),
				)

				return kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn)},
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
								Name:             "deny-feedback-textarea",
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
									if ke.Code == key.KeyEnter && (ke.Mod&key.ModShift) == 0 {
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
						return kitex.Fragment(
							kitex.Box(kitex.BoxProps{Style: style.S().PaddingBottom(0).Foreground(t.Color.Text.Primary)}, kitex.Text("Grant permission...")),
							AuthorizationHybridSelector(AuthorizationHybridSelectorProps{
								Options:             currReqOptions,
								DirectoryOptions:    currReqDirOptions,
								FocusedItem:         props.FocusedItem,
								SelectedScopeIndex:  props.SelectedScopeIndex,
								SelectedOptionIndex: activeOptIdx,
								SelectedDirIndex:    activeDirIdx,
								IsActive:            props.IsActive,
								AllowedScopes:       getAllowedScopes(props.Request, currentPage),
								OnSelectVertical:    props.OnSelectVertical,
								OnSelectScope:       props.OnSelectScope,
								OnSelectOption:      props.OnSelectOption,
								OnSelectDir:         props.OnSelectDir,
							}),
							buttonsRow,
							hintNode,
						)
					}),
				)
			}),
		),
	)
})

func renderHint(t *theme.Scheme, keys, action string) kitex.Node {
	return kitex.Fragment(
		kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary)}, kitex.Text(keys)),
		kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text(" "+action)),
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

type AuthorizationPreviewModalProps struct {
	IsOpen              bool
	PendingRequests     []permissions.AuthorizationRequest
	CurrentPendingIndex int
	CurrentPageIndex    int
	FocusedItem         FocusItem
	SelectedScopeIndex  int
	SelectedOptions     map[string]int
	SelectedDirs        map[string]int
	IsSubmitting        bool
	OnClose             func()
	OnApprove           func()
	OnDeny              func()
	OnHardCancel        func()
	OnSelectVertical    func(FocusItem)
	OnSelectScope       func(int)
	OnSelectOption      func(int)
	OnSelectDir         func(int)
	OnSetCurrentPage    func(int)
	IsProvidingFeedback bool
	FeedbackText        string
	OnFeedbackChange    func(string)
	OnDenyWithFeedback  func(string)
	OnCancelFeedback    func()
	OnStartFeedback     func()
}

var AuthorizationPreviewModal = kitex.FCC("AuthorizationPreviewModal", func(props AuthorizationPreviewModalProps) kitex.Node {
	if !props.IsOpen || len(props.PendingRequests) == 0 || props.CurrentPendingIndex >= len(props.PendingRequests) {
		return nil
	}

	t := theme.UseTheme()
	localInputRef := kitex.CreateRef[dom.Element]()
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

	textRef := kitex.UseRef("")
	textRef.Current = props.FeedbackText

	req := props.PendingRequests[props.CurrentPendingIndex]

	leftNode := PreviewPanel(PreviewPanelProps{
		Preview: req.Preview,
		Payload: req.Payload,
		Border:  false,
	})

	// Unified details and selector for the active page
	totalPages := len(req.GrantRequests)
	if totalPages == 0 {
		totalPages = 1
	}
	currentPage := props.CurrentPageIndex
	if currentPage >= totalPages {
		currentPage = 0
	}

	var currReq *permissions.PermissionGrantRequest
	var currReqOptions []permissions.PermissionOption
	var currReqDirOptions []permissions.PermissionOption
	if len(req.GrantRequests) > 0 {
		currReq = &req.GrantRequests[currentPage]
		currReqOptions = currReq.Options
		currReqDirOptions = currReq.DirectoryOptions
	}

	var activeOptIdx int
	var activeDirIdx int
	if currReq != nil {
		activeOptIdx = props.SelectedOptions[currReq.ID]
		activeDirIdx = props.SelectedDirs[currReq.ID]
	}

	enterLabel := "Allow"
	if currentPage == totalPages-1 {
		enterLabel = "Allow All"
	}

	return components.Modal(components.ModalProps{
		IsOpen:  props.IsOpen,
		Title:   kitex.Text("Authorization Preview"),
		OnClose: props.OnClose,
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
				}, kitex.Text(fmt.Sprintf("%s (Enter)", enterLabel))),
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
				kitex.If(currentPage > 0, func() kitex.Node {
					return components.Button(components.ButtonProps{
						Variant: components.ButtonText,
						Color:   components.ButtonPrimary,
						OnClick: func() {
							props.OnSetCurrentPage(currentPage - 1)
						},
					}, kitex.Text("Prev (b)"))
				}),
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
				renderHint(t, "x", "hard cancel"),
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
				Height(style.Percent(100)).
				Gap(2),
		},
			// Left Panel: Code/Diff Preview (no borders)
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
				kitex.Box(kitex.BoxProps{
					Style: style.S().Bold(true).PaddingBottom(1).Foreground(t.Color.Text.Primary),
				}, kitex.Text("Resource Preview:")),
				leftNode,
			),
			// Right Panel: Details & Hybrid Selector (no borders)
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Flex(1, 1, style.Cells(0)).
					MinWidth(style.Cells(0)).
					Height(style.Percent(100)).
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Background(t.Color.Surface.BaseFocus).
					Padding(1).
					Gap(1).
					Overflow(style.OverflowAuto),
			},
				// Step Tracker Header
				kitex.Box(kitex.BoxProps{
					Style: style.S().Bold(true).PaddingBottom(1).Foreground(t.Color.Surface.Info),
				}, kitex.Text(fmt.Sprintf("Authorization Details [Step %d of %d]", currentPage+1, totalPages))),

				// Tool Details
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
				},
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).MinWidth(style.Cells(9))}, kitex.Text("Tool:")),
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Magenta).Bold(true)}, kitex.Text(req.ToolName)),
				),

				// Action Details
				kitex.If(currReq != nil, func() kitex.Node {
					return kitex.Box(kitex.BoxProps{
						Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).PaddingBottom(1),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).MinWidth(style.Cells(9))}, kitex.Text("Action:")),
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Purple).Bold(true)}, kitex.Text(currReq.Description)),
					)
				}),

				// Pending Grants list for page > 0
				kitex.If(currentPage > 0 && len(req.GrantRequests) > 0, func() kitex.Node {
					var pendingNodes []kitex.Node
					scopeNames := getScopeNames(req, currentPage)
					scopeName := "Once"
					if props.SelectedScopeIndex >= 0 && props.SelectedScopeIndex < len(scopeNames) {
						scopeName = scopeNames[props.SelectedScopeIndex]
					}

					for p := 0; p < currentPage; p++ {
						gr := req.GrantRequests[p]
						optIdx := props.SelectedOptions[gr.ID]
						if optIdx < 0 || optIdx >= len(gr.Options) {
							optIdx = 0
						}
						target := "*"
						if len(gr.Options) > 0 {
							target = gr.Options[optIdx].Target
						}

						dirIdx := props.SelectedDirs[gr.ID]
						if dirIdx < 0 || dirIdx >= len(gr.DirectoryOptions) {
							dirIdx = 0
						}
						dirTarget := "*"
						if len(gr.DirectoryOptions) > 0 {
							dirTarget = gr.DirectoryOptions[dirIdx].Target
						}

						summary := fmt.Sprintf("+ %s · %s", scopeName, target)
						if dirTarget != "" && dirTarget != "*" {
							summary += fmt.Sprintf(" (in %s)", dirTarget)
						}

						pendingNodes = append(pendingNodes, kitex.Span(kitex.SpanProps{
							Style: style.S().Foreground(t.Color.Surface.Success),
						}, kitex.Text(summary)))
					}

					return kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexColumn).
							Gap(0).
							MarginBottom(1).
							Padding(0, 1).
							Border(true, style.SingleBorder(), t.Color.Border.Primary),
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
								if ke.Code == key.KeyEnter && (ke.Mod&key.ModShift) == 0 {
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
						SelectedOptionIndex: activeOptIdx,
						SelectedDirIndex:    activeDirIdx,
						IsActive:            true,
						AllowedScopes:       getAllowedScopes(req, currentPage),
						OnSelectVertical:    props.OnSelectVertical,
						OnSelectScope:       props.OnSelectScope,
						OnSelectOption:      props.OnSelectOption,
						OnSelectDir:         props.OnSelectDir,
					})
				}),
			),
		),
	)
})

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

func getScopeNames(req permissions.AuthorizationRequest, page int) []string {
	allowed := getAllowedScopes(req, page)
	names := make([]string, len(allowed))
	for idx, s := range allowed {
		switch s {
		case permissions.ScopeOnce:
			names[idx] = "Once"
		case permissions.ScopeSession:
			names[idx] = "Session"
		case permissions.ScopeWorkspace:
			names[idx] = "Workspace"
		case permissions.ScopeGlobal:
			names[idx] = "Global"
		}
	}
	return names
}
