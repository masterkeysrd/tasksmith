package ui

import (
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// ApprovalProps defines the properties for the permission approval component.
type ApprovalProps struct {
	Request    api.PermissionRequest
	OnDecision func(req api.PermissionRequest, dec api.PermissionDecision)
}

// Approval is a trail functional component managing tool permission authorization decisions,
// inline rejection feedback input, and key shortcuts.
var Approval = trail.FC("Approval", func(props ApprovalProps) trail.Node {
	cursor, setCursor := kitex.UseState(0)
	feedbackMode, setFeedbackMode := kitex.UseState(false)
	feedbackText, setFeedbackText := kitex.UseState("")

	feedbackInputRef := kitex.UseRef[dom.Element](nil)

	kitex.UseLayoutEffect(func() {
		if feedbackMode() && feedbackInputRef.Current != nil {
			if doc := feedbackInputRef.Current.OwnerDocument(); doc != nil {
				doc.Focus(feedbackInputRef.Current)
			}
		}
	}, []any{feedbackMode()})

	trail.UseInput(func(input string, k event.KeyEvent) {
		if feedbackMode() {
			return
		}

		cur := cursor()
		switch {
		case input == "y" || input == "Y" || k.MatchString("y"):
			if props.OnDecision != nil {
				props.OnDecision(props.Request, api.PermissionDecision{Kind: api.DecisionAllowOnce})
			}

		case input == "a" || input == "A" || k.MatchString("a"):
			if props.OnDecision != nil {
				props.OnDecision(props.Request, api.PermissionDecision{Kind: api.DecisionAllowSession})
			}

		case input == "n" || input == "N" || k.MatchString("n"):
			if props.OnDecision != nil {
				props.OnDecision(props.Request, api.PermissionDecision{Kind: api.DecisionReject})
			}

		case input == "r" || input == "R" || k.MatchString("r"):
			setFeedbackMode(true)
			setFeedbackText("")
			setCursor(2)

		case k.MatchString("up", "k"):
			if cur > 0 {
				setCursor(cur - 1)
			}

		case k.MatchString("down", "j"):
			if cur < 2 {
				setCursor(cur + 1)
			}

		case k.MatchString("enter"):
			if props.OnDecision != nil {
				var kind api.DecisionKind
				switch cur {
				case 0:
					kind = api.DecisionAllowOnce
				case 1:
					kind = api.DecisionAllowSession
				default:
					kind = api.DecisionReject
				}
				props.OnDecision(props.Request, api.PermissionDecision{Kind: kind})
			}
		}
	})

	perm := props.Request
	var lines []trail.Node

	// Header line
	var headerSpans []trail.Node
	headerSpans = append(headerSpans,
		trail.Span(trail.SpanProps{
			Style: theme.ApprovalTitleStyle,
		}, trail.Text("[Permission Required] ")),
		trail.Span(trail.SpanProps{
			Style: theme.ActionBadgeStyle,
		}, trail.Text(strings.ToUpper(string(perm.Action)))),
		trail.Span(trail.SpanProps{
			Style: theme.StatusBarStyle,
		}, trail.Text(" • ")),
		trail.Span(trail.SpanProps{
			Style: theme.SafetyBadgeStyle(perm.Safety),
		}, trail.Text(strings.ToUpper(string(perm.Safety)))),
	)
	if perm.ToolName != "" {
		headerSpans = append(headerSpans,
			trail.Span(trail.SpanProps{
				Style: theme.StatusBarStyle,
			}, trail.Text(" • ")),
			trail.Span(trail.SpanProps{
				Style: theme.ToolCallStyle,
			}, trail.Text(perm.ToolName)),
		)
	}
	lines = append(lines, trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow),
	}, headerSpans...))

	// Summary & Target
	if perm.Summary != "" {
		lines = append(lines, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{
			Style: theme.UserMsgStyle,
		}, trail.Text("Summary: "+perm.Summary))))
	}
	if perm.Target != "" {
		lines = append(lines, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{
			Style: theme.StatusBarStyle,
		}, trail.Text("Target:  "+perm.Target))))
	}

	// Diff (if any)
	if perm.Diff != "" {
		lines = append(lines, Diff(DiffProps{Text: strings.TrimRight(perm.Diff, "\n")}))
	}

	// Decision option list
	options := []string{
		"[y] Allow once",
		"[a] Always allow",
		"[n] Deny",
	}
	cur := cursor()
	for i, opt := range options {
		optStyle := theme.OptionNormalStyle
		prefix := "  "
		if i == cur {
			optStyle = theme.OptionSelectedStyle
			prefix = "❯ "
		}
		lines = append(lines, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{
			Style: optStyle,
		}, trail.Text(prefix+opt))))
	}

	// Bottom action/feedback row
	if feedbackMode() {
		lines = append(lines, trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Width(style.Percent(100)),
		},
			trail.Span(trail.SpanProps{
				Style: theme.ApprovalTitleStyle.Margin(0, 1, 0, 0),
			}, trail.Text("Feedback:")),
			kitex.Input(kitex.InputProps{
				Ref:   feedbackInputRef,
				Value: feedbackText(),
				Style: style.S().Flex(1).Width(style.Percent(100)),
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
							fb := feedbackText()
							setFeedbackMode(false)
							setFeedbackText("")
							if props.OnDecision != nil {
								props.OnDecision(props.Request, api.PermissionDecision{
									Kind:     api.DecisionReject,
									Feedback: fb,
								})
							}

						case ke.MatchString("esc", "escape"):
							e.PreventDefault()
							e.StopPropagation()
							setFeedbackMode(false)
							setFeedbackText("")
						}
					}
				},
			}),
			trail.Span(trail.SpanProps{
				Style: theme.StatusBarStyle,
			}, trail.Text(" (enter to submit, esc to cancel)")),
		))
	} else {
		lines = append(lines, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{
			Style: theme.StatusBarStyle,
		}, trail.Text("↑/↓ or j/k to select • enter to confirm • r to reject with feedback"))))
	}

	return trail.Box(trail.BoxProps{
		Style: theme.ApprovalBoxStyle,
	}, lines...)
})
