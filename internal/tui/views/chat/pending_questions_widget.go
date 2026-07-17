package chat

import (
	"context"
	"fmt"
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type PendingQuestionsWidgetProps struct {
	SessionID        string
	PendingQuestions []api.PendingQuestion
}

var PendingQuestionsWidget = kitex.FC("PendingQuestionsWidget", func(props PendingQuestionsWidgetProps) kitex.Node {
	t := theme.UseTheme()
	if t == nil {
		return nil
	}

	client := tuiapi.UseClient()
	windClient := wind.UseClient()
	currentMode := mode.Use()

	currentStep, setCurrentStep := kitex.UseState(0)
	tempAnswers, setTempAnswers := kitex.UseState(make(map[string]api.QuestionAnswer))
	selectedOpts, setSelectedOpts := kitex.UseState(make(map[string]bool))
	writeInText, setWriteInText := kitex.UseState("")
	focusedIdx, setFocusedIdx := kitex.UseState(0)
	submitting, setSubmitting := kitex.UseState(false)

	outerRef := kitex.UseRef[dom.Element](nil)
	inputRef := kitex.UseRef[dom.Element](nil)

	N := len(props.PendingQuestions)
	step := currentStep()
	var isWriting bool
	if step < N {
		isWriting = focusedIdx() == len(props.PendingQuestions[step].Options)
	}
	isVisuallyActive := currentMode == mode.Normal || isWriting

	// Local helper methods
	handleNext := func() {
		step := currentStep()
		if step >= N {
			return
		}
		q := props.PendingQuestions[step]

		var selected []string
		for opt, sel := range selectedOpts() {
			if sel {
				selected = append(selected, opt)
			}
		}

		currAnswers := tempAnswers()
		newAnswers := make(map[string]api.QuestionAnswer)
		for k, v := range currAnswers {
			newAnswers[k] = v
		}
		newAnswers[q.ToolCallID] = api.QuestionAnswer{
			ToolCallID: q.ToolCallID,
			Selected:   selected,
			WriteIn:    writeInText(),
		}
		setTempAnswers(newAnswers)

		setCurrentStep(step + 1)
	}

	handleBack := func() {
		step := currentStep()
		if step > 0 {
			setCurrentStep(step - 1)
		}
	}

	handleCancel := func() {
		setSubmitting(true)
		promise.New(func(ctx context.Context) (bool, error) {
			_, err := client.CancelTurn(ctx, api.CancelTurnRequest{
				SessionID: props.SessionID,
			})
			return err == nil, err
		}).Then(func(success bool) {
			setSubmitting(false)
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: props.SessionID})
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: props.SessionID})
		}, func(err error) {
			setSubmitting(false)
			log.Error(fmt.Sprintf("Failed to cancel execution: %v", err))
		})
	}

	handleSubmit := func() {
		setSubmitting(true)
		var ansList []api.QuestionAnswer
		for _, q := range props.PendingQuestions {
			if ans, ok := tempAnswers()[q.ToolCallID]; ok {
				ansList = append(ansList, ans)
			}
		}

		promise.New(func(ctx context.Context) (bool, error) {
			_, err := client.SubmitQuestionAnswers(ctx, api.SubmitQuestionAnswersRequest{
				SessionID: props.SessionID,
				Answers:   ansList,
			})
			return err == nil, err
		}).Then(func(success bool) {
			setSubmitting(false)
			windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: props.SessionID})
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: props.SessionID})
		}, func(err error) {
			setSubmitting(false)
			log.Error(fmt.Sprintf("Failed to submit question answers: %v", err))
		})
	}

	// Unmount cleanup
	kitex.UseEffectCleanup(func() func() {
		return func() {
			IsFeedbackActive = false
		}
	}, []any{})

	// Bind handlers to the persistent static ActiveWidgetCtrl if this widget is active
	kitex.UseEffectCleanup(func() func() {
		if !isVisuallyActive {
			return func() {}
		}

		ActiveWidgetCtrl.ActiveToolCallID = "pending_questions"

		ActiveWidgetCtrl.MoveDown = func() {
			if step < N {
				q := props.PendingQuestions[step]
				hasBack := step > 0
				numOptions := len(q.Options)
				var totalFocusable int
				if hasBack {
					totalFocusable = numOptions + 4
				} else {
					totalFocusable = numOptions + 3
				}
				setFocusedIdx((focusedIdx() + 1) % totalFocusable)
			} else {
				totalFocusable := 3
				setFocusedIdx((focusedIdx() + 1) % totalFocusable)
			}
		}

		ActiveWidgetCtrl.MoveUp = func() {
			if step < N {
				q := props.PendingQuestions[step]
				hasBack := step > 0
				numOptions := len(q.Options)
				var totalFocusable int
				if hasBack {
					totalFocusable = numOptions + 4
				} else {
					totalFocusable = numOptions + 3
				}
				setFocusedIdx((focusedIdx() - 1 + totalFocusable) % totalFocusable)
			} else {
				totalFocusable := 3
				setFocusedIdx((focusedIdx() - 1 + totalFocusable) % totalFocusable)
			}
		}

		ActiveWidgetCtrl.SelectPrevOption = func() {
			handleBack()
		}

		ActiveWidgetCtrl.SelectNextOption = func() {
			handleNext()
		}

		ActiveWidgetCtrl.Approve = func() {
			if step < N {
				q := props.PendingQuestions[step]
				hasBack := step > 0
				numOptions := len(q.Options)
				idx := focusedIdx()

				if idx < numOptions {
					handleNext()
				} else if idx == numOptions {
					if inputRef.Current != nil {
						if doc := inputRef.Current.OwnerDocument(); doc != nil {
							doc.Focus(inputRef.Current)
						}
					}
				} else if idx == numOptions+1 {
					handleNext()
				} else if hasBack && idx == numOptions+2 {
					handleBack()
				} else if (hasBack && idx == numOptions+3) || (!hasBack && idx == numOptions+2) {
					handleCancel()
				}
			} else {
				idx := focusedIdx()
				if idx == 0 {
					handleSubmit()
				} else if idx == 1 {
					handleBack()
				} else if idx == 2 {
					handleCancel()
				}
			}
		}

		ActiveWidgetCtrl.ToggleCancelDialog = func() {
			handleCancel()
		}

		return func() {
			if ActiveWidgetCtrl.ActiveToolCallID == "pending_questions" {
				ActiveWidgetCtrl.ActiveToolCallID = ""
				ActiveWidgetCtrl.MoveDown = nil
				ActiveWidgetCtrl.MoveUp = nil
				ActiveWidgetCtrl.SelectPrevOption = nil
				ActiveWidgetCtrl.SelectNextOption = nil
				ActiveWidgetCtrl.Approve = nil
				ActiveWidgetCtrl.ToggleCancelDialog = nil
			}
		}
	}, []any{isVisuallyActive, step, focusedIdx(), selectedOpts()})

	// Stepper rehydration effect
	kitex.UseEffect(func() {
		step := currentStep()
		if step < N {
			q := props.PendingQuestions[step]
			ans, ok := tempAnswers()[q.ToolCallID]
			optsMap := make(map[string]bool)
			writeIn := ""
			if ok {
				for _, sel := range ans.Selected {
					optsMap[sel] = true
				}
				writeIn = ans.WriteIn
			}
			setSelectedOpts(optsMap)
			setWriteInText(writeIn)
			setFocusedIdx(0)
		} else {
			setFocusedIdx(0)
		}

		kitex.PostMacro(func() {
			if outerRef.Current != nil {
				outerRef.Current.SetTabIndex(0)
				if doc := outerRef.Current.OwnerDocument(); doc != nil {
					doc.Focus(outerRef.Current)
				}
			}
		})
	}, []any{currentStep()})

	// Input focus synchronizer
	kitex.UseEffect(func() {
		step := currentStep()
		if step < N {
			q := props.PendingQuestions[step]
			if focusedIdx() == len(q.Options) {
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
						if doc := outerRef.Current.OwnerDocument(); doc != nil {
							doc.Focus(outerRef.Current)
						}
					}
				})
			}
		}
	}, []any{focusedIdx()})

	// Global Key Down Handler
	handleKeyDown := func(e event.Event) {
		ke, ok := e.(*event.KeyEvent)
		if !ok {
			return
		}
		if !isVisuallyActive {
			return
		}
		if currentMode != mode.Normal {
			return
		}

		step := currentStep()
		if step < N {
			q := props.PendingQuestions[step]
			hasBack := step > 0
			numOptions := len(q.Options)

			var totalFocusable int
			if hasBack {
				totalFocusable = numOptions + 4
			} else {
				totalFocusable = numOptions + 3
			}

			if ke.Code == key.KeyDown || ke.Text == "j" || (ke.Code == key.KeyTab && (ke.Mod&key.ModShift) == 0) {
				e.PreventDefault()
				e.StopPropagation()
				nextIdx := (focusedIdx() + 1) % totalFocusable
				setFocusedIdx(nextIdx)
			} else if ke.Code == key.KeyUp || ke.Text == "k" || (ke.Code == key.KeyTab && (ke.Mod&key.ModShift) != 0) {
				e.PreventDefault()
				e.StopPropagation()
				prevIdx := (focusedIdx() - 1 + totalFocusable) % totalFocusable
				setFocusedIdx(prevIdx)
			} else if ke.Code == key.KeySpace {
				e.PreventDefault()
				e.StopPropagation()

				idx := focusedIdx()
				if idx < numOptions {
					opt := q.Options[idx]
					currentSelected := selectedOpts()
					newSelected := make(map[string]bool)
					if q.IsMultiSelect {
						for k, v := range currentSelected {
							newSelected[k] = v
						}
						newSelected[opt] = !currentSelected[opt]
					} else {
						if !currentSelected[opt] {
							newSelected[opt] = true
						}
					}
					setSelectedOpts(newSelected)
				} else if idx == numOptions+1 {
					handleNext()
				} else if hasBack && idx == numOptions+2 {
					handleBack()
				} else if (hasBack && idx == numOptions+3) || (!hasBack && idx == numOptions+2) {
					handleCancel()
				}
			} else if ke.Code == key.KeyEnter || ke.Text == "\r" || ke.Text == "\n" {
				e.PreventDefault()
				e.StopPropagation()

				idx := focusedIdx()
				if idx < numOptions {
					handleNext()
				} else if idx == numOptions {
					if inputRef.Current != nil {
						if doc := inputRef.Current.OwnerDocument(); doc != nil {
							doc.Focus(inputRef.Current)
						}
					}
				} else if idx == numOptions+1 {
					handleNext()
				} else if hasBack && idx == numOptions+2 {
					handleBack()
				} else if (hasBack && idx == numOptions+3) || (!hasBack && idx == numOptions+2) {
					handleCancel()
				}
			} else if ke.Text == "i" {
				idx := focusedIdx()
				if idx == numOptions {
					e.PreventDefault()
					e.StopPropagation()
					if inputRef.Current != nil {
						if doc := inputRef.Current.OwnerDocument(); doc != nil {
							doc.Focus(inputRef.Current)
						}
					}
				}
			} else if ke.Code == key.KeyLeft || ke.Text == "h" || ke.Code == key.KeyBackspace {
				e.PreventDefault()
				e.StopPropagation()
				handleBack()
			} else if ke.Code == key.KeyRight || ke.Text == "l" {
				e.PreventDefault()
				e.StopPropagation()
				handleNext()
			}
		} else {
			// Step N (Confirm Screen)
			totalFocusable := 3
			if ke.Code == key.KeyDown || ke.Text == "j" || (ke.Code == key.KeyTab && (ke.Mod&key.ModShift) == 0) {
				e.PreventDefault()
				e.StopPropagation()
				setFocusedIdx((focusedIdx() + 1) % totalFocusable)
			} else if ke.Code == key.KeyUp || ke.Text == "k" || (ke.Code == key.KeyTab && (ke.Mod&key.ModShift) != 0) {
				e.PreventDefault()
				e.StopPropagation()
				setFocusedIdx((focusedIdx() - 1 + totalFocusable) % totalFocusable)
			} else if ke.Code == key.KeySpace || ke.Code == key.KeyEnter {
				e.PreventDefault()
				e.StopPropagation()

				idx := focusedIdx()
				if idx == 0 {
					handleSubmit()
				} else if idx == 1 {
					handleBack()
				} else if idx == 2 {
					handleCancel()
				}
			} else if ke.Code == key.KeyLeft || ke.Text == "h" || ke.Code == key.KeyBackspace {
				e.PreventDefault()
				e.StopPropagation()
				handleBack()
			}
		}
	}

	borderColor := t.Color.Border.Primary
	if isVisuallyActive {
		borderColor = t.Color.Surface.Info
	}

	containerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100)).
		PaddingHorizontal(1).
		PaddingVertical(0).
		Border(true, style.SingleBorder(), borderColor).
		Background(t.Color.Surface.BaseHover).
		MarginVertical(1)

	headerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		JustifyContent(style.JustifyBetween).
		PaddingBottom(0).
		MarginBottom(1)

	var body kitex.Node
	if step < N {
		q := props.PendingQuestions[step]
		numOptions := len(q.Options)
		hasBack := step > 0

		var optNodes []kitex.Node
		for idx, opt := range q.Options {
			isFocused := focusedIdx() == idx
			isSelected := selectedOpts()[opt]

			radioIcon := "( )"
			if q.IsMultiSelect {
				radioIcon = "[ ]"
				if isSelected {
					radioIcon = "[x]"
				}
			} else {
				if isSelected {
					radioIcon = "(●)"
				}
			}

			optStyle := style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Gap(1).
				Padding(0, 1).
				MarginVertical(0)

			if isFocused {
				optStyle = optStyle.
					Background(t.Color.Surface.BaseFocus).
					Foreground(t.Color.Surface.Info)
			} else if isSelected {
				optStyle = optStyle.Foreground(t.Color.Text.Primary)
			} else {
				optStyle = optStyle.Foreground(t.Color.Text.Secondary)
			}

			optNodes = append(optNodes, kitex.Box(kitex.BoxProps{
				Style: optStyle,
				OnClick: func(e event.Event) {
					setFocusedIdx(idx)
					currentSelected := selectedOpts()
					newSelected := make(map[string]bool)
					if q.IsMultiSelect {
						for k, v := range currentSelected {
							newSelected[k] = v
						}
						newSelected[opt] = !currentSelected[opt]
					} else {
						if !currentSelected[opt] {
							newSelected[opt] = true
						}
					}
					setSelectedOpts(newSelected)
				},
			},
				kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(radioIcon)),
				kitex.Span(kitex.SpanProps{}, kitex.Text(opt)),
			))
		}

		isWriteInFocused := focusedIdx() == numOptions
		writeInLabelStyle := style.S().Foreground(t.Color.Text.Secondary).MarginBottom(0)
		if isWriteInFocused {
			writeInLabelStyle = writeInLabelStyle.Foreground(t.Color.Surface.Info).Bold(true)
		}

		nextFocused := focusedIdx() == numOptions+1
		backFocused := hasBack && focusedIdx() == numOptions+2
		cancelFocused := (hasBack && focusedIdx() == numOptions+3) || (!hasBack && focusedIdx() == numOptions+2)

		body = kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1)},
			kitex.Span(kitex.SpanProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Primary).MarginBottom(0)}, kitex.Text(q.Question)),
			kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0)}, optNodes...),

			kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).MarginTop(0)},
				kitex.Span(kitex.SpanProps{Style: writeInLabelStyle}, kitex.Text("Write-in Answer:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).Gap(0).MarginTop(0)},
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info).Bold(isWriteInFocused).MarginRight(0)}, kitex.Text(">")),
					components.Input(components.InputProps{
						Value:       writeInText(),
						Placeholder: "Type custom answer here...",
						Variant:     components.InputOutline,
						Color:       components.InputInfo,
						Style:       style.S().Border(false),
						OnChange:    setWriteInText,
						Ref:         inputRef,
						OnFocus: func() {
							IsFeedbackActive = true
							mode.Set(mode.Insert)
						},
						OnBlur: func() {
							IsFeedbackActive = false
							mode.Set(mode.Normal)
						},
						OnKeyDown: func(e event.Event) {
							ke, ok := e.(*event.KeyEvent)
							if !ok {
								return
							}
							if ke.Code == key.KeyUp {
								e.PreventDefault()
								e.StopPropagation()
								setFocusedIdx(numOptions - 1)
							} else if ke.Code == key.KeyDown {
								e.PreventDefault()
								e.StopPropagation()
								setFocusedIdx(numOptions + 1)
							} else if ke.Code == key.KeyTab && (ke.Mod&key.ModShift) == 0 {
								e.PreventDefault()
								e.StopPropagation()
								setFocusedIdx(numOptions + 1)
							} else if ke.Code == key.KeyTab && (ke.Mod&key.ModShift) != 0 {
								e.PreventDefault()
								e.StopPropagation()
								setFocusedIdx(numOptions - 1)
							} else if ke.Code == key.KeyEscape {
								e.PreventDefault()
								e.StopPropagation()
								mode.Set(mode.Normal)
								if outerRef.Current != nil {
									if doc := outerRef.Current.OwnerDocument(); doc != nil {
										doc.Focus(outerRef.Current)
									}
								}
							} else if ke.Code == key.KeyEnter || ke.Text == "\r" || ke.Text == "\n" {
								e.PreventDefault()
								e.StopPropagation()
								handleNext()
							}
						},
					}),
				),
			),

			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).MarginTop(0),
			},
				components.Button(components.ButtonProps{
					Variant: components.ButtonSolid,
					Color:   components.ButtonInfo,
					Active:  nextFocused,
					OnClick: handleNext,
				}, kitex.Text("Next Question (Enter)")),
				kitex.If(hasBack, func() kitex.Node {
					return components.Button(components.ButtonProps{
						Variant: components.ButtonText,
						Color:   components.ButtonSecondary,
						Active:  backFocused,
						OnClick: handleBack,
					}, kitex.Text("Back"))
				}),
				components.Button(components.ButtonProps{
					Variant:   components.ButtonText,
					Color:     components.ButtonError,
					Active:    cancelFocused,
					StartIcon: icon.Error,
					OnClick:   handleCancel,
				}, kitex.Text("Cancel Execution")),
			),
		)
	} else {
		// Review / Confirm Screen
		var summaryNodes []kitex.Node
		for idx, q := range props.PendingQuestions {
			ans := tempAnswers()[q.ToolCallID]
			var ansParts []string
			if len(ans.Selected) > 0 {
				ansParts = append(ansParts, strings.Join(ans.Selected, ", "))
			}
			if ans.WriteIn != "" {
				ansParts = append(ansParts, fmt.Sprintf("Write-in: %q", ans.WriteIn))
			}
			ansText := strings.Join(ansParts, " | ")
			if ansText == "" {
				ansText = "(No answer)"
			}

			summaryNodes = append(summaryNodes, kitex.Box(kitex.BoxProps{
				Style: style.S().MarginBottom(1),
			},
				kitex.Span(kitex.SpanProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary)}, kitex.Text(fmt.Sprintf("%d. %s", idx+1, q.Question))),
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text("  "+ansText)),
			))
		}

		submitFocused := focusedIdx() == 0
		backFocused := focusedIdx() == 1
		cancelFocused := focusedIdx() == 2

		body = kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1)},
			kitex.Span(kitex.SpanProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Primary).MarginBottom(0)}, kitex.Text("Review and Confirm Answers:")),
			kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn)}, summaryNodes...),
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).MarginTop(0),
			},
				components.Button(components.ButtonProps{
					Variant:   components.ButtonSolid,
					Color:     components.ButtonSuccess,
					Active:    submitFocused,
					StartIcon: icon.Checkmark,
					OnClick:   handleSubmit,
				}, kitex.Text("Confirm & Submit (Enter)")),
				components.Button(components.ButtonProps{
					Variant: components.ButtonText,
					Color:   components.ButtonSecondary,
					Active:  backFocused,
					OnClick: handleBack,
				}, kitex.Text("Back")),
				components.Button(components.ButtonProps{
					Variant:   components.ButtonText,
					Color:     components.ButtonError,
					Active:    cancelFocused,
					StartIcon: icon.Error,
					OnClick:   handleCancel,
				}, kitex.Text("Cancel Execution")),
			),
		)
	}

	stepTitle := "Clarification Required"
	if N > 1 {
		if step < N {
			stepTitle = fmt.Sprintf("Clarification (%d/%d)", step+1, N)
		} else {
			stepTitle = "Confirm Clarifications"
		}
	}

	var statusText string
	var statusColor color.Color
	if isVisuallyActive {
		statusText = "● ACTIVE"
		statusColor = t.Color.Surface.Info
	} else {
		statusText = "● INACTIVE"
		statusColor = t.Color.Text.Tertiary
	}

	return kitex.Box(kitex.BoxProps{
		Ref:       outerRef,
		Style:     containerStyle,
		OnKeyDown: handleKeyDown,
	},
		kitex.Box(kitex.BoxProps{Style: headerStyle},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Gap(1),
			},
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(statusColor)}, icon.Warning),
				kitex.Span(kitex.SpanProps{Style: style.S().Bold(true).Foreground(statusColor)}, kitex.Text(stepTitle)),
			),
			kitex.Span(kitex.SpanProps{Style: style.S().Bold(true).Foreground(statusColor)}, kitex.Text(statusText)),
		),
		kitex.If(submitting(), func() kitex.Node {
			return kitex.Box(kitex.BoxProps{
				Style: style.S().Padding(2).AlignItems(style.AlignCenter).JustifyContent(style.JustifyCenter),
			},
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse()),
				kitex.Span(kitex.SpanProps{Style: style.S().Bold(true).MarginTop(1)}, kitex.Text("Submitting...")),
			)
		}),
		kitex.If(!submitting(), func() kitex.Node {
			return body
		}),
	)
})
