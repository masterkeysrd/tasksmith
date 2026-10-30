package store

import (
	"context"
	"fmt"

	"github.com/masterkeysrd/tasksmith/api"
)

func CurrentThinkingModel(ctx context.Context) (*api.Model, error) {
	c := Client()
	state := GetSession()
	if state.Session != nil {
		if state.Session.Model != nil {
			return state.Session.Model, nil
		}
		return nil, fmt.Errorf("model capabilities are unavailable")
	}
	preview, err := c.PreviewSession(ctx, api.PreviewSessionRequest{Config: state.Config})
	if err != nil {
		return nil, err
	}
	if preview == nil || preview.Model == nil {
		return nil, fmt.Errorf("model capabilities are unavailable")
	}
	return preview.Model, nil
}

const ThinkingProviderDefault = "provider default"

func thinkingDisplay(setting *api.ThinkingSetting) string {
	if setting == nil {
		return ThinkingProviderDefault
	}
	switch setting.Type {
	case "effort":
		return setting.Effort
	case "budget", "budget_tokens":
		return fmt.Sprintf("budget %d tokens", setting.Budget)
	case "toggle":
		if setting.Enabled {
			return "thinking on"
		}
		return "thinking off"
	}
	return ThinkingProviderDefault
}

func ApplyThinkingChange(setting *api.ThinkingSetting) {
	session.Set(func(s SessionState) SessionState {
		s.Config.Thinking = setting
		if s.Session != nil {
			updated := *s.Session
			updated.Config = s.Config
			s.Session = &updated
		}
		s.Thinking = thinkingDisplay(setting)
		return s
	})
	if setting == nil {
		state := GetSession()
		if state.Session != nil {
			label, value := modelDisplay(state.Session.Model, nil)
			SetModelInfo(label, value)
		} else {
			updateDraftModel(context.Background(), Client(), state.Config)
		}
	}
}

func SetThinking(setting api.ThinkingSetting, command, description string) {
	state := GetSession()
	if state.Session != nil && state.Session.ID != "" {
		c := Client()
		go func() {
			updated, err := c.UpdateSession(context.Background(), state.Session.ID, api.UpdateSessionRequest{Config: api.SessionConfig{Thinking: &setting}})
			if err != nil {
				AppendError("Error", err.Error())
				return
			}
			SetSession(updated)
			showSessionDiagnostics(updated, false)
		}()
		return
	}
	session.Set(func(s SessionState) SessionState {
		if setting.Type == "default" {
			s.Config.Thinking = nil
		} else {
			value := setting
			s.Config.Thinking = &value
		}
		return s
	})
	updateDraftModel(context.Background(), Client(), GetSession().Config)
	AppendCommandOutput(command, description)
}
