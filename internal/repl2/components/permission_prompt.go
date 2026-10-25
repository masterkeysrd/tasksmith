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
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// PermissionPromptProps defines the properties for the interactive PermissionPrompt component.
type PermissionPromptProps struct {
	Request    api.PermissionRequest
	OnDecision func(req api.PermissionRequest, dec api.PermissionDecision)
}

type permissionOptionRow struct {
	label    string
	scopeTag string
	decision api.PermissionDecision
}

// PermissionPrompt is a trail functional component rendering a tool authorization request.
var PermissionPrompt = trail.FC("PermissionPrompt", func(props PermissionPromptProps) trail.Node {
	scheme := theme.UseTheme()

	cursor, setCursor := kitex.UseState(0)
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

	totalRows := 1 + len(patterns) + 1

	// Ensure cursor is within bounds
	cur := cursor()
	if cur >= totalRows {
		cur = totalRows - 1
	}
	if cur < 0 {
		cur = 0
	}

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

		case k.MatchString("up", "k"):
			if cur > 0 {
				setCursor(cur - 1)
			}

		case k.MatchString("down", "j"):
			if cur < totalRows-1 {
				setCursor(cur + 1)
			}

		case input == "y" || input == "Y" || k.MatchString("y"):
			submitDecision(api.PermissionDecision{Kind: api.DecisionAllowOnce})

		case input == "a" || input == "A" || k.MatchString("a"):
			pat := patterns[0]
			if cur >= 1 && cur <= len(patterns) {
				pat = patterns[cur-1]
			}
			submitDecision(api.PermissionDecision{
				Kind:         api.DecisionAllowSession,
				GrantPattern: pat,
				Scope:        scope(),
			})

		case input == "n" || input == "N" || k.MatchString("n"):
			submitDecision(api.PermissionDecision{Kind: api.DecisionReject})

		case input == "r" || input == "R" || k.MatchString("r"):
			setFeedbackMode(true)
			setFeedbackText("")
			setCursor(totalRows - 1)

		case k.MatchString("enter"):
			switch {
			case cur == 0:
				submitDecision(api.PermissionDecision{Kind: api.DecisionAllowOnce})
			case cur >= 1 && cur <= len(patterns):
				submitDecision(api.PermissionDecision{
					Kind:         api.DecisionAllowSession,
					GrantPattern: patterns[cur-1],
					Scope:        scope(),
				})
			default:
				submitDecision(api.PermissionDecision{Kind: api.DecisionReject})
			}
		}
	})

	var optionRows []permissionOptionRow

	// Row 0: Yes, allow once
	optionRows = append(optionRows, permissionOptionRow{
		label: "Yes, allow once",
		decision: api.PermissionDecision{
			Kind: api.DecisionAllowOnce,
		},
	})

	// Rows 1 .. len(patterns): Always allow
	maxAllowLen := 0
	for _, pat := range patterns {
		lbl := "Always allow " + pat
		if len(lbl) > maxAllowLen {
			maxAllowLen = len(lbl)
		}
	}

	for _, pat := range patterns {
		optionRows = append(optionRows, permissionOptionRow{
			label:    "Always allow " + pat,
			scopeTag: fmt.Sprintf("[%s]", scope()),
			decision: api.PermissionDecision{
				Kind:         api.DecisionAllowSession,
				GrantPattern: pat,
				Scope:        scope(),
			},
		})
	}

	// Last row: No, deny
	optionRows = append(optionRows, permissionOptionRow{
		label: "No, deny",
		decision: api.PermissionDecision{
			Kind: api.DecisionReject,
		},
	})

	// Styles
	border := style.RoundedBorder()
	if scheme != nil && scheme.Color.Border.Primary != nil {
		border = border.Color(scheme.Color.Border.Primary)
	}

	titleStyle := style.S().Bold(true).WhiteSpace(style.WhiteSpacePre)
	if scheme != nil && scheme.Color.Text.Purple != nil {
		titleStyle = titleStyle.Foreground(scheme.Color.Text.Purple)
	} else if scheme != nil && scheme.Color.Icon.Primary != nil {
		titleStyle = titleStyle.Foreground(scheme.Color.Icon.Primary)
	} else if scheme != nil && scheme.Color.Text.Primary != nil {
		titleStyle = titleStyle.Foreground(scheme.Color.Text.Primary)
	}

	toolStyle := style.S().Bold(true).WhiteSpace(style.WhiteSpacePre)
	if scheme != nil && scheme.Color.Text.Primary != nil {
		toolStyle = toolStyle.Foreground(scheme.Color.Text.Primary)
	}

	dotStyle := style.S().WhiteSpace(style.WhiteSpacePre)
	if scheme != nil && scheme.Color.Text.Secondary != nil {
		dotStyle = dotStyle.Foreground(scheme.Color.Text.Secondary)
	}

	targetStyle := style.S().WhiteSpace(style.WhiteSpacePre)
	if scheme != nil && scheme.Color.Text.Secondary != nil {
		targetStyle = targetStyle.Foreground(scheme.Color.Text.Secondary)
	}

	selectedStyle := style.S().Bold(true).WhiteSpace(style.WhiteSpacePre)
	if scheme != nil && scheme.Color.Icon.Primary != nil {
		selectedStyle = selectedStyle.Foreground(scheme.Color.Icon.Primary)
	} else if scheme != nil && scheme.Color.Text.Primary != nil {
		selectedStyle = selectedStyle.Foreground(scheme.Color.Text.Primary)
	}

	unselectedStyle := style.S().WhiteSpace(style.WhiteSpacePre)
	if scheme != nil && scheme.Color.Text.Secondary != nil {
		unselectedStyle = unselectedStyle.Foreground(scheme.Color.Text.Secondary)
	}

	scopeStyle := style.S().WhiteSpace(style.WhiteSpacePre)
	if scheme != nil && scheme.Color.Surface.Tertiary != nil {
		scopeStyle = scopeStyle.Foreground(scheme.Color.Surface.Tertiary)
	} else if scheme != nil && scheme.Color.Text.Purple != nil {
		scopeStyle = scopeStyle.Foreground(scheme.Color.Text.Purple)
	} else if scheme != nil && scheme.Color.Text.Secondary != nil {
		scopeStyle = scopeStyle.Foreground(scheme.Color.Text.Secondary)
	}

	perm := props.Request

	var boxChildren []trail.Node

	// Row 1 (Title): "Permission required"
	boxChildren = append(boxChildren, trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow),
	}, trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text("Permission required"))))

	// Row 2 (Subtitle / Target)
	targetText := strings.TrimSpace(perm.Target)
	if targetText == "" {
		targetText = strings.TrimSpace(perm.Summary)
	}

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
		}))
	}

	// Option rows & vertically aligned colored [scope]
	boxChildren = append(boxChildren, trail.Newline(1))
	for i, r := range optionRows {
		isSel := i == cur
		prefix := "  "
		rowStyle := unselectedStyle
		if isSel {
			prefix = "❯ "
			rowStyle = selectedStyle
		}

		var rowSpans []trail.Node
		if r.scopeTag != "" {
			padLen := max(maxAllowLen-len(r.label)+3, 1)
			padding := strings.Repeat(" ", padLen)
			rowSpans = append(rowSpans,
				trail.Span(trail.SpanProps{Style: rowStyle}, trail.Text(prefix+r.label+padding)),
				trail.Span(trail.SpanProps{Style: scopeStyle}, trail.Text(r.scopeTag)),
			)
		} else {
			rowSpans = append(rowSpans,
				trail.Span(trail.SpanProps{Style: rowStyle}, trail.Text(prefix+r.label)),
			)
		}
		boxChildren = append(boxChildren, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, rowSpans...))
	}

	// Inline feedback input
	if feedbackMode() {
		feedbackPromptStyle := style.S().Bold(true)
		if scheme != nil && scheme.Color.Text.Purple != nil {
			feedbackPromptStyle = feedbackPromptStyle.Foreground(scheme.Color.Text.Purple)
		} else if scheme != nil && scheme.Color.Text.Primary != nil {
			feedbackPromptStyle = feedbackPromptStyle.Foreground(scheme.Color.Text.Primary)
		}

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
		boxChildren = append(boxChildren, KeyboardHint(KeyboardHintProps{
			Bindings: []KeyBinding{
				{Key: "↑/↓", Label: "Navigate"},
				{Key: "tab", Label: "Scope"},
				{Key: "enter", Label: "Confirm"},
				{Key: "r", Label: "Feedback"},
			},
		}))
	} else {
		boxChildren = append(boxChildren, KeyboardHint(KeyboardHintProps{
			Bindings: []KeyBinding{
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
