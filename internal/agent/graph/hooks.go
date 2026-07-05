package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
)

// ToolStateHook defines a graph transition callback to modify state on successful tool call.
type ToolStateHook func(ctx context.Context, args map[string]any, a *AgentGraph) (func(AgentState) AgentState, error)

var toolStateHooks = map[string]ToolStateHook{
	"todos": func(ctx context.Context, args map[string]any, a *AgentGraph) (func(AgentState) AgentState, error) {
		data, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		var input struct {
			Todos []tools.Todo `json:"todos"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			return nil, err
		}

		if a.onTodosUpdated != nil {
			if err := a.onTodosUpdated(ctx, input.Todos); err != nil {
				return nil, fmt.Errorf("todos callback error: %w", err)
			}
		}

		return func(s AgentState) AgentState {
			s.Todos = input.Todos
			return s
		}, nil
	},
	"activate_skill": func(ctx context.Context, args map[string]any, a *AgentGraph) (func(AgentState) AgentState, error) {
		data, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		var input struct {
			Skill string `json:"skill"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			return nil, err
		}

		return func(s AgentState) AgentState {
			if input.Skill != "" {
				found := false
				for _, sk := range s.ActivatedSkills {
					if strings.EqualFold(sk, input.Skill) {
						found = true
						break
					}
				}
				if !found {
					s.ActivatedSkills = append(s.ActivatedSkills, input.Skill)
				}
			}
			return s
		}, nil
	},
}
