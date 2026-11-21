package components

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// PermissionPromptProps defines the properties for the interactive PermissionPrompt component.
type PermissionPromptProps struct {
	Request       api.PermissionRequest
	OnDecision    func(req api.PermissionRequest, dec api.PermissionDecision)
	OnAcceptEdits func(req api.PermissionRequest)
}

func compactSingleLine(s string, maxRunes int) string {
	return format.CompactSingleLine(s, maxRunes)
}

// PermissionPrompt is a trail functional component rendering a tool authorization request.
var PermissionPrompt = trail.FC("PermissionPrompt", func(props PermissionPromptProps) trail.Node {
	scheme := theme.UseTheme()

	scope, setScope := kitex.UseState(api.ScopeSession)
	feedbackMode, setFeedbackMode := kitex.UseState(false)
	feedbackText, setFeedbackText := kitex.UseState("")

	feedbackInputRef := kitex.UseRef[dom.Element](nil)
	trail.UseFocus(feedbackInputRef, trail.FocusOptions{
		AutoFocus: true,
		IsActive:  feedbackMode(),
	})

	patterns := props.Request.GrantPatterns
	if len(patterns) == 0 {
		if props.Request.ToolName != "" {
			patterns = []string{props.Request.ToolName + "(*)"}
		} else {
			patterns = []string{"*(*)"}
		}
	}

	totalRows := len(patterns) + 2
	if props.OnAcceptEdits != nil {
		totalRows++
	}
	cursor := components.UseCursor(totalRows, 0)

	submitDecision := func(dec api.PermissionDecision) {
		if props.OnDecision != nil {
			props.OnDecision(props.Request, dec)
		}
	}

	trail.UseInput(func(input string, k event.KeyEvent) {
		if feedbackMode() {
			return
		}

		switch {
		case k.MatchString("tab"):
			switch scope() {
			case api.ScopeSession:
				setScope(api.ScopeProject)
			case api.ScopeProject:
				setScope(api.ScopeGlobal)
			default:
				setScope(api.ScopeSession)
			}

		case input == "a" || input == "A" || k.MatchString("a"):
			pat := patterns[0]
			if cursor.Index >= 1 && cursor.Index <= len(patterns) {
				pat = patterns[cursor.Index-1]
			}
			submitDecision(api.PermissionDecision{
				Kind:         api.DecisionAllowSession,
				GrantPattern: pat,
				Scope:        scope(),
			})

		case input == "r" || input == "R" || k.MatchString("r"):
			setFeedbackMode(true)
			setFeedbackText("")
			cursor.Set(totalRows - 1)
		}
	})

	// Styles
	border := style.RoundedBorder().Color(scheme.Color.Border.Default)
	titleStyle := style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Agent.Permission)
	toolStyle := style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Primary)
	dotStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary)
	targetStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary)
	unselectedStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary)
	scopeStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Agent.Permission)

	menuItems := make([]components.ActionMenuItem, 0, totalRows)
	menuItems = append(menuItems, components.ActionMenuItem{
		ID:       "allow_once",
		Label:    "Yes, allow once",
		Shortcut: "y",
	})
	scopeTag := fmt.Sprintf("[%s]", scope())
	for _, pat := range patterns {
		menuItems = append(menuItems, components.ActionMenuItem{
			ID:         "allow_scope:" + pat,
			Label:      "Always allow " + compactSingleLine(pat, 60),
			Badge:      scopeTag,
			BadgeStyle: &scopeStyle,
		})
	}
	if props.OnAcceptEdits != nil {
		menuItems = append(menuItems, components.ActionMenuItem{
			ID:       "accept_edits",
			Label:    "Yes, and switch to accept_edits",
			Shortcut: "e",
		})
	}
	menuItems = append(menuItems, components.ActionMenuItem{
		ID:       "deny",
		Label:    "No, deny",
		Shortcut: "n",
	})

	perm := props.Request

	var boxChildren []trail.Node

	// Row 1 (Title): "Permission required"
	boxChildren = append(boxChildren, trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow),
	}, trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text("Permission required"))))

	if perm.Unsandboxed {
		badgeStyle := style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Error)
		boxChildren = append(boxChildren, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{Style: badgeStyle}, trail.Text("⚠ UNSANDBOXED — runs directly on the host, outside the OS sandbox"))))
	}

	// Row 2 (Subtitle / Target)
	targetText := strings.TrimSpace(perm.Target)
	if targetText == "" {
		targetText = strings.TrimSpace(perm.Summary)
	}
	targetText = compactSingleLine(targetText, 80)

	var subtitleNodes []trail.Node
	if perm.ToolName != "" && targetText != "" {
		subtitleNodes = append(subtitleNodes,
			trail.Span(trail.SpanProps{Style: toolStyle}, trail.Text(perm.ToolName)),
			trail.Span(trail.SpanProps{Style: dotStyle}, trail.Text(" · ")),
			trail.Span(trail.SpanProps{Style: targetStyle}, trail.Text(targetText)),
		)
	} else if targetText != "" {
		subtitleNodes = append(subtitleNodes,
			trail.Span(trail.SpanProps{Style: targetStyle}, trail.Text(targetText)),
		)
	} else if perm.ToolName != "" {
		subtitleNodes = append(subtitleNodes,
			trail.Span(trail.SpanProps{Style: toolStyle}, trail.Text(perm.ToolName)),
		)
	}

	if len(subtitleNodes) > 0 {
		boxChildren = append(boxChildren, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, subtitleNodes...))
	}

	// Unified Diff
	if perm.Diff != "" {
		boxChildren = append(boxChildren, trail.Newline(1), DiffView(DiffProps{
			Text: strings.TrimRight(perm.Diff, "\n"),
			Path: perm.Target,
		}))
	}

	// Option rows & vertically aligned colored [scope]
	boxChildren = append(boxChildren, trail.Newline(1), components.ActionMenu(components.ActionMenuProps{
		Items:           menuItems,
		Selected:        &cursor.Index,
		Disabled:        feedbackMode(),
		UnselectedStyle: &unselectedStyle,
		OnChange:        cursor.Set,
		OnSelect: func(index int, item components.ActionMenuItem) {
			switch item.ID {
			case "allow_once":
				submitDecision(api.PermissionDecision{Kind: api.DecisionAllowOnce})
			case "accept_edits":
				props.OnAcceptEdits(props.Request)
			case "deny":
				submitDecision(api.PermissionDecision{Kind: api.DecisionReject})
			default:
				submitDecision(api.PermissionDecision{
					Kind:         api.DecisionAllowSession,
					GrantPattern: patterns[index-1],
					Scope:        scope(),
				})
			}
		},
	}))

	// Inline feedback input
	if feedbackMode() {
		feedbackPromptStyle := style.S().Bold(true).Foreground(scheme.Color.Agent.Permission)

		boxChildren = append(boxChildren, trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				MarginTop(1).
				Width(style.Percent(100)),
		},
			trail.Span(trail.SpanProps{
				Style: feedbackPromptStyle,
			}, trail.Text("Feedback: ")),
			kitex.Input(kitex.InputProps{
				Ref:   feedbackInputRef,
				Value: feedbackText(),
				Style: style.S().Flex(1),
				OnChange: func(e event.Event) {
					if ie, ok := e.(*event.InputEvent); ok {
						setFeedbackText(ie.Value)
					}
				},
				OnKeyDown: func(e event.Event) {
					if ke, ok := e.(*event.KeyEvent); ok {
						switch {
						case ke.MatchString("enter"):
							e.PreventDefault()
							e.StopPropagation()
							submitDecision(api.PermissionDecision{
								Kind:     api.DecisionReject,
								Feedback: feedbackText(),
							})
						case ke.MatchString("esc", "escape"):
							e.PreventDefault()
							e.StopPropagation()
							setFeedbackMode(false)
						}
					}
				},
			}),
		))
	}

	// Inline KeyboardHint at bottom
	if !feedbackMode() {
		bindings := []components.KeyBinding{
			{Key: "↑/↓", Label: "Navigate"},
			{Key: "tab", Label: "Scope"},
			{Key: "enter", Label: "Confirm"},
			{Key: "r", Label: "Feedback"},
		}
		if props.OnAcceptEdits != nil {
			bindings = append(bindings, components.KeyBinding{Key: "e", Label: "Accept edits"})
		}
		boxChildren = append(boxChildren, components.KeyboardHint(components.KeyboardHintProps{Bindings: bindings}))
	} else {
		boxChildren = append(boxChildren, components.KeyboardHint(components.KeyboardHintProps{
			Bindings: []components.KeyBinding{
				{Key: "enter", Label: "Submit"},
				{Key: "esc", Label: "Cancel"},
			},
		}))
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Padding(0, 1).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	}, boxChildren...)
})
