package components

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// Notice is a trail functional component rendering a dim informational notice line.
var Notice = trail.FC("Notice", func(props store.NoticeData) trail.Node {
	scheme := theme.UseTheme()
	s := style.S().FlexDirection(style.FlexRow).MarginBottom(1).Foreground(scheme.Color.Text.Secondary)
	if props.Severity == store.NoticeSeverityWarning {
		s = s.PaddingLeft(1).BorderLeft(true, style.SingleBorder(), scheme.Color.Border.Warning).Foreground(scheme.Color.Text.Warning)
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
	badgeStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Error)
	msgStyle := style.S().Foreground(scheme.Color.Text.Error)
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
	promptStyle := style.S().FlexDirection(style.FlexRow).Foreground(scheme.Color.Text.Primary)
	outStyle := style.S().FlexDirection(style.FlexRow).Foreground(scheme.Color.Text.Secondary)
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

	bashColor := scheme.Color.Mode.Bash
	primaryColor := scheme.Color.Text.Primary
	secondaryColor := scheme.Color.Text.Secondary
	errorColor := scheme.Color.Text.Error

	headerChildren := []trail.Node{
		trail.Span(trail.SpanProps{
			Style: style.S().Bold(true).Foreground(bashColor).WhiteSpace(style.WhiteSpaceNoWrap).Flex(0, 0),
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
	case api.TimelineKindThinking:
		if entry.Thinking != nil {
			value := entry.Thinking.To
			if value == nil {
				text = "Thinking reset to default"
			} else {
				switch value.Type {
				case "effort":
					text = "Thinking effort set to " + value.Effort
				case "budget", "budget_tokens":
					text = fmt.Sprintf("Thinking budget set to %d tokens", value.Budget)
				case "toggle":
					if value.Enabled {
						text = "Thinking enabled"
					} else {
						text = "Thinking disabled"
					}
				}
			}
		}
	case api.TimelineKindAgent:
		if entry.Agent != nil {
			text = "Agent set to " + entry.Agent.To
		}
	case api.TimelineKindPermission:
		return nil
	case api.TimelineKindInterrupted:
		text = "Interrupted by user"
	case api.TimelineKindCompacted:
		if entry.Compacted != nil && entry.Compacted.BeforeTokens > 0 {
			text = fmt.Sprintf("Context compacted (%s → %s)", format.FormatTokens(entry.Compacted.BeforeTokens), format.FormatTokens(entry.Compacted.AfterTokens))
		} else {
			text = "Context compacted"
		}
		return Notice(store.NoticeData{Text: text})
	case api.TimelineKindCacheBroken:
		if entry.CacheBroken != nil {
			text = fmt.Sprintf("Prompt cache miss (%s → %s cached tokens)", format.FormatTokens(entry.CacheBroken.PrevCached), format.FormatTokens(entry.CacheBroken.CurrCached))
		} else {
			text = "Prompt cache miss"
		}
		return Notice(store.NoticeData{Text: text})
	default:
		text = "Timeline event: " + string(entry.Kind)
	}
	if text == "" {
		return nil
	}
	return Notice(store.NoticeData{Text: text})
})
