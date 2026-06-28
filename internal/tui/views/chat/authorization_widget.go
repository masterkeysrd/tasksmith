package chat

import (
	"fmt"
	"image/color"
	"os"
	"strings"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type AuthorizationWidgetProps struct {
	Request            permissions.AuthorizationRequest
	SelectedIndex      int
	SelectedScopeIndex int
	OnPreview          func()
	IsActive           bool
	IsFocused          bool
	IsDecided          bool
	Decision           permissions.AuthorizationDecision // valid when IsDecided == true
	IsSubmitting       bool                              // true while the batch API call is in flight
	OnSelectVertical   func(int)
	OnSelectHorizontal func(int)
	OnApprove          func()
	OnDeny             func()
}

type AuthorizationHybridSelectorProps struct {
	Options            []permissions.PermissionOption
	VerticalIndex      int // 0: Once, 1: Session, 2: Workspace, 3: Global
	HorizontalIndex    int // index into Options
	IsActive           bool
	OnSelectVertical   func(int)
	OnSelectHorizontal func(int)
}

var AuthorizationHybridSelector = kitex.FC("AuthorizationHybridSelector", func(props AuthorizationHybridSelectorProps) kitex.Node {
	t := theme.UseTheme()
	if t == nil {
		return nil
	}

	scopesList := []struct {
		Name        string
		Description string
		Scope       permissions.PermissionScope
		Icon        kitex.Node
	}{
		{Name: "Once", Description: "Only this one time", Scope: permissions.ScopeOnce},
		{Name: "Session", Description: "For this session only", Scope: permissions.ScopeSession},
		{Name: "Workspace", Description: "Save to workspace config", Scope: permissions.ScopeWorkspace, Icon: icon.Cog},
		{Name: "Global", Description: "Save to global config", Scope: permissions.ScopeGlobal, Icon: icon.Warning},
	}

	var rows []kitex.Node
	for idx, s := range scopesList {
		rowIdx := idx
		isVSelected := props.VerticalIndex == rowIdx && props.IsActive

		// Risk-aware colors
		var rowFg color.Color = t.Color.Text.Secondary
		var selectedFg color.Color
		var selectedBg color.Color

		switch s.Scope {
		case permissions.ScopeOnce:
			selectedFg = t.Color.Surface.Success
			selectedBg = color.RGBA{R: 0, G: 255, B: 0, A: 30}
		case permissions.ScopeSession:
			selectedFg = t.Color.Surface.Info
			selectedBg = t.Color.Surface.BaseHover
		case permissions.ScopeWorkspace:
			selectedFg = color.RGBA{R: 224, G: 153, B: 36, A: 255} // warningColor
			selectedBg = color.RGBA{R: 224, G: 153, B: 36, A: 40}  // warningFocusColor
		case permissions.ScopeGlobal:
			selectedFg = t.Color.Surface.Error
			selectedBg = color.RGBA{R: 255, G: 0, B: 0, A: 30}
		}

		rowStyle := style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Padding(0, 1)

		lblStyle := style.S()
		if isVSelected {
			lblStyle = lblStyle.Foreground(selectedFg).Bold(true)
			rowStyle = rowStyle.Background(selectedBg)
		} else {
			lblStyle = lblStyle.Foreground(rowFg)
		}

		checkbox := "○"
		if isVSelected {
			checkbox = "●"
		}

		hasHorizontal := rowIdx == 1 || rowIdx == 2 || rowIdx == 3
		var horizNode kitex.Node
		if isVSelected && hasHorizontal && len(props.Options) > 1 {
			var pills []kitex.Node
			pills = append(pills, kitex.Span(kitex.SpanProps{
				Style: style.S().Foreground(t.Color.Text.Tertiary).PaddingRight(1),
			}, kitex.Text("↳ Limit to:")))

			for hIdx, opt := range props.Options {
				pillIdx := hIdx
				isHSelected := props.HorizontalIndex == pillIdx
				label := formatTargetLabel(opt)

				pillStyle := style.S().MarginRight(2)

				if isHSelected {
					pills = append(pills, kitex.Box(kitex.BoxProps{
						Style: pillStyle.Foreground(t.Color.Surface.Success).Bold(true),
						OnClick: func(e event.Event) {
							if props.OnSelectHorizontal != nil {
								props.OnSelectHorizontal(pillIdx)
							}
						},
					}, kitex.Text(fmt.Sprintf("[%s]", label))))
				} else {
					pills = append(pills, kitex.Box(kitex.BoxProps{
						Style: pillStyle.Foreground(t.Color.Text.Secondary),
						OnClick: func(e event.Event) {
							if props.OnSelectHorizontal != nil {
								props.OnSelectHorizontal(pillIdx)
							}
						},
					}, kitex.Text(label)))
				}
			}

			horizNode = kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					PaddingLeft(4),
			}, pills...)
		}

		rows = append(rows, kitex.Box(kitex.BoxProps{
			Style: rowStyle,
			OnClick: func(e event.Event) {
				if props.OnSelectVertical != nil {
					props.OnSelectVertical(rowIdx)
				}
			},
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					Gap(1),
			},
				kitex.Span(kitex.SpanProps{Style: lblStyle}, kitex.Text(fmt.Sprintf("%s %s", checkbox, s.Name))),
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text(s.Description)),
				kitex.If(s.Icon != nil, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(selectedFg)}, s.Icon)
				}),
			),
			kitex.If(horizNode != nil, func() kitex.Node { return horizNode }),
		))
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Gap(0),
	}, rows...)
})

