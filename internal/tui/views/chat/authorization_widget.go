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
	OnSelectVertical   func(int)
	OnSelectHorizontal func(int)
	OnApprove          func()
	OnDeny             func()
}

type AuthorizationHybridSelectorProps struct {
	Options            []permissions.PermissionOption
	VerticalIndex      int // 0: Once, 1: Session, 2: Workspace, 3: Global, 4: Deny
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
	}{
		{Name: "Once", Description: "Allow this action only once"},
		{Name: "Session", Description: "Allow for the duration of this session"},
		{Name: "Workspace", Description: "Allow for this workspace (local configuration)"},
		{Name: "Global", Description: "Allow globally across all projects"},
		{Name: "Deny", Description: "Deny execution of this tool call"},
	}

	var rows []kitex.Node
	for idx, s := range scopesList {
		// Use a local copy of idx for safe closure capture
		rowIdx := idx
		isVSelected := props.VerticalIndex == rowIdx && props.IsActive

		rowStyle := style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Padding(0, 1).
			MarginVertical(0)

		lblStyle := style.S()
		if isVSelected {
			lblStyle = lblStyle.
				Foreground(t.Color.Surface.Info).
				Bold(true)
			rowStyle = rowStyle.Background(t.Color.Surface.BaseHover)
		} else {
			lblStyle = lblStyle.Foreground(t.Color.Text.Secondary)
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
				Style: style.S().Foreground(t.Color.Text.Secondary).PaddingRight(1),
			}, kitex.Text("Limit to:")))

			for hIdx, opt := range props.Options {
				pillIdx := hIdx
				isHSelected := props.HorizontalIndex == pillIdx
				label := formatTargetLabel(opt)

				pillStyle := style.S().
					MarginRight(1)

				var text string
				if isHSelected {
					pillStyle = pillStyle.
						Foreground(t.Color.Surface.Success).
						Bold(true)
					text = fmt.Sprintf("[%s]", label)
				} else {
					pillStyle = pillStyle.
						Foreground(t.Color.Text.Secondary)
					text = fmt.Sprintf(" %s ", label)
				}

				pills = append(pills, kitex.Box(kitex.BoxProps{
					Style: pillStyle,
					OnClick: func(e event.Event) {
						if props.OnSelectHorizontal != nil {
							props.OnSelectHorizontal(pillIdx)
						}
					},
				}, kitex.Text(text)))
			}

			horizNode = kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					PaddingLeft(5).
					PaddingTop(0).
					PaddingBottom(0),
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
					Gap(1).
					PaddingVertical(0),
			},
				kitex.Span(kitex.SpanProps{Style: lblStyle}, kitex.Text(fmt.Sprintf("%s [%s]", checkbox, s.Name))),
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(s.Description)),
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
	warningFocusColor := color.Color(color.RGBA{R: 224, G: 153, B: 36, A: 40})

	borderColor := t.Color.Border.Primary
	if props.IsActive {
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
		MaxWidth(style.Percent(100)).
		Overflow(style.OverflowHidden).
		Border(true, style.SingleBorder(), borderColor).
		Background(t.Color.Surface.BaseHover)

	headerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Gap(1).
		PaddingBottom(1)

	titleColor := t.Color.Text.Secondary
	if props.IsActive {
		if props.IsFocused {
			titleColor = warningColor
		} else {
			titleColor = t.Color.Text.Secondary
		}
	}

	titleStyle := style.S().
		Bold(true).
		Foreground(titleColor)

	// Render hints (only if active)
	var hintNodes []kitex.Node
	if props.IsActive {
		for _, hint := range req.SystemHints {
			hintNodes = append(hintNodes, kitex.Box(kitex.BoxProps{
				Style: style.S().
					Background(warningFocusColor).
					Foreground(warningColor).
					Padding(0, 1).
					MarginBottom(1),
			}, kitex.Text(hint)))
		}
	}

	return kitex.Box(kitex.BoxProps{Style: containerStyle},
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Padding(1, 1, 0, 1).
				Width(style.Percent(100)).
				MaxWidth(style.Percent(100)).
				Overflow(style.OverflowHidden),
		},
			// Header
			kitex.Box(kitex.BoxProps{Style: headerStyle},
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(titleColor)}, icon.Alert),
				kitex.Span(kitex.SpanProps{Style: titleStyle}, kitex.Text("AUTHORIZATION REQUIRED")),
				kitex.If(!props.IsActive, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Italic(true)}, kitex.Text(" (Queued)"))
				}),
				kitex.If(props.IsActive && !props.IsFocused, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Italic(true)}, kitex.Text(" (Unfocused)"))
				}),
			),

			// Hints
			kitex.If(len(hintNodes) > 0, func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn),
				}, hintNodes...)
			}),

			// Tool details
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					Gap(1).
					PaddingBottom(1),
			},
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text("Tool:")),
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Magenta).Bold(true)}, kitex.Text(req.ToolName)),
			),
			kitex.If(len(req.Options) > 0, func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						Gap(1).
						PaddingBottom(1),
				},
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text("Target:")),
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Purple).Bold(true)}, kitex.Text(req.Options[0].Target)),
				)
			}),

			// Hybrid Scope & Target Selector
			kitex.Box(kitex.BoxProps{
				Style: style.S().PaddingBottom(0),
			},
				AuthorizationHybridSelector(AuthorizationHybridSelectorProps{
					Options:            req.Options,
					VerticalIndex:      props.SelectedIndex,
					HorizontalIndex:    props.SelectedScopeIndex,
					IsActive:           props.IsActive,
					OnSelectVertical:   props.OnSelectVertical,
					OnSelectHorizontal: props.OnSelectHorizontal,
				}),
			),

			// Action Buttons (only if active)
			kitex.If(props.IsActive, func() kitex.Node {
				var btnNodes []kitex.Node
				btnNodes = append(btnNodes, components.Button(components.ButtonProps{
					Variant: components.ButtonText,
					Color:   components.ButtonSuccess,
					Style:   style.S().MarginRight(1),
					OnClick: func() {
						if props.OnApprove != nil {
							props.OnApprove()
						}
					},
				}, kitex.Text("Approve [Enter]")))

				btnNodes = append(btnNodes, components.Button(components.ButtonProps{
					Variant: components.ButtonText,
					Color:   components.ButtonError,
					Style:   style.S().MarginRight(1),
					OnClick: func() {
						if props.OnDeny != nil {
							props.OnDeny()
						}
					},
				}, kitex.Text("Deny [d]")))

				if req.Preview != "" {
					btnNodes = append(btnNodes, components.Button(components.ButtonProps{
						Variant: components.ButtonText,
						Color:   components.ButtonPrimary,
						Style:   style.S().MarginRight(1),
						OnClick: func() {
							if props.OnPreview != nil {
								props.OnPreview()
							}
						},
					}, kitex.Text("Preview [p]")))
				}

				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						MarginTop(1).
						MarginBottom(0),
				}, btnNodes...)
			}),

			// Instructions (only if active)
			kitex.If(props.IsActive, func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Border(style.SingleBorder().Color(t.Color.Border.Primary)).
						Padding(0, 1).
						MarginTop(1).
						Foreground(t.Color.Text.Secondary).
						Width(style.Percent(100)),
				},
					func() kitex.Node {
						if props.IsFocused {
							text := "[j/k] Navigate Scope"
							if len(req.Options) > 1 && (props.SelectedIndex == 1 || props.SelectedIndex == 2 || props.SelectedIndex == 3) {
								text += "    [h/l] Limit Target"
							}
							text += "    [Enter] Approve    [d / Esc] Deny"
							if req.Preview != "" {
								text += "    [p] Preview"
							}
							return kitex.Text(text)
						} else {
							return kitex.Text("Composer focused    [Esc] Focus widget")
						}
					}(),
				)
			}),
		),
	)
})

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
