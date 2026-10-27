package components

import (
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type questionToolProps struct {
	Tool store.ToolPart
}

var questionToolWidget = trail.FC("QuestionToolWidget", func(props questionToolProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	input := parseToolInput[api.AskQuestionInput](tool.Call)
	var output api.AskQuestionOutput
	answered := tool.Response != nil && !tool.Response.IsError
	if answered {
		output = parseToolOutput[api.AskQuestionOutput](tool.Response)
	}
	if tool.Question != nil {
		input.Questions = tool.Question.Request.Questions
		output = api.AskQuestionOutput{Answers: tool.Question.Response.Answers, Canceled: tool.Question.Response.Canceled}
		answered = true
	}
	if len(input.Questions) == 0 && answered {
		for _, answer := range output.Answers {
			input.Questions = append(input.Questions, api.Question{Question: answer.Question})
		}
	}

	heading := "Asking questions:"
	if len(input.Questions) == 1 {
		heading = "Asking question:"
	}
	if answered {
		heading = "Questions asked:"
		if len(input.Questions) == 1 {
			heading = "Question asked:"
		}
	}
	if tool.Response != nil && tool.Response.IsError {
		heading = "Unable to ask questions:"
	} else if !answered && tool.Status == store.ToolInterrupted {
		heading = "Questions interrupted:"
	}

	headerStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Accent).WhiteSpace(style.WhiteSpacePreWrap)
	questionStyle := style.S().Foreground(scheme.Color.Text.Secondary).WhiteSpace(style.WhiteSpacePreWrap).OverflowWrap(style.OverflowWrapAnywhere)
	answerStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Primary).WhiteSpace(style.WhiteSpacePreWrap).OverflowWrap(style.OverflowWrapAnywhere)
	row := func(prefix, text string, textStyle style.Style) trail.Node {
		return trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow).Width(style.Percent(100)).MinWidth(style.Cells(0))},
			trail.Span(trail.SpanProps{Style: style.S().WhiteSpace(style.WhiteSpacePre).Flex(0, 0)}, trail.Text(prefix)),
			trail.Span(trail.SpanProps{Style: textStyle.Flex(1, 1).MinWidth(style.Cells(0))}, trail.Text(text)),
		)
	}
	children := []trail.Node{row("", heading, headerStyle)}
	for i, q := range input.Questions {
		if strings.TrimSpace(q.Question) == "" {
			continue
		}
		children = append(children, row("  ", q.Question, questionStyle))
		if answered && !output.Canceled && i < len(output.Answers) {
			answer := questionAnswerText(q, output.Answers[i])
			if answer != "" {
				children = append(children, row("    > ", answer, answerStyle))
			}
		}
	}
	if answered && output.Canceled {
		children = append(children, row("    > ", "Canceled", answerStyle))
	}
	if err := toolErrorSubline(tool, scheme); err != nil {
		children = append(children, err)
	}
	return trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexColumn).Width(style.Percent(100)).MinWidth(style.Cells(0))}, children...)
})

func QuestionToolRenderer(tool store.ToolPart) trail.Node {
	return questionToolWidget(questionToolProps{Tool: tool})
}
