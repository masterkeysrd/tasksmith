package chat

import (
	"fmt"
	"image/color"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// ActivateSkillToolWidget renders the result of an activate_skill tool call inline, opening a modal on click.
var ActivateSkillToolWidget = kitex.FC("ActivateSkillToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

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
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
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
	if tm != nil && (tm.IsError || hasInstructions) {
		onClick = func() { setShowModal(true) }
	}

	badgeNode := components.ToolBadge(components.ToolBadgeProps{
		Icon:      iconNode,
		Label:     statusLabel,
		LabelNode: labelNode,
		Color:     themeColor,
		OnClick:   onClick,
	})

	return kitex.Fragment(
		badgeNode,
		components.Modal(components.ModalProps{
			IsOpen: showModal(),
			Title: kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Gap(1),
			},
				kitex.If(t != nil && tm != nil && tm.IsError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
				}),
				kitex.If(tm != nil && tm.IsError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Error Activating Skill %s", skillName)))
				}),
				kitex.If(tm != nil && !tm.IsError, func() kitex.Node {
					return kitex.Fragment(
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Pencil),
						kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Instructions for %s Skill", skillName))),
					)
				}),
			),
			OnClose: func() { setShowModal(false) },
		},
			kitex.If(showModal() && tm != nil && tm.IsError && details != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Padding(1).
						Width(style.Percent(100)).
						MinWidth(style.Percent(0)).
						Foreground(t.Color.Text.Secondary).
						WhiteSpace(style.WhiteSpacePreWrap),
				}, kitex.Text(details))
			}),
			kitex.If(showModal() && tm != nil && !tm.IsError && hasInstructions, func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Padding(1).
						Width(style.Percent(100)).
						MinWidth(style.Percent(0)),
				},
					components.Markdown(components.MarkdownProps{
						Source: instructions,
					}),
				)
			}),
		),
	)
})
