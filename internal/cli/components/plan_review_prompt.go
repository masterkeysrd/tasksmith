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
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// PlanReviewPromptProps defines the properties for the interactive plan review
// prompt opened when exit_plan_mode pauses a turn.
type PlanReviewPromptProps struct {
	Request  api.PlanReviewRequest
	OnSubmit func(api.PlanReviewRequest, api.PlanReviewDecision, func(error))
}

type planReviewOption struct {
	id    string
	label string
}

var planReviewOptions = []planReviewOption{
	{id: string(api.ModeAcceptEdits), label: "Approve & auto-accept edits (accept_edits)"},
	{id: string(api.ModeDefault), label: "Approve with standard prompts (default)"},
	{id: string(api.ModeAuto), label: "Approve autonomous (auto)"},
	{id: "keep_planning", label: "Keep planning"},
}

type planReviewState struct {
	cursor     int
	viewing    bool
	scrollPos  int
	editing    bool
	feedback   string
	submitting bool
	err        string
}

// PlanReviewPrompt renders a compact plan approval prompt for exit_plan_mode,
// with a toggleable scrollable Markdown plan viewer.
var PlanReviewPrompt = trail.FC("PlanReviewPrompt", func(props PlanReviewPromptProps) trail.Node {
	scheme := theme.UseTheme()
	state, setState := kitex.UseState(planReviewState{})
	feedbackRef := kitex.UseRef[dom.Element](nil)

	s := state()
	req := props.Request

	trail.UseFocus(feedbackRef, trail.FocusOptions{
		AutoFocus: true,
		IsActive:  s.editing && !s.submitting,
	})

	submit := func(selectedID, feedback string, canceled bool) {
		if props.OnSubmit == nil || s.submitting {
			return
		}
		next := state()
		next.submitting = true
		next.err = ""
		setState(next)

		var dec api.PlanReviewDecision
		if canceled {
			dec.Canceled = true
		} else {
			switch api.PermissionMode(selectedID) {
			case api.ModeAcceptEdits, api.ModeDefault, api.ModeAuto:
				dec.Approved = true
				dec.Mode = api.PermissionMode(selectedID)
			default:
				dec.Approved = false
				dec.Feedback = strings.TrimSpace(feedback)
			}
		}
		props.OnSubmit(req, dec, func(err error) {
			if err != nil {
				cur := state()
				cur.submitting = false
				cur.err = err.Error()
				setState(cur)
			}
		})
	}

	trail.UseInput(func(input string, k event.KeyEvent) {
		cur := state()
		if cur.submitting || cur.editing || cur.viewing {
			return
		}
		switch {
		case input == "p" || input == "P" || k.MatchString("p"):
			cur.viewing = true
			cur.err = ""
			setState(cur)
		case k.MatchString("esc", "escape"):
			submit("", "", true)
		}
	})

	titleStyle := style.S().Bold(true).Foreground(scheme.Color.Icon.Primary).WhiteSpace(style.WhiteSpacePreWrap)
	pathStyle := style.S().Foreground(scheme.Color.Text.Secondary).WhiteSpace(style.WhiteSpacePreWrap).OverflowWrap(style.OverflowWrapAnywhere)
	selectedStyle := style.S().Bold(true).Foreground(scheme.Color.Icon.Primary).WhiteSpace(style.WhiteSpacePreWrap)
	optionStyle := style.S().Foreground(scheme.Color.Text.Primary).WhiteSpace(style.WhiteSpacePreWrap)
	mutedStyle := style.S().Foreground(scheme.Color.Text.Muted).WhiteSpace(style.WhiteSpacePreWrap)

	planPath := req.Path
	planContent := req.Content
	hasContent := strings.TrimSpace(planContent) != ""

	headerNodes := []trail.Node{
		trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text("Plan review")),
	}
	if planPath != "" {
		headerNodes = append(headerNodes,
			trail.Span(trail.SpanProps{Style: pathStyle}, trail.Text(" · "+planPath)),
		)
	}
	if hasContent {
		lineCount := strings.Count(strings.TrimRight(planContent, "\r\n"), "\n") + 1
		headerNodes = append(headerNodes,
			trail.Span(trail.SpanProps{Style: mutedStyle}, trail.Text(fmt.Sprintf("  (%d lines)", lineCount))),
		)
	}

	var children []trail.Node
	if s.viewing {
		closeViewer := func() {
			cur := state()
			cur.viewing = false
			setState(cur)
		}
		previewBorder := style.SingleBorder().Top(true).Bottom(true).Left(false).Right(false).Color(scheme.Color.Border.Default)
		viewportStyle := style.S().Border(previewBorder).Margin(1, 0)
		children = append(children, components.ScrollViewer(components.ScrollViewerProps{
			Header:        headerNodes,
			Markdown:      planContent,
			EmptyText:     "(empty plan file)",
			InitialScroll: s.scrollPos,
			OnScroll: func(pos int) {
				cur := state()
				cur.scrollPos = pos
				setState(cur)
			},
			ViewportStyle: &viewportStyle,
			Disabled:      s.submitting,
			OnClose:       closeViewer,
			Actions: []components.ScrollViewerAction{
				{Keys: []string{"p", "P", "q", "enter"}, Handler: closeViewer},
			},
			Bindings: []components.KeyBinding{
				{Key: "↑/↓ or j/k", Label: "Scroll"},
				{Key: "pgup/pgdn", Label: "Page"},
				{Key: "g/G", Label: "Top/Bottom"},
				{Key: "p/esc", Label: "Back to review"},
			},
		}))
	} else {
		children = append(children,
			trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow).Width(style.Percent(100))}, headerNodes...),
			trail.Box(trail.BoxProps{
				Style: style.S().FlexDirection(style.FlexRow).Width(style.Percent(100)).Margin(1, 0),
			},
				trail.Span(trail.SpanProps{Style: optionStyle}, trail.Text("Approve implementation plan?")),
				trail.Span(trail.SpanProps{Style: mutedStyle}, trail.Text("  (press p to view full plan)")),
			),
		)

		menuItems := make([]components.ActionMenuItem, len(planReviewOptions))
		for i, opt := range planReviewOptions {
			item := components.ActionMenuItem{
				ID:    opt.id,
				Label: opt.label,
			}
			if opt.id == "keep_planning" {
				item.Shortcut = "r"
			}
			menuItems[i] = item
		}
		children = append(children, components.ActionMenu(components.ActionMenuProps{
			Items:    menuItems,
			Selected: &s.cursor,
			Numbered: true,
			Disabled: s.editing || s.submitting,
			OnChange: func(index int) {
				cur := state()
				cur.cursor = index
				setState(cur)
			},
			OnSelect: func(_ int, item components.ActionMenuItem) {
				if item.ID == "keep_planning" {
					cur := state()
					cur.editing = true
					cur.err = ""
					setState(cur)
					return
				}
				submit(item.ID, "", false)
			},
		}))

		if s.editing {
			children = append(children, trail.Box(trail.BoxProps{
				Style: style.S().FlexDirection(style.FlexRow).Width(style.Percent(100)).MarginTop(1),
			},
				trail.Span(trail.SpanProps{Style: selectedStyle}, trail.Text("Feedback (optional): ")),
				kitex.Input(kitex.InputProps{
					Ref:   feedbackRef,
					Value: s.feedback,
					Style: optionStyle.Flex(1),
					OnChange: func(e event.Event) {
						if ie, ok := e.(*event.InputEvent); ok {
							cur := state()
							cur.feedback = ie.Value
							setState(cur)
						}
					},
					OnKeyDown: func(e event.Event) {
						if ke, ok := e.(*event.KeyEvent); ok {
							switch {
							case ke.MatchString("enter"):
								e.PreventDefault()
								e.StopPropagation()
								cur := state()
								submit("keep_planning", cur.feedback, false)
							case ke.MatchString("esc", "escape"):
								e.PreventDefault()
								e.StopPropagation()
								cur := state()
								cur.editing = false
								setState(cur)
							}
						}
					},
				}),
			))
		}

		if s.submitting {
			children = append(children, trail.Box(trail.BoxProps{Style: style.S().Width(style.Percent(100)).MarginTop(1)},
				trail.Span(trail.SpanProps{Style: mutedStyle}, trail.Text("Submitting…")),
			))
		}
		if s.err != "" {
			children = append(children, trail.Box(trail.BoxProps{Style: style.S().Width(style.Percent(100))},
				trail.Span(trail.SpanProps{Style: style.S().Foreground(scheme.Color.Text.Error)}, trail.Text(s.err)),
			))
		}

		if !s.submitting {
			var bindings []components.KeyBinding
			if s.editing {
				bindings = []components.KeyBinding{
					{Key: "enter", Label: "Submit"},
					{Key: "esc", Label: "Back"},
				}
			} else {
				bindings = []components.KeyBinding{
					{Key: "↑/↓", Label: "Navigate"},
					{Key: "1-4/enter", Label: "Select"},
					{Key: "p", Label: "View plan"},
					{Key: "r", Label: "Feedback"},
					{Key: "esc", Label: "Cancel"},
				}
			}
			children = append(children, components.KeyboardHint(components.KeyboardHintProps{Bindings: bindings}))
		}
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(style.RoundedBorder().Color(scheme.Color.Border.Default)).
			Padding(0, 1).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	}, children...)
})
