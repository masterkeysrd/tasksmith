package modelpicker

import (
	"context"
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/toast"
)

type ViewProps struct{}

var View = kitex.FC("ModelPickerView", func(props ViewProps) kitex.Node {
	isOpen := active.UseModal() == "modelpicker"
	if !isOpen {
		return nil
	}

	activeSessionID := active.UseSessionID()
	t := theme.UseTheme()
	client := tuiapi.UseClient()
	windClient := wind.UseClient()
	providersQuery := queries.UseListProviders()

	if t == nil {
		return nil
	}

	var groups []components.PickerGroup
	if providersQuery.Data != nil {
		for _, p := range providersQuery.Data.Providers {
			var items []components.PickerItem
			for _, m := range p.Models {
				label := m.Label
				if label == "" {
					label = m.Name
				}
				if label == "" {
					label = m.ID
				}

				val := map[string]string{
					"provider": p.Name,
					"model":    m.ID,
				}

				items = append(items, components.PickerItem{
					ID:       p.Name + "/" + m.ID,
					Label:    label,
					Sublabel: m.ID,
					Value:    val,
				})
			}
			if len(items) > 0 {
				groups = append(groups, components.PickerGroup{
					Name:  p.DisplayName,
					Items: items,
				})
			}
		}
	}

	renderPreview := func(item components.PickerItem) kitex.Node {
		val, ok := item.Value.(map[string]string)
		if !ok {
			return nil
		}

		providerName := val["provider"]
		modelID := val["model"]

		var targetModel *api.Model
		var targetProvider *api.Provider

		if providersQuery.Data != nil {
			for _, p := range providersQuery.Data.Providers {
				if p.Name == providerName {
					targetProvider = &p
					for _, m := range p.Models {
						if m.ID == modelID {
							targetModel = &m
							break
						}
					}
					break
				}
			}
		}

		if targetModel == nil || targetProvider == nil {
			return nil
		}

		modelLabel := item.Label
		if modelLabel == "" {
			modelLabel = targetModel.ID
		}
		if targetModel.IsDefault {
			modelLabel += " (default)"
		}

		contextStr := strconv.Itoa(targetModel.ContextWindow)
		if targetModel.ContextWindow >= 1000000 {
			contextStr = fmt.Sprintf("%.1fM", float64(targetModel.ContextWindow)/1000000.0)
		} else if targetModel.ContextWindow >= 1000 {
			contextStr = fmt.Sprintf("%dk", targetModel.ContextWindow/1000)
		}

		maxOutputStr := strconv.Itoa(targetModel.MaxOutputTokens)
		if targetModel.MaxOutputTokens >= 1000 {
			maxOutputStr = fmt.Sprintf("%dk", targetModel.MaxOutputTokens/1000)
		}

		// Statusline-like colors
		modelColor := t.Color.Surface.Tertiary
		if val, ok := t.Palette["orange"]; ok {
			modelColor = val
		}
		providerColor := t.Color.Text.Magenta
		actionColor := t.Color.Surface.Success
		if val, ok := t.Palette["green"]; ok {
			actionColor = val
		}

		var familyNode kitex.Node
		if targetModel.Family != "" {
			familyNode = kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Family:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(targetModel.Family)),
			)
		}

		var badgeNodes []kitex.Node
		tagStyle := style.S().Background(t.Color.Surface.InfoFocus).Foreground(t.Color.Surface.Info).Padding(0, 1).Bold(true)

		if targetModel.Capabilities.ToolCall {
			badgeNodes = append(badgeNodes, kitex.Box(kitex.BoxProps{
				Style: tagStyle,
			}, kitex.Text("TOOL CALL")))
		}
		if targetModel.Capabilities.Reasoning {
			badgeNodes = append(badgeNodes, kitex.Box(kitex.BoxProps{
				Style: tagStyle,
			}, kitex.Text("REASONING")))
		}

		if targetModel.OpenWeights {
			badgeNodes = append(badgeNodes, kitex.Box(kitex.BoxProps{
				Style: tagStyle,
			}, kitex.Text("OPEN WEIGHTS")))
		}

		var capabilitiesNode kitex.Node
		if len(badgeNodes) > 0 {
			capabilitiesNode = kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("CAPABILITIES")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).FlexWrap(style.FlexWrapOn).Gap(1),
				}, badgeNodes...),
			)
		}

		formatPrice := func(val float64) string {
			if val <= 0 {
				return "$0.00"
			}
			if val < 0.01 {
				return fmt.Sprintf("$%.4f", val)
			}
			return fmt.Sprintf("$%.2f", val)
		}

		children := []kitex.Node{
			// Header: CPU icon + model label
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(modelColor)}, icon.CPU),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(modelColor).Bold(true)}, kitex.Text(modelLabel)),
			),
			// Provider line
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Provider:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(providerColor)}, kitex.Text(targetProvider.DisplayName)),
			),
		}

		if familyNode != nil {
			children = append(children, familyNode)
		}

		yellowColor := t.Color.Text.Primary
		if val, ok := t.Palette["yellow"]; ok {
			yellowColor = val
		}
		cyanColor := t.Color.Text.Primary
		if val, ok := t.Palette["cyan"]; ok {
			cyanColor = val
		}

		// Specification Table Card Layout
		columnWidth := style.Percent(50)

		row1 := kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).MarginBottom(1),
		},
			// Context Window
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Width(columnWidth).Gap(0),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("CONTEXT WINDOW")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Purple).Bold(true)}, kitex.Text(contextStr)),
			),
			// Max Output Limit
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Width(columnWidth).Gap(0),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("MAX OUTPUT LIMIT")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Purple).Bold(true)}, kitex.Text(maxOutputStr)),
			),
		)

		// Build input cost and output cost strings, including tiers if present
		inputCostStr := formatPrice(targetModel.Pricing.Input)
		outputCostStr := formatPrice(targetModel.Pricing.Output)

		for _, tier := range targetModel.Pricing.TieredLimits {
			limitStr := fmt.Sprintf("%dk", tier.TierLimit/1000)
			if tier.TierLimit >= 1000000 {
				limitStr = fmt.Sprintf("%dM", tier.TierLimit/1000000)
			}
			inputCostStr += fmt.Sprintf("\n%s (>%s)", formatPrice(tier.Input), limitStr)
			outputCostStr += fmt.Sprintf("\n%s (>%s)", formatPrice(tier.Output), limitStr)
		}

		row2 := kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).MarginBottom(1),
		},
			// Input Cost
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Width(columnWidth).Gap(0),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("INPUT COST / 1M")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(yellowColor).Bold(true)}, kitex.Text(inputCostStr)),
			),
			// Output Cost
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Width(columnWidth).Gap(0),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("OUTPUT COST / 1M")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(yellowColor).Bold(true)}, kitex.Text(outputCostStr)),
			),
		)

		row3 := kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow),
		},
			// Knowledge Cutoff
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Width(columnWidth).Gap(0),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("KNOWLEDGE CUTOFF")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(cyanColor).Bold(true)}, kitex.Text(targetModel.KnowledgeCutoff)),
			),
			// Last Updated
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Width(columnWidth).Gap(0),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("LAST UPDATED")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary).Bold(true)}, kitex.Text(targetModel.LastUpdated)),
			),
		)

		cardBg := color.RGBA{R: 0x16, G: 0x16, B: 0x1e, A: 0xff}

		specCard := kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Gap(0).
				Padding(1).
				Background(cardBg),
		},
			row1,
			row2,
			row3,
		)

		// Input Modalities Tags
		var inputModsNode kitex.Node
		if len(targetModel.Modalities.Inputs) > 0 {
			var inputBadgeNodes []kitex.Node
			for _, in := range targetModel.Modalities.Inputs {
				inputBadgeNodes = append(inputBadgeNodes, kitex.Box(kitex.BoxProps{
					Style: tagStyle,
				}, kitex.Text(strings.ToUpper(in))))
			}
			inputModsNode = kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("INPUTS")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).FlexWrap(style.FlexWrapOn).Gap(1),
				}, inputBadgeNodes...),
			)
		}

		// Output Modalities Tags
		var outputModsNode kitex.Node
		if len(targetModel.Modalities.Outputs) > 0 {
			var outputBadgeNodes []kitex.Node
			for _, out := range targetModel.Modalities.Outputs {
				outputBadgeNodes = append(outputBadgeNodes, kitex.Box(kitex.BoxProps{
					Style: tagStyle,
				}, kitex.Text(strings.ToUpper(out))))
			}
			outputModsNode = kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("OUTPUTS")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).FlexWrap(style.FlexWrapOn).Gap(1),
				}, outputBadgeNodes...),
			)
		}

		// Thinking/Reasoning Configs Tags
		var thinkingNode kitex.Node
		if targetModel.Capabilities.Reasoning {
			var thinkingBadgeNodes []kitex.Node
			for _, opt := range targetModel.Capabilities.ReasoningOptions {
				switch opt.Type {
				case "toggle":
					thinkingBadgeNodes = append(thinkingBadgeNodes, kitex.Box(kitex.BoxProps{
						Style: tagStyle,
					}, kitex.Text("TOGGLE")))
				case "budget_tokens":
					thinkingBadgeNodes = append(thinkingBadgeNodes, kitex.Box(kitex.BoxProps{
						Style: tagStyle,
					}, kitex.Text("BUDGET")))
				case "effort":
					if len(opt.Values) > 0 {
						for _, val := range opt.Values {
							thinkingBadgeNodes = append(thinkingBadgeNodes, kitex.Box(kitex.BoxProps{
								Style: tagStyle,
							}, kitex.Text("EFFORT: "+strings.ToUpper(val))))
						}
					} else {
						thinkingBadgeNodes = append(thinkingBadgeNodes, kitex.Box(kitex.BoxProps{
							Style: tagStyle,
						}, kitex.Text("EFFORT")))
					}
				case "adaptive":
					thinkingBadgeNodes = append(thinkingBadgeNodes, kitex.Box(kitex.BoxProps{
						Style: tagStyle,
					}, kitex.Text("ADAPTIVE")))
				default:
					thinkingBadgeNodes = append(thinkingBadgeNodes, kitex.Box(kitex.BoxProps{
						Style: tagStyle,
					}, kitex.Text(strings.ToUpper(opt.Type))))
				}
			}

			if len(thinkingBadgeNodes) == 0 {
				thinkingBadgeNodes = append(thinkingBadgeNodes, kitex.Box(kitex.BoxProps{
					Style: tagStyle,
				}, kitex.Text("ENABLED")))
			}

			thinkingNode = kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("THINKING CONFIGS")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).FlexWrap(style.FlexWrapOn).Gap(1),
				}, thinkingBadgeNodes...),
			)
		}

		children = append(children, specCard)

		if capabilitiesNode != nil {
			children = append(children, capabilitiesNode)
		}
		if thinkingNode != nil {
			children = append(children, thinkingNode)
		}
		if inputModsNode != nil {
			children = append(children, inputModsNode)
		}
		if outputModsNode != nil {
			children = append(children, outputModsNode)
		}

		// Footer: ID + action hint separated by space
		children = append(children, kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).JustifyContent(style.JustifyBetween).Foreground(t.Color.Text.Tertiary).PaddingTop(1),
		},
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("ID: "+modelID)),
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(actionColor).Bold(true)}, kitex.Text("PRESS ENTER TO SELECT MODEL")),
		))

		return kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1).Padding(1),
		}, children...)
	}

	onSelect := func(item components.PickerItem) {
		val, ok := item.Value.(map[string]string)
		if !ok {
			return
		}
		providerName := val["provider"]
		modelID := val["model"]

		promise.New(func(ctx context.Context) (any, error) {
			_, err := client.ConfigureSession(ctx, api.ConfigureSessionRequest{
				SessionID:    activeSessionID,
				ProviderName: providerName,
				ModelName:    modelID,
			})
			return nil, err
		}).Then(func(any) {
			windClient.InvalidateQueries(api.ListSessionsRequest{})
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: activeSessionID})
			active.SetModal("")
		}, func(err error) {
			toast.AddErrorMessage("Model Switch Failed", err.Error())
		})
	}

	onClose := func() {
		active.SetModal("")
	}

	return components.Picker(components.PickerProps{
		IsOpen:        true,
		Title:         "SWITCH MODEL",
		Placeholder:   "Search model ID, label or provider...",
		Groups:        groups,
		PreviewWidth:  36,
		RenderPreview: renderPreview,
		OnSelect:      onSelect,
		OnClose:       onClose,
		Attributes:    map[string]string{"data-context": "modal:modelpicker"},
	})
})