var AuthorizationWidget = kitex.FC("AuthorizationWidget", func(props AuthorizationWidgetProps) kitex.Node {
	t := theme.UseTheme()
	if t == nil {
		return nil
	}

	req := props.Request

	warningColor := color.Color(color.RGBA{R: 224, G: 153, B: 36, A: 255})

	borderColor := t.Color.Border.Primary
	if props.IsDecided {
		if props.Decision.Approved {
			borderColor = t.Color.Surface.Success
		} else {
			borderColor = t.Color.Surface.Error
		}
	} else if props.IsActive {
		if props.IsFocused {
			borderColor = t.Color.Surface.Info
		} else {
			borderColor = t.Color.Border.Primary
		}
	}

	containerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100)).
		Border(true, style.SingleBorder(), borderColor).
		Background(t.Color.Surface.BaseHover)

	titleColor := t.Color.Text.Secondary
	if props.IsActive && props.IsFocused {
		titleColor = warningColor
	}

	// 1. Decided State
	if props.IsDecided {
		statusText := "✔ APPROVED"
		statusCol := t.Color.Surface.Success
		if !props.Decision.Approved {
			statusText = "✖ DENIED"
			statusCol = t.Color.Surface.Error
		}

		return kitex.Box(kitex.BoxProps{Style: containerStyle},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					PaddingTop(0).
					PaddingHorizontal(1).
					PaddingBottom(1).
					Gap(1).
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn),
			},
				// Header row
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).JustifyContent(style.JustifyBetween),
				},
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Authorization request has been recorded.")),
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(statusCol).Bold(true)}, kitex.Text(statusText)),
				),
				// Identity
				renderIdentity(t, req),
				// Decision summary
				kitex.If(props.Decision.Approved, func() kitex.Node {
					summary := string(props.Decision.Scope)
					if props.Decision.SelectedTarget != "" {
						summary += " · " + props.Decision.SelectedTarget
					}
					return kitex.Span(kitex.SpanProps{
						Style: style.S().Foreground(t.Color.Text.Tertiary).Italic(true),
					}, kitex.Text("✔ "+summary))
				}),
				kitex.If(props.IsSubmitting, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{
						Style: style.S().Foreground(t.Color.Text.Tertiary).Italic(true).MarginTop(1),
					}, kitex.Text("Sending..."))
				}),
			),
		)
	}

	// 2. Standard State (Active or Queued)

	// State Badge
	stateText := "○ QUEUED"
	stateCol := t.Color.Text.Tertiary
	if props.IsActive {
		if props.IsFocused {
			stateText = "● ACTIVE"
			stateCol = t.Color.Surface.Info
		} else {
			stateText = "○ UNFOCUSED"
		}
	}

	return kitex.Box(kitex.BoxProps{Style: containerStyle},
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				PaddingTop(0).
				PaddingHorizontal(1).
				PaddingBottom(1).
				Width(style.Percent(100)),
		},
			// Header
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).JustifyContent(style.JustifyBetween).PaddingBottom(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1)},
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(titleColor)}, icon.Alert),
					kitex.Span(kitex.SpanProps{Style: style.S().Bold(true).Foreground(titleColor)}, kitex.Text("Authorization Required")),
				),
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(stateCol).Bold(props.IsFocused)}, kitex.Text(stateText)),
			),

			// Identity details
			kitex.Box(kitex.BoxProps{Style: style.S().MarginTop(0)}, renderIdentity(t, req)),

			// Context (surfacing req.Description - tool hint or default)
			kitex.If(req.Description != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).MarginBottom(1),
				},
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).MinWidth(style.Cells(9))}, kitex.Text("Context")),
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Italic(true)}, kitex.Text(req.Description)),
				)
			}),

			// System Hints (Always visible if they exist)
			kitex.If(len(req.SystemHints) > 0, func() kitex.Node {
				var hintNodes []kitex.Node
				for _, hint := range req.SystemHints {
					hintNodes = append(hintNodes, components.Alert(components.AlertProps{
						Severity: components.AlertWarning,
						ShowIcon: true,
						Style:    style.S(),
					}, kitex.Text(hint)))
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
				},
					hintNodes...,
				)
			}),

			// Interactive area (only if active)
			kitex.If(props.IsActive, func() kitex.Node {
				if !props.IsFocused {
					// Collapsed summary for unfocused state
					summary := "Session" // Fallback
					switch props.SelectedIndex {
					case 0:
						summary = "Once"
					case 1:
						summary = "Session"
					case 2:
						summary = "Workspace"
					case 3:
						summary = "Global"
					}
					if len(req.Options) > props.SelectedScopeIndex && props.SelectedIndex > 0 {
						summary += " · [" + formatTargetLabel(req.Options[props.SelectedScopeIndex]) + "]"
					}
					return kitex.Box(kitex.BoxProps{Style: style.S().MarginTop(1)},
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text("● "+summary)),
						kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).PaddingTop(1)},
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary)}, kitex.Text("Composer focused")),
							kitex.Text("    "),
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary)}, kitex.Text("[Esc]")),
							kitex.Text(" Focus widget"),
						),
					)
				}

				// Full interactive selector
				return kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).MarginTop(1)},
					kitex.Box(kitex.BoxProps{Style: style.S().PaddingBottom(0).Foreground(t.Color.Text.Primary)}, kitex.Text("Grant permission...")),
					AuthorizationHybridSelector(AuthorizationHybridSelectorProps{
						Options:            req.Options,
						VerticalIndex:      props.SelectedIndex,
						HorizontalIndex:    props.SelectedScopeIndex,
						IsActive:           props.IsActive,
						OnSelectVertical:   props.OnSelectVertical,
						OnSelectHorizontal: props.OnSelectHorizontal,
					}),

					// Buttons
					kitex.Box(kitex.BoxProps{
						Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).MarginTop(1),
					},
						components.Button(components.ButtonProps{
							Variant:   components.ButtonText,
							Color:     components.ButtonSuccess,
							StartIcon: icon.Check,
							OnClick:   props.OnApprove,
						}, kitex.Text("Allow (Enter)")),
						components.Button(components.ButtonProps{
							Variant:   components.ButtonText,
							Color:     components.ButtonError,
							StartIcon: icon.Error,
							OnClick:   props.OnDeny,
						}, kitex.Text("Deny (d)")),
						kitex.If(req.Preview != "", func() kitex.Node {
							return components.Button(components.ButtonProps{
								Variant: components.ButtonText,
								Color:   components.ButtonPrimary,
								OnClick: props.OnPreview,
							}, kitex.Text("Preview (p)"))
						}),
					),

					// Hint Bar
					kitex.Box(kitex.BoxProps{Style: style.S().PaddingTop(1)},
						renderHint(t, "j/k", "scope"),
						kitex.If(len(req.Options) > 1 && props.SelectedIndex > 0, func() kitex.Node {
							return kitex.Fragment(kitex.Text(" · "), renderHint(t, "h/l", "target"))
						}),
						kitex.If(req.Preview != "", func() kitex.Node {
							return kitex.Fragment(kitex.Text(" · "), renderHint(t, "p", "preview"))
						}),
						kitex.Text(" · "),
						renderHint(t, "Esc", "deny"),
					),
				)
			}),
		),
	)
})

func renderHint(t *theme.Scheme, keys, action string) kitex.Node {
	return kitex.Fragment(
		kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary)}, kitex.Text(keys)),
		kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text(" "+action)),
	)
}

func renderIdentity(t *theme.Scheme, req permissions.AuthorizationRequest) kitex.Node {
	return kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn)},
		kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).PaddingBottom(0),
		},
			kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).MinWidth(style.Cells(9))}, kitex.Text("Tool")),
			kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Magenta).Bold(true)}, kitex.Text(req.ToolName)),
		),
		kitex.If(len(req.Options) > 0, func() kitex.Node {
			return kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).PaddingBottom(0),
			},
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).MinWidth(style.Cells(9))}, kitex.Text("Action")),
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Purple)}, kitex.Text(formatTargetLabel(req.Options[0]))),
			)
		}),
	)
}

func formatTargetLabel(opt permissions.PermissionOption) string {
	if opt.Target == "*" {
		return "All"
	}
	if strings.HasPrefix(opt.Target, "http://") || strings.HasPrefix(opt.Target, "https://") {
		return opt.Target
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(opt.Target, home) {
		return "~" + strings.TrimPrefix(opt.Target, home)
	}
	return opt.Target
}
