package components

import (
	"fmt"
	"time"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// StreamingHint registers the streaming cancellation hint in the store while mounted.
var StreamingHint = trail.SimpleFC("StreamingHint", func() trail.Node {
	store.UseHint("streaming • esc to cancel")
	return nil
})

// ActiveStream renders the in-flight conversation messages, animated spinner,
// and pinned task checklist for the current active LLM turn.
var ActiveStream = trail.SimpleFC("ActiveStream", func() trail.Node {
	scheme := theme.UseTheme()

	msgsPtr := store.UseStream(func(s store.StreamState) *[]store.Message {
		return s.Messages
	})
	streaming := store.UseStream(func(s store.StreamState) bool {
		return s.Streaming
	})
	waitingPerm := store.UseStream(func(s store.StreamState) bool {
		return s.WaitingPermission || s.WaitingQuestion
	})
	status := store.UseStream(func(s store.StreamState) api.SessionStatus {
		return s.Status
	})
	pendingCmd := store.UseStream(func(s store.StreamState) string {
		return s.PendingCommand
	})
	todosPtr := store.UseStream(func(s store.StreamState) *[]api.TodoItem {
		return s.Todos
	})
	turn := store.UseStream(func(s store.StreamState) turnStats {
		return turnStats{StartedAt: s.TurnStartedAt, BaseInput: s.BaseInput, BaseOutput: s.BaseOutput, CallInput: s.CallInput, CallOutput: s.CallOutput}
	})
	totals := store.UseUsageTotals()

	var msgs []store.Message
	if msgsPtr != nil {
		msgs = *msgsPtr
	}

	var todos []api.TodoItem
	if todosPtr != nil {
		todos = *todosPtr
	}

	hasIncomplete := hasIncompleteTodos(todos)
	if !streaming && len(msgs) == 0 && pendingCmd == "" && !hasIncomplete {
		return nil
	}

	var children []trail.Node
	if streaming && !waitingPerm {
		children = append(children, StreamingHint())
	}

	for _, msg := range msgs {
		children = append(children, Message(MessageProps{Message: msg}))
	}

	if pendingCmd != "" {
		bashColor := scheme.Color.Mode.Bash
		primaryColor := scheme.Color.Text.Primary
		children = append(children, trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				Width(style.Percent(100)).
				MarginBottom(1),
		},
			trail.Span(trail.SpanProps{
				Style: style.S().Bold(true).Foreground(bashColor).WhiteSpace(style.WhiteSpaceNoWrap).Flex(0, 0),
			}, trail.Text("! ")),
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(primaryColor),
			}, trail.Text(pendingCmd)),
		))
	}

	activeText := activeTodoText(todos)

	if streaming && !waitingPerm {
		spinnerBlock := []trail.Node{
			Spinner(SpinnerProps{Status: status, ActiveText: activeText, StartedAt: turn.StartedAt, Input: turn.input(totals), Output: turn.output(totals)}),
		}
		if hasIncomplete {
			for i, item := range todos {
				prefix := "     "
				if i == 0 {
					prefix = "  ⎿  "
				}
				spinnerBlock = append(spinnerBlock, renderChecklistRow(item, prefix, scheme))
			}
		}
		children = append(children, trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				MarginBottom(1),
		}, spinnerBlock...))
	} else if hasIncomplete {
		var done, inProgress, pending int
		for _, item := range todos {
			switch item.Status {
			case "completed":
				done++
			case "in_progress":
				inProgress++
			case "pending":
				pending++
			}
		}
		headerText := fmt.Sprintf("%d todos (%d done, %d in progress, %d pending)", len(todos), done, inProgress, pending)
		headerNode := trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				Width(style.Percent(100)),
		}, trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Secondary),
		}, trail.Text(headerText)))

		checklistBlock := []trail.Node{headerNode}
		for i, item := range todos {
			prefix := "     "
			if i == 0 {
				prefix = "  ⎿  "
			}
			checklistBlock = append(checklistBlock, renderChecklistRow(item, prefix, scheme))
		}
		children = append(children, trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				MarginBottom(1),
		}, checklistBlock...))
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)).
			MinWidth(style.Cells(0)),
	}, children...)
})

func hasIncompleteTodos(todos []api.TodoItem) bool {
	if len(todos) == 0 {
		return false
	}
	for _, it := range todos {
		if it.Status != "completed" {
			return true
		}
	}
	return false
}

func activeTodoText(todos []api.TodoItem) string {
	for _, it := range todos {
		if it.Status == "in_progress" {
			return it.ActiveText
		}
	}
	return ""
}

func renderChecklistRow(item api.TodoItem, prefix string, scheme *theme.Scheme) trail.Node {
	var (
		marker       string
		markerStyle  style.Style
		contentStyle style.Style
	)
	switch item.Status {
	case "in_progress":
		marker = "◼ "
		markerStyle = style.S().Bold(true).Foreground(scheme.Color.Text.Accent).Flex(0, 0).WhiteSpace(style.WhiteSpacePre)
		contentStyle = style.S().Bold(true).Foreground(scheme.Color.Text.Primary)
	case "completed":
		marker = "✔ "
		markerStyle = style.S().Foreground(scheme.Color.Text.Success).Flex(0, 0).WhiteSpace(style.WhiteSpacePre)
		contentStyle = style.S().Foreground(scheme.Color.Text.Muted)
	case "pending":
		fallthrough
	default:
		marker = "◻ "
		markerStyle = style.S().Foreground(scheme.Color.Text.Secondary).Flex(0, 0).WhiteSpace(style.WhiteSpacePre)
		contentStyle = style.S().Foreground(scheme.Color.Text.Secondary)
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Width(style.Percent(100)),
	},
		trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Secondary).Flex(0, 0).WhiteSpace(style.WhiteSpacePre),
		}, trail.Text(prefix)),
		trail.Span(trail.SpanProps{
			Style: markerStyle,
		}, trail.Text(marker)),
		trail.Span(trail.SpanProps{
			Style: contentStyle,
		}, trail.Text(item.Content)),
	)
}

// turnStats are the spinner statistics of the current turn. The tokens used so
// far are the session's token growth since the turn started plus the running
// totals of the LLM call in flight.
type turnStats struct {
	StartedAt  time.Time
	BaseInput  int64
	BaseOutput int64
	CallInput  int64
	CallOutput int64
}

func (t turnStats) input(totals store.UsageTotals) int64 {
	return max(0, totals.InputTokens-t.BaseInput) + t.CallInput
}

func (t turnStats) output(totals store.UsageTotals) int64 {
	return max(0, totals.OutputTokens-t.BaseOutput) + t.CallOutput
}
