package effortpicker

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/internal/agent/model"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
)

type ViewProps struct{}

// View renders a picker popup for selecting the model's reasoning effort level.
var View = kitex.FC("EffortPickerView", func(props ViewProps) kitex.Node {
	isOpen := active.UseModal() == "effortpicker"
	if !isOpen {
		return nil
	}

	sessionID := active.UseSessionID()
	client := tuiapi.UseClient()
	windClient := wind.UseClient()
	sessionsQuery := queries.UseListSessions()
	providersQuery := queries.UseListProviders()

	// 1. Resolve current session settings & model
	var currentSession api.Session
	foundSession := false
	if sessionsQuery.Data != nil && sessionID != "" {
		for _, s := range sessionsQuery.Data.Sessions {
			if s.ID == sessionID {
				currentSession = s
				foundSession = true
				break
			}
		}
	}

	if !foundSession {
		return nil
	}

	// 2. Resolve active model capabilities
	var activeModel api.Model
	foundModel := false
	if currentSession.Settings.ModelName != "" && providersQuery.Data != nil {
		for _, p := range providersQuery.Data.Providers {
			if p.Name == currentSession.Settings.ProviderName {
				for _, m := range p.Models {
					if m.ID == currentSession.Settings.ModelName {
						activeModel = m
						foundModel = true
						break
					}
				}
				break
			}
		}
	}

	// 3. Gather supported effort values
	var effortValues []string
	if foundModel && activeModel.Capabilities.Reasoning {
		for _, opt := range activeModel.Capabilities.ReasoningOptions {
			if opt.Type == "effort" {
				effortValues = opt.Values
				break
			}
		}
	}

	if len(effortValues) == 0 {
		effortValues = []string{"low", "medium", "high"}
	}

	// 4. Build items for the picker
	var items []components.PickerItem
	for _, val := range effortValues {
		label := strings.Title(val)
		sublabel := fmt.Sprintf("Set reasoning effort level to %s", val)
		items = append(items, components.PickerItem{
			ID:       val,
			Label:    label,
			Sublabel: sublabel,
			Value:    val,
		})
	}

	onSelect := func(item components.PickerItem) {
		selectedEffort := item.Value.(string)

		promise.New(func(ctx context.Context) (any, error) {
			// Fetch fresh list to avoid stale closure updates
			sessionsResp, err := client.ListSessions(ctx, api.ListSessionsRequest{})
			if err != nil {
				return nil, err
			}
			var sess api.Session
			foundSess := false
			for _, s := range sessionsResp.Sessions {
				if s.ID == sessionID {
					sess = s
					foundSess = true
					break
				}
			}
			if !foundSess {
				return nil, fmt.Errorf("session not found")
			}

			newSettings := sess.Settings
			if newSettings.Thinking == nil {
				newSettings.Thinking = &model.SessionThinkingSetting{}
			}
			newSettings.Thinking.Effort = &selectedEffort
			// Setting effort implicitly enables thinking
			enabledVal := true
			newSettings.Thinking.Enabled = &enabledVal

			_, err = client.ConfigureSession(ctx, api.ConfigureSessionRequest{
				SessionID:    sessionID,
				ProviderName: sess.Settings.ProviderName,
				ModelName:    sess.Settings.ModelName,
				AgentName:    sess.Settings.AgentName,
				Settings:     &newSettings,
			})
			return nil, err
		}).Then(func(any) {
			windClient.InvalidateQueries(api.ListSessionsRequest{})
			active.SetModal("")
			active.SetStatusMessage(fmt.Sprintf("Thinking effort set to: %s", strings.ToUpper(selectedEffort)))
		}, func(err error) {
			// Close modal on error, and let the caller show feedback
			active.SetModal("")
		})
	}

	onClose := func() {
		active.SetModal("")
	}

	return components.Picker(components.PickerProps{
		IsOpen:      true,
		Title:       "SELECT REASONING EFFORT",
		Placeholder: "Search effort level...",
		Items:       items,
		OnSelect:    onSelect,
		OnClose:     onClose,
		Attributes:  map[string]string{"data-context": "modal:effortpicker"},
	})
})
