package modelpicker

import (
	"context"
	"fmt"
	"strconv"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
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

		return kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Provider:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Primary).Bold(true)}, kitex.Text(targetProvider.DisplayName)),
			),
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Model ID:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(targetModel.ID)),
			),
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Context:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Surface.Success)}, kitex.Text(contextStr+" tokens")),
			),
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Max Output:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(maxOutputStr+" tokens")),
			),
		)
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
			// Handle error silently or log in debug mode
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
		PreviewWidth:  28,
		RenderPreview: renderPreview,
		OnSelect:      onSelect,
		OnClose:       onClose,
	})
})
