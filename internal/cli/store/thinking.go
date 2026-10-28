package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
)

func CurrentThinkingModel(ctx context.Context) (*api.Model, error) {
	c := Client()
	if c == nil {
		return nil, fmt.Errorf("model capabilities are unavailable")
	}
	state := GetSession()
	cfg := state.Config
	if state.Session != nil {
		cfg = state.Session.Config
	}
	sessMu.Lock()
	model := strings.TrimSpace(cfg.Model)
	agent := strings.TrimSpace(cfg.Agent)
	if model == "" {
		model = strings.TrimSpace(activeOpts.Model)
	}
	if agent == "" {
		agent = strings.TrimSpace(activeOpts.Agent)
	}
	sessMu.Unlock()
	if model == "" {
		ws, err := c.GetWorkspace(ctx)
		if err != nil {
			return nil, err
		}
		if agent == "" {
			agent = ws.DefaultAgent
		}
		if agent != "" {
			if ag, err := c.GetAgent(ctx, agent); err == nil && len(ag.Models) > 0 {
				model = ag.Models[0]
			}
		}
		if model == "" {
			model = ws.DefaultModel
		}
	}
	if model == "" {
		return nil, fmt.Errorf("model capabilities are unavailable")
	}
	return c.GetModel(ctx, model)
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
		c := Client()
		if state.Session != nil && c != nil {
			id := state.Session.ID
			go func() {
				label, value := ResolveModelInfo(context.Background(), c, state.Config.Agent, state.Config.Model, state.Session)
				current := GetSession()
				if current.Session != nil && current.Session.ID == id && current.Config.Thinking == nil {
					SetModelInfo(label, value)
				}
			}()
		}
	}
}

func SetThinking(setting api.ThinkingSetting, command, description string) {
	state := GetSession()
	if state.Session != nil && state.Session.ID != "" {
		c := Client()
		if c == nil {
			AppendError("Error", "model capabilities are unavailable")
			return
		}
		go func() {
			updated, err := c.UpdateSession(context.Background(), state.Session.ID, api.UpdateSessionRequest{Config: api.SessionConfig{Thinking: &setting}})
			if err != nil {
				AppendError("Error", err.Error())
				return
			}
			SetSession(updated)
			label, thinking := ResolveModelInfo(context.Background(), c, updated.Config.Agent, updated.Config.Model, updated)
			SetModelInfo(label, thinking)
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
	label, thinking := ResolveModelInfo(context.Background(), Client(), state.Config.Agent, state.Config.Model, &api.Session{Config: GetSession().Config})
	SetModelInfo(label, thinking)
	AppendCommandOutput(command, description)
}
