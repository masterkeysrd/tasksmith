package components

import (
	"fmt"
	"slices"
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

type QuestionPromptProps struct {
	Request  api.QuestionRequest
	OnSubmit func(api.QuestionRequest, api.QuestionResponse, func(error))
}

type questionForm struct {
	step       int
	cursor     int
	answers    []api.QuestionAnswer
	answered   []bool
	editing    bool
	input      string
	canceling  bool
	submitting bool
	err        string
}

func newQuestionForm(req api.QuestionRequest) questionForm {
	f := questionForm{answers: make([]api.QuestionAnswer, len(req.Questions)), answered: make([]bool, len(req.Questions))}
	for i, q := range req.Questions {
		f.answers[i].Question = q.Question
	}
	if len(req.Questions) > 0 && req.Questions[0].Type == string(api.QuestionTypeText) {
		f.editing = true
	}
	return f
}

func questionLabel(q api.Question) string {
	if strings.TrimSpace(q.Label) != "" {
		return q.Label
	}
	words := strings.Fields(q.Question)
	if len(words) > 3 {
		words = words[:3]
	}
	return strings.Join(words, " ")
}

func questionAnswerText(q api.Question, answer api.QuestionAnswer) string {
	if q.Type == string(api.QuestionTypeConfirm) {
		if answer.Confirmed != nil && *answer.Confirmed {
			return "Yes"
		}
		return "No"
	}
	var parts []string
	for _, id := range answer.Selected {
		label := id
		for _, opt := range q.Options {
			if opt.ID == id {
				label = opt.Label
				break
			}
		}
		parts = append(parts, label)
	}
	if strings.TrimSpace(answer.Text) != "" {
		parts = append(parts, answer.Text)
	}
	return strings.Join(parts, ", ")
}

var QuestionPrompt = trail.FC("QuestionPrompt", func(props QuestionPromptProps) trail.Node {
	scheme := theme.UseTheme()
	form, setForm := kitex.UseState(newQuestionForm(props.Request))
	inputRef := kitex.UseRef[dom.Element](nil)
	f := form()
	req := props.Request
	trail.UseFocus(inputRef, trail.FocusOptions{AutoFocus: true, IsActive: f.editing && !f.canceling && !f.submitting})
	if len(req.Questions) == 0 {
		return nil
	}

	update := func(next questionForm) { setForm(next) }
	clone := func() questionForm {
		n := form()
		n.answers = slices.Clone(n.answers)
		n.answered = slices.Clone(n.answered)
		return n
	}
	move := func(n *questionForm, step int) {
		n.step, n.cursor, n.err = step, 0, ""
		n.editing = step < len(req.Questions) && req.Questions[step].Type == string(api.QuestionTypeText)
		if step < len(req.Questions) {
			q := req.Questions[step]
			a := n.answers[step]
			if q.Type == string(api.QuestionTypeSingle) {
				if a.Text != "" {
					n.cursor = len(q.Options)
				} else if len(a.Selected) > 0 {
					for i, option := range q.Options {
						if option.ID == a.Selected[0] {
							n.cursor = i
							break
						}
					}
				}
			} else if q.Type == string(api.QuestionTypeConfirm) && a.Confirmed != nil && !*a.Confirmed {
				n.cursor = 1
			}
		}
		if n.editing {
			n.input = n.answers[step].Text
		}
	}
	submit := func(n questionForm, canceled bool) {
		if props.OnSubmit == nil || n.submitting {
			return
		}
		n.submitting, n.err = true, ""
		update(n)
		response := api.QuestionResponse{Canceled: canceled}
		if !canceled {
			response.Answers = slices.Clone(n.answers)
		}
		props.OnSubmit(req, response, func(err error) {
			if err != nil {
				current := form()
				current.submitting, current.canceling, current.err = false, false, err.Error()
				if current.step < len(req.Questions) && req.Questions[current.step].Type == string(api.QuestionTypeText) {
					current.editing = true
				}
				update(current)
			}
		})
	}
	advance := func(n questionForm) {
		n.answered[n.step] = true
		if len(req.Questions) == 1 {
			submit(n, false)
			return
		}
		for i := range n.answered {
			if !n.answered[i] {
				move(&n, i)
				update(n)
				return
			}
		}
		move(&n, len(req.Questions))
		update(n)
	}
	commitInput := func() {
		n := clone()
		if strings.TrimSpace(n.input) == "" {
			if req.Questions[n.step].Type == string(api.QuestionTypeMulti) {
				n.answers[n.step].Text = ""
				n.editing, n.err = false, ""
				n.cursor = 0
				update(n)
				return
			}
			n.err = "Enter an answer."
			update(n)
			return
		}
		q := req.Questions[n.step]
		n.answers[n.step].Text = strings.TrimSpace(n.input)
		if q.Type == string(api.QuestionTypeSingle) {
			n.answers[n.step].Selected = nil
		}
		n.editing = false
		if q.Type == string(api.QuestionTypeMulti) {
			n.err = ""
			n.cursor = 0
			update(n)
		} else {
			advance(n)
		}
	}

	trail.UseInput(func(input string, k event.KeyEvent) {
		n := clone()
		if n.submitting || n.editing && !n.canceling {
			return
		}
		if n.canceling {
			switch {
			case k.MatchString("esc", "escape", "n"):
				n.canceling = false
				update(n)
			case k.MatchString("y", "enter"):
				submit(n, true)
			}
			return
		}
		if k.MatchString("esc", "escape") {
			n.canceling = true
			update(n)
			return
		}
		if k.MatchString("left", "shift+tab") && n.step > 0 {
			move(&n, n.step-1)
			update(n)
			return
		}
		if k.MatchString("right", "tab") && n.step < len(req.Questions) && len(req.Questions) > 1 {
			if n.answered[n.step] {
				move(&n, n.step+1)
			} else {
				n.err = "Answer this question before continuing."
			}
			update(n)
			return
		}
		if n.step == len(req.Questions) {
			if k.MatchString("enter") {
				for i, answered := range n.answered {
					if !answered {
						move(&n, i)
						n.err = "Answer this question before submitting."
						update(n)
						return
					}
				}
				submit(n, false)
			}
			return
		}
		q := req.Questions[n.step]
		rows := len(q.Options) + 1
		if q.Type == string(api.QuestionTypeConfirm) {
			rows = 2
		}
		switch {
		case k.MatchString("up", "k"):
			if n.cursor > 0 {
				n.cursor--
			}
			update(n)
		case k.MatchString("down", "j"):
			if n.cursor < rows-1 {
				n.cursor++
			}
			update(n)
		case q.Type == string(api.QuestionTypeMulti) && (input == " " || k.MatchString("space")):
			if n.cursor == len(q.Options) {
				n.editing, n.input = true, n.answers[n.step].Text
			} else {
				id := q.Options[n.cursor].ID
				selected := slices.Clone(n.answers[n.step].Selected)
				if slices.Contains(selected, id) {
					selected = slices.DeleteFunc(selected, func(s string) bool { return s == id })
				} else {
					selected = append(selected, id)
				}
				n.answers[n.step].Selected = selected
			}
			n.err = ""
			update(n)
		case k.MatchString("enter"):
			if q.Type == string(api.QuestionTypeConfirm) {
				n.answers[n.step].Confirmed = new(n.cursor == 0)
				advance(n)
			} else if q.Type == string(api.QuestionTypeSingle) {
				if n.cursor == len(q.Options) {
					n.editing, n.input = true, n.answers[n.step].Text
					update(n)
				} else {
					n.answers[n.step].Selected = []string{q.Options[n.cursor].ID}
					n.answers[n.step].Text = ""
					advance(n)
				}
			} else if q.Type == string(api.QuestionTypeMulti) {
				if n.cursor == len(q.Options) {
					n.editing, n.input = true, n.answers[n.step].Text
					update(n)
				} else if len(n.answers[n.step].Selected) == 0 && strings.TrimSpace(n.answers[n.step].Text) == "" {
					n.err = "Select at least one option or enter a custom answer."
					update(n)
				} else {
					advance(n)
				}
			}
		}
	})

	primary := style.S().Foreground(scheme.Color.Text.Primary).WhiteSpace(style.WhiteSpacePreWrap).OverflowWrap(style.OverflowWrapAnywhere)
	secondary := style.S().Foreground(scheme.Color.Text.Secondary).WhiteSpace(style.WhiteSpacePreWrap).OverflowWrap(style.OverflowWrapAnywhere)
	answerStyle := primary.Bold(true)
	titleStyle := style.S().Bold(true).Foreground(scheme.Color.Icon.Primary).WhiteSpace(style.WhiteSpacePreWrap)
	activeTabStyle := style.S().Bold(true).Foreground(scheme.Color.Icon.Primary).WhiteSpace(style.WhiteSpacePre)
	inactiveTabStyle := style.S().Foreground(scheme.Color.Text.Secondary).WhiteSpace(style.WhiteSpacePre)
	line := func(text string, s style.Style) trail.Node {
		return trail.Box(trail.BoxProps{Style: style.S().Width(style.Percent(100))}, trail.Span(trail.SpanProps{Style: s}, trail.Text(text)))
	}
	var children []trail.Node
	title := req.Title
	if title == "" {
		title = "Question"
	}
	children = append(children, line(title, titleStyle))
	if req.Description != "" {
		children = append(children, line(req.Description, secondary))
	}
	if len(req.Questions) > 1 {
		var tabs []trail.Node
		for i, q := range req.Questions {
			if i > 0 {
				tabs = append(tabs, trail.Span(trail.SpanProps{Style: inactiveTabStyle}, trail.Text("  ")))
			}
			label := fmt.Sprintf("%d %s", i+1, questionLabel(q))
			tabStyle := inactiveTabStyle
			if i == f.step {
				label = "[" + label + "]"
				tabStyle = activeTabStyle
			}
			tabs = append(tabs, trail.Span(trail.SpanProps{Style: tabStyle}, trail.Text(label)))
		}
		review, reviewStyle := "Review", inactiveTabStyle
		if f.step == len(req.Questions) {
			review, reviewStyle = "[Review]", activeTabStyle
		}
		tabs = append(tabs, trail.Span(trail.SpanProps{Style: inactiveTabStyle}, trail.Text("  ")),
			trail.Span(trail.SpanProps{Style: reviewStyle}, trail.Text(review)))
		children = append(children, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow).Width(style.Percent(100)).MarginTop(1)}, tabs...))
	}
	children = append(children, trail.Newline(1))
	if f.step == len(req.Questions) {
		children = append(children, line("Review your answers", titleStyle), trail.Newline(1))
		for i, q := range req.Questions {
			children = append(children, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow).Width(style.Percent(100))},
				trail.Span(trail.SpanProps{Style: secondary}, trail.Text(questionLabel(q)+": ")),
				trail.Span(trail.SpanProps{Style: answerStyle}, trail.Text(questionAnswerText(q, f.answers[i]))),
			))
		}
		children = append(children, trail.Newline(1), line("❯ Submit answers", answerStyle))
	} else {
		q := req.Questions[f.step]
		children = append(children, line(q.Question, titleStyle), line(q.Description, secondary), trail.Newline(1))
		if q.Type == string(api.QuestionTypeSingle) || q.Type == string(api.QuestionTypeMulti) || q.Type == string(api.QuestionTypeConfirm) {
			options := q.Options
			if q.Type == string(api.QuestionTypeConfirm) {
				options = []api.QuestionOption{{Label: "Yes"}, {Label: "No"}}
			} else {
				options = append(slices.Clone(options), api.QuestionOption{Label: "Other — type an answer"})
			}
			for i, opt := range options {
				prefix := "  "
				if i == f.cursor {
					prefix = "❯ "
				}
				mark := "○ "
				if q.Type == string(api.QuestionTypeMulti) {
					mark = "[ ] "
					if i < len(q.Options) && slices.Contains(f.answers[f.step].Selected, opt.ID) || i == len(q.Options) && f.answers[f.step].Text != "" {
						mark = "[x] "
					}
				} else if i < len(q.Options) && slices.Contains(f.answers[f.step].Selected, opt.ID) || i == len(q.Options) && q.Type == string(api.QuestionTypeSingle) && f.answers[f.step].Text != "" {
					mark = "● "
				}
				children = append(children, line(prefix+mark+opt.Label, answerStyle))
				if opt.Description != "" {
					children = append(children, line("    "+opt.Description, secondary))
				}
			}
		}
		if f.editing && !f.canceling {
			children = append(children, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow).Width(style.Percent(100))},
				trail.Span(trail.SpanProps{Style: primary}, trail.Text("Answer: ")),
				kitex.Input(kitex.InputProps{Ref: inputRef, Value: f.input, Style: answerStyle.Flex(1),
					OnChange: func(e event.Event) {
						if ie, ok := e.(*event.InputEvent); ok {
							n := form()
							n.input = ie.Value
							setForm(n)
						}
					},
					OnKeyDown: func(e event.Event) {
						if ke, ok := e.(*event.KeyEvent); ok {
							switch {
							case ke.MatchString("enter"):
								e.PreventDefault()
								e.StopPropagation()
								commitInput()
							case ke.MatchString("tab") && !ke.MatchString("shift+tab") && q.Type == string(api.QuestionTypeText) && len(req.Questions) > 1:
								e.PreventDefault()
								e.StopPropagation()
								n := clone()
								if strings.TrimSpace(n.input) == "" {
									n.err = "Enter an answer."
								} else {
									n.answers[n.step].Text = strings.TrimSpace(n.input)
									n.answered[n.step] = true
									move(&n, n.step+1)
									if n.step == len(req.Questions) {
										for i, answered := range n.answered {
											if !answered {
												move(&n, i)
												n.err = "Answer this question before continuing."
												break
											}
										}
									}
								}
								setForm(n)
							case ke.MatchString("shift+tab") && q.Type == string(api.QuestionTypeText) && f.step > 0:
								e.PreventDefault()
								e.StopPropagation()
								n := clone()
								n.answers[n.step].Text = n.input
								move(&n, n.step-1)
								setForm(n)
							case ke.MatchString("esc", "escape"):
								e.PreventDefault()
								e.StopPropagation()
								n := form()
								if q.Type == string(api.QuestionTypeText) {
									n.canceling = true
								} else {
									n.editing = false
								}
								setForm(n)
							}
						}
					},
				}),
			))
		}
	}
	if f.canceling {
		children = append(children, line("Cancel this question? y/enter: yes  n/esc: no", titleStyle))
	}
	if f.submitting {
		children = append(children, line("Submitting…", secondary))
	}
	if f.err != "" {
		children = append(children, line(f.err, style.S().Foreground(scheme.Color.Text.Error)))
	}
	if !f.submitting && !f.canceling {
		bindings := []components.KeyBinding{{Key: "↑/↓", Label: "Navigate"}, {Key: "enter", Label: "Next"}, {Key: "esc", Label: "Cancel"}}
		if f.step < len(req.Questions) && req.Questions[f.step].Type == string(api.QuestionTypeMulti) {
			bindings = append(bindings, components.KeyBinding{Key: "space", Label: "Toggle"})
		}
		if len(req.Questions) > 1 {
			backKey := "←/→"
			if f.editing && req.Questions[f.step].Type == string(api.QuestionTypeText) {
				backKey = "shift+tab/tab"
			}
			bindings = append(bindings, components.KeyBinding{Key: backKey, Label: "Questions"})
		}
		children = append(children, components.KeyboardHint(components.KeyboardHintProps{Bindings: bindings}))
	}
	return trail.Box(trail.BoxProps{Style: style.S().Border(style.RoundedBorder().Color(scheme.Color.Border.Default)).Padding(0, 1).Width(style.Percent(100)).FlexDirection(style.FlexColumn)}, children...)
})
