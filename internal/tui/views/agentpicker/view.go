package agentpicker

import (
	"context"
	"fmt"
	"image/color"
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

var View = kitex.FC("AgentPickerView", func(props ViewProps) kitex.Node {
	isOpen := active.UseModal() == "agentpicker"
	if !isOpen {
		return nil
	}

	activeSessionID := active.UseSessionID()
	t := theme.UseTheme()
	client := tuiapi.UseClient()
	windClient := wind.UseClient()
	agentsQuery := queries.UseListAgents()

	if t == nil {
		return nil
	}

	var items []components.PickerItem
	if agentsQuery.Data != nil {
		for _, a := range agentsQuery.Data.Agents {
			items = append(items, components.PickerItem{
				ID:       a.Name,
				Label:    a.Name,
				Sublabel: a.Description,
				Value:    a.Name,
			})
		}
	}

	groups := []components.PickerGroup{
		{
			Name:  "Available Agents",
			Items: items,
		},
	}

	renderPreview := func(item components.PickerItem) kitex.Node {
		agentName, ok := item.Value.(string)
		if !ok {
			return nil
		}

		var targetAgent *api.Agent
		if agentsQuery.Data != nil {
			for _, a := range agentsQuery.Data.Agents {
				if a.Name == agentName {
					targetAgent = &a
					break
				}
			}
		}

		if targetAgent == nil {
			return nil
		}

		agentColor := t.Color.Text.Magenta
		actionColor := t.Color.Surface.Success
		if val, ok := t.Palette["green"]; ok {
			actionColor = val
		}

		desc := strings.TrimSpace(targetAgent.Description)
		if desc == "" {
			desc = "No description provided."
		}

		var specRows []kitex.Node

		// Triggers
		if len(targetAgent.Triggers) > 0 {
			specRows = append(specRows, kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("Triggers:")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Foreground(t.Color.Text.Secondary).
						WhiteSpace(style.WhiteSpacePreWrap).
						OverflowWrap(style.OverflowWrapBreakWord),
				}, kitex.Text(strings.Join(targetAgent.Triggers, ", "))),
			))
		}

		// Models
		if len(targetAgent.Models) > 0 {
			specRows = append(specRows, kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("Models:")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Foreground(t.Color.Text.Secondary).
						WhiteSpace(style.WhiteSpacePreWrap).
						OverflowWrap(style.OverflowWrapBreakWord),
				}, kitex.Text(strings.Join(targetAgent.Models, ", "))),
			))
		}

		// Temperature
		specRows = append(specRows, kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
		},
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("Temperature:")),
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(fmt.Sprintf("%.1f", targetAgent.Temperature))),
		))

		// Tools
		if len(targetAgent.Tools) > 0 {
			specRows = append(specRows, kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("Authorized Tools:")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Foreground(t.Color.Text.Secondary).
						PaddingLeft(1).
						WhiteSpace(style.WhiteSpacePreWrap).
						OverflowWrap(style.OverflowWrapBreakWord),
				}, kitex.Text(strings.Join(targetAgent.Tools, ", "))),
			))
		}

		// Skills
		if len(targetAgent.Skills) > 0 {
			specRows = append(specRows, kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("Skills:")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Foreground(t.Color.Text.Secondary).
						PaddingLeft(1).
						WhiteSpace(style.WhiteSpacePreWrap).
						OverflowWrap(style.OverflowWrapBreakWord),
				}, kitex.Text(strings.Join(targetAgent.Skills, ", "))),
			))
		}

		// Subagents
		if len(targetAgent.Subagents) > 0 {
			specRows = append(specRows, kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("Allowed Delegation:")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Foreground(t.Color.Text.Secondary).
						WhiteSpace(style.WhiteSpacePreWrap).
						OverflowWrap(style.OverflowWrapBreakWord),
				}, kitex.Text(strings.Join(targetAgent.Subagents, ", "))),
			))
		}

		children := []kitex.Node{
			// Header
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).Gap(1).MarginBottom(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(agentColor)}, icon.Robot),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(agentColor).Bold(true)}, kitex.Text(strings.ToUpper(targetAgent.Name))),
			),
			// Description
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Foreground(t.Color.Text.Primary).
					Italic(true).
					MarginBottom(1).
					WhiteSpace(style.WhiteSpacePreWrap).
					OverflowWrap(style.OverflowWrapBreakWord),
			}, kitex.Text(desc)),
		}

		// Specifications
		if len(specRows) > 0 {
			children = append(children, kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Gap(1).
					Padding(1).
					Background(color.RGBA{R: 0x16, G: 0x16, B: 0x1e, A: 0xff}),
			}, specRows...))
		}

		// Instructions / Prompt Preview
		if targetAgent.Instructions != "" {
			instructionsText := strings.TrimSpace(targetAgent.Instructions)
			children = append(children, kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0).MarginTop(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("SYSTEM PERSONA:")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Foreground(t.Color.Text.Secondary).
						Padding(1).
						Background(color.RGBA{R: 0x0f, G: 0x0f, B: 0x14, A: 0xff}).
						Border(style.SingleBorder().Color(t.Color.Surface.Tertiary)).
						WhiteSpace(style.WhiteSpacePreWrap).
						OverflowWrap(style.OverflowWrapBreakWord),
				}, kitex.Text(instructionsText)),
			))
		}

		// Footer: action hint
		children = append(children, kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).JustifyContent(style.JustifyEnd).Foreground(t.Color.Text.Tertiary).PaddingTop(1),
		},
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(actionColor).Bold(true)}, kitex.Text("PRESS ENTER TO SELECT AGENT")),
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
		agentName, ok := item.Value.(string)
		if !ok {
			return
		}

		// Find the active session to get the current provider/model configuration
		sessionsResp, err := client.ListSessions(context.Background(), api.ListSessionsRequest{})
		if err != nil {
			toast.AddErrorMessage("Agent Switch Failed", err.Error())
			return
		}

		var currentSession *api.Session
		for _, s := range sessionsResp.Sessions {
			if s.ID == activeSessionID {
				currentSession = &s
				break
			}
		}

		if currentSession == nil {
			toast.AddErrorMessage("Agent Switch Failed", "Active session not found")
			return
		}

		promise.New(func(ctx context.Context) (any, error) {
			_, err := client.ConfigureSession(ctx, api.ConfigureSessionRequest{
				SessionID:    activeSessionID,
				ProviderName: currentSession.Settings.ProviderName,
				ModelName:    currentSession.Settings.ModelName,
				AgentName:    agentName,
			})
			return nil, err
		}).Then(func(any) {
			windClient.InvalidateQueries(api.ListSessionsRequest{})
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: activeSessionID})
			active.SetModal("")
		}, func(err error) {
			toast.AddErrorMessage("Agent Switch Failed", err.Error())
		})
	}

	onClose := func() {
		active.SetModal("")
	}

	return components.Picker(components.PickerProps{
		IsOpen:        true,
		Title:         "SWITCH AGENT",
		Placeholder:   "Search agent name...",
		Groups:        groups,
		PreviewWidth:  36,
		RenderPreview: renderPreview,
		OnSelect:      onSelect,
		OnClose:       onClose,
		Attributes:    map[string]string{"data-context": "modal:agentpicker"},
	})
})
