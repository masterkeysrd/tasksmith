package sessionpicker

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/format"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type ViewProps struct{}

var View = kitex.FC("SessionPickerView", func(props ViewProps) kitex.Node {
	isOpen := active.UseModal() == "sessionpicker"
	if !isOpen {
		return nil
	}

	t := theme.UseTheme()
	sessionsQuery := queries.UseListSessions(api.ListSessionsRequest{Limit: 100})

	if t == nil {
		return nil
	}

	var items []components.PickerItem
	if sessionsQuery.Data != nil {
		for _, s := range sessionsQuery.Data.Sessions {
			title := s.Title
			if title == "" {
				title = "Untitled Session"
			}
			sublabel := fmt.Sprintf("Agent: %s | Model: %s", s.Settings.AgentName, s.Settings.ModelName)
			items = append(items, components.PickerItem{
				ID:       s.ID,
				Label:    title,
				Sublabel: sublabel,
				Value:    s,
			})
		}
	}

	groups := []components.PickerGroup{
		{
			Name:  "Available Sessions",
			Items: items,
		},
	}

	renderPreview := func(item components.PickerItem) kitex.Node {
		s, ok := item.Value.(api.Session)
		if !ok {
			return nil
		}

		sessionColor := t.Color.Text.Magenta
		if val, ok := t.Palette["cyan"]; ok {
			sessionColor = val
		}
		actionColor := t.Color.Surface.Success
		if val, ok := t.Palette["green"]; ok {
			actionColor = val
		}

		cardBg := color.RGBA{R: 0x16, G: 0x16, B: 0x1e, A: 0xff}

		var specRows []kitex.Node

		// Agent
		if s.Settings.AgentName != "" {
			specRows = append(specRows, kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("Agent:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(s.Settings.AgentName)),
			))
		}

		// Provider
		if s.Settings.ProviderName != "" {
			specRows = append(specRows, kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("Provider:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(s.Settings.ProviderName)),
			))
		}

		// Model
		if s.Settings.ModelName != "" {
			specRows = append(specRows, kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("Model:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(s.Settings.ModelName)),
			))
		}

		// Temperature
		if s.Settings.Temperature != nil {
			specRows = append(specRows, kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("Temperature:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(fmt.Sprintf("%.1f", *s.Settings.Temperature))),
			))
		}

		var metricsRows []kitex.Node
		if s.LastTurnMetrics != nil {
			m := s.LastTurnMetrics
			// Last Turn Cost
			metricsRows = append(metricsRows, kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("Last Turn Cost:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(fmt.Sprintf("$%.4f", m.EstimatedCostUSD))),
			))

			// Last Turn Tokens
			metricsRows = append(metricsRows, kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("Last Turn Tokens:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(fmt.Sprintf("%d total (%d prompt / %d completion)", m.TotalTokens, m.PromptTokens, m.CompletionTokens))),
			))

			// Cumulative Cost
			metricsRows = append(metricsRows, kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("Cumulative Cost:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(fmt.Sprintf("$%.4f", m.CumulativeCostUSD))),
			))

			// Cumulative Tokens
			metricsRows = append(metricsRows, kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("Cumulative Tokens:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(fmt.Sprintf("%d total (%d prompt / %d completion)", m.CumulativeTotalTokens, m.CumulativePromptTokens, m.CumulativeCompletionTokens))),
			))
		}

		var infoRows []kitex.Node
		// Updated
		if s.UpdatedAt != "" {
			infoRows = append(infoRows, kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("Last Active:")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(format.CompactRelativeTime(s.UpdatedAt)+" ago")),
			))
		}

		children := []kitex.Node{
			// Header
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).Gap(1).MarginBottom(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(sessionColor)}, icon.Calendar),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(sessionColor).Bold(true)}, kitex.Text(strings.ToUpper(item.Label))),
			),
			// Subtitle / ID
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Foreground(t.Color.Text.Tertiary).
					Italic(true).
					MarginBottom(1).
					WhiteSpace(style.WhiteSpacePreWrap).
					OverflowWrap(style.OverflowWrapBreakWord),
			}, kitex.Text("ID: "+s.ID)),
		}

		// Settings Box
		if len(specRows) > 0 {
			children = append(children,
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true).MarginTop(1)}, kitex.Text("SETTINGS:")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Gap(1).
						Padding(1).
						Background(cardBg),
				}, specRows...),
			)
		}

		// Metrics Box
		if len(metricsRows) > 0 {
			children = append(children,
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true).MarginTop(1)}, kitex.Text("METRICS:")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Gap(1).
						Padding(1).
						Background(cardBg),
				}, metricsRows...),
			)
		}

		// Info Box
		if len(infoRows) > 0 {
			children = append(children,
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true).MarginTop(1)}, kitex.Text("TIMESTAMPS:")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Gap(1).
						Padding(1).
						Background(cardBg),
				}, infoRows...),
			)
		}

		// Footer: action hint
		children = append(children, kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).JustifyContent(style.JustifyEnd).Foreground(t.Color.Text.Tertiary).PaddingTop(1).MarginTop(1),
		},
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(actionColor).Bold(true)}, kitex.Text("PRESS ENTER TO SWITCH SESSION")),
		))

		return kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Gap(1).
				Padding(1).
				Overflow(style.OverflowAuto),
		}, children...)
	}

	onSelect := func(item components.PickerItem) {
		sessionID := item.ID

		active.SetSessionID(sessionID)
		if active.InvalidateSessionState != nil {
			active.InvalidateSessionState(sessionID)
		}
		if active.InvalidateSessionMessages != nil {
			active.InvalidateSessionMessages(sessionID)
		}
		if active.InvalidateFileChanges != nil {
			active.InvalidateFileChanges(sessionID)
		}
		active.SetModal("")
	}

	onClose := func() {
		active.SetModal("")
	}

	return components.Picker(components.PickerProps{
		IsOpen:        true,
		Title:         "SWITCH SESSION",
		Placeholder:   "Search session title...",
		Groups:        groups,
		PreviewWidth:  42,
		RenderPreview: renderPreview,
		OnSelect:      onSelect,
		OnClose:       onClose,
		Attributes:    map[string]string{"data-context": "modal:sessionpicker"},
	})
})
