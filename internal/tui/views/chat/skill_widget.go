package chat

import (
	"fmt"
	"image/color"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/core/preview"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// ActivateSkillToolWidget renders the result of an activate_skill tool call inline, opening a modal on click.
var ActivateSkillToolWidget = kitex.FC("ActivateSkillToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

	tc := props.ToolCall
	tm := props.ToolMessage

	var skillName string
	if tc != nil && tc.Args != nil {
		skillName, _ = tc.Args["skill"].(string)
	}

	var statusLabel string
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color
	var instructions string
	var hasInstructions bool
	var details string

	if tm != nil {
		out, ok := parseStructuredOutput[tools.ActivateSkillOutput](tm.StructuredContent)
		if ok && out.Success {
			instructions = out.Instructions
			hasInstructions = instructions != ""
		}
	}

	if t != nil {
		var actionText string
		if tm == nil {
			actionText = "Activating Skill "
			statusLabel = fmt.Sprintf("Activating Skill [%s]", skillName)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			actionText = "Error Activating Skill "
			statusLabel = fmt.Sprintf("Error Activating Skill [%s]", skillName)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
			details = getToolOutput(tm.Content)
		} else {
			out, ok := parseStructuredOutput[tools.ActivateSkillOutput](tm.StructuredContent)
			if ok && out.Success {
				actionText = "Activated Skill "
				statusLabel = fmt.Sprintf("Activated Skill [%s]", skillName)
				iconNode = nil // remove checkmark completely on success
				themeColor = t.Color.Surface.Success
			} else {
				actionText = "Failed to Activate Skill "
				statusLabel = fmt.Sprintf("Failed to Activate Skill [%s]", skillName)
				iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
				themeColor = t.Color.Text.Error
			}
		}

		baseFocusBg := t.Color.Surface.BaseFocus
		pencilIconColor := t.Color.Surface.Info

		labelNode = kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter),
		},
			kitex.Span(kitex.SpanProps{
				Style: style.S().
					Bold(true).
					Foreground(color.RGBA{255, 255, 255, 255}),
			}, kitex.Text(actionText)),
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Background(baseFocusBg).
					PaddingHorizontal(1).
					Gap(1),
			},
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(pencilIconColor)}, icon.Pencil),
				kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(color.RGBA{255, 255, 255, 255}).
						Bold(true),
				}, kitex.Text(skillName)),
			),
		)
	}

	var onClick func()
	if tm != nil && props.OnViewPreview != nil {
		onClick = func() {
			if tm.IsError {
				props.OnViewPreview(
					fmt.Sprintf("Error Activating Skill %s", skillName),
					preview.DefaultTextPreview{Text: details},
				)
			} else if hasInstructions {
				props.OnViewPreview(
					fmt.Sprintf("Instructions for %s Skill", skillName),
					preview.MarkdownPreview{Markdown: instructions},
				)
			}
		}
	}

	return components.ToolBadge(components.ToolBadgeProps{
		Icon:      iconNode,
		Label:     statusLabel,
		LabelNode: labelNode,
		Color:     themeColor,
		OnClick:   onClick,
	})
})
