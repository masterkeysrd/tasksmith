package components

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// Notice is a trail functional component rendering a dim informational notice line.
var Notice = trail.FC("Notice", func(props store.NoticeData) trail.Node {
	scheme := theme.UseTheme()
	s := style.S().FlexDirection(style.FlexRow).MarginBottom(1)
	if scheme != nil && scheme.Color.Text.Secondary != nil {
		s = s.Foreground(scheme.Color.Text.Secondary)
	}
	return trail.Box(trail.BoxProps{Style: s},
		trail.Span(trail.SpanProps{}, trail.Text(props.Text)),
	)
})

// ErrorBanner is a trail functional component rendering a styled error notification.
var ErrorBanner = trail.FC("ErrorBanner", func(props store.ErrorData) trail.Node {
	scheme := theme.UseTheme()
	title := props.Title
	if title == "" {
		title = "error"
	}
	badgeStyle := style.S().Bold(true)
	msgStyle := style.S()
	if scheme != nil {
		badgeStyle = badgeStyle.Foreground(scheme.Color.Text.Error)
		msgStyle = msgStyle.Foreground(scheme.Color.Text.Error)
	}
	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow).MarginBottom(1),
	},
		trail.Span(trail.SpanProps{Style: badgeStyle}, trail.Text(title+": ")),
		trail.Span(trail.SpanProps{Style: msgStyle}, trail.Text(props.Message)),
	)
})

// CommandOutput is a trail functional component rendering a slash command execution and output.
var CommandOutput = trail.FC("CommandOutput", func(props store.CommandData) trail.Node {
	scheme := theme.UseTheme()
	promptStyle := style.S().FlexDirection(style.FlexRow)
	outStyle := style.S().FlexDirection(style.FlexRow)
	if scheme != nil {
		promptStyle = promptStyle.Foreground(scheme.Color.Text.Primary)
		outStyle = outStyle.Foreground(scheme.Color.Text.Secondary)
	}
	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn).MarginBottom(1),
	},
		trail.Box(trail.BoxProps{Style: promptStyle},
			trail.Span(trail.SpanProps{Style: style.S().Bold(true)}, trail.Text("> ")),
			trail.Span(trail.SpanProps{}, trail.Text(props.Command)),
		),
		trail.Box(trail.BoxProps{Style: outStyle},
			trail.Span(trail.SpanProps{Style: style.S().WhiteSpace(style.WhiteSpacePre)}, trail.Text("  ⎿  ")),
			trail.Span(trail.SpanProps{}, trail.Text(props.Output)),
		),
	)
})

// BashCommand renders a completed direct user shell command (!) from the timeline.
var BashCommand = trail.FC("BashCommand", func(record api.CommandRecord) trail.Node {
	scheme := theme.UseTheme()

	var magentaColor, primaryColor, secondaryColor, errorColor color.Color
	if scheme != nil {
		magentaColor = scheme.Color.Text.Magenta
		if magentaColor == nil {
			magentaColor = scheme.Color.Text.Primary
		}
		primaryColor = scheme.Color.Text.Primary
		secondaryColor = scheme.Color.Text.Secondary
		errorColor = scheme.Color.Text.Error
	}

	headerChildren := []trail.Node{
		trail.Span(trail.SpanProps{
			Style: style.S().Bold(true).Foreground(magentaColor).WhiteSpace(style.WhiteSpaceNoWrap).Flex(0, 0),
		}, trail.Text("! ")),
		trail.Span(trail.SpanProps{
			Style: style.S().Foreground(primaryColor),
		}, trail.Text(record.Command)),
	}
	if record.ExitCode != 0 {
		headerChildren = append(headerChildren, trail.Span(trail.SpanProps{
			Style: style.S().Foreground(errorColor).WhiteSpace(style.WhiteSpacePre),
		}, trail.Text(fmt.Sprintf(" (exit %d)", record.ExitCode))))
	}

	row1 := trail.Box(trail.BoxProps{
		Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Width(style.Percent(100)),
	}, headerChildren...)

	outText := record.Output
	if strings.TrimSpace(outText) == "" {
		outText = "(no output)"
	}
	row2 := trail.Box(trail.BoxProps{
		Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Width(style.Percent(100)),
	},
		trail.Span(trail.SpanProps{
			Style: style.S().Foreground(secondaryColor).WhiteSpace(style.WhiteSpacePre).Flex(0, 0),
		}, trail.Text("  ⎿  ")),
		trail.Span(trail.SpanProps{
			Style: style.S().Foreground(secondaryColor).WhiteSpace(style.WhiteSpacePreWrap).OverflowWrap(style.OverflowWrapAnywhere).Flex(1, 1).MinWidth(style.Cells(0)),
		}, trail.Text(outText)),
	)

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)).
			MaxWidth(style.Percent(100)).
			MinWidth(style.Cells(0)).
			MarginBottom(1),
	}, row1, row2)
})

// TimelineEvent renders a timeline state change (mode, model, effort) as a notice.
var TimelineEvent = trail.FC("TimelineEvent", func(entry api.TimelineEntry) trail.Node {
	var text string
	switch entry.Kind {
	case api.TimelineKindCommand:
		if entry.Command != nil {
			return BashCommand(*entry.Command)
		}
		return nil
	case api.TimelineKindMode:
		if entry.Mode != nil {
			text = "Mode set to " + string(entry.Mode.To)
		}
	case api.TimelineKindModel:
		if entry.Model != nil {
			text = "Model set to " + entry.Model.To
		}
	case api.TimelineKindEffort:
		if entry.Effort != nil {
			text = "Thinking effort set to " + entry.Effort.To
		}
	case api.TimelineKindAgent:
		if entry.Agent != nil {
			text = "Agent set to " + entry.Agent.To
		}
	case api.TimelineKindPermission:
		return nil
	case api.TimelineKindInterrupted:
		text = "Interrupted by user"
	default:
		text = "Timeline event: " + string(entry.Kind)
	}
	if text == "" {
		return nil
	}
	return Notice(store.NoticeData{Text: text})
})
