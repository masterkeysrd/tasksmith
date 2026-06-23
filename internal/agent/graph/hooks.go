package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/agent/prompt"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
	"github.com/masterkeysrd/warp"
)

type skillResolver struct {
	ws        *workspace.Workspace
	agentName string
}

func (r *skillResolver) ResolveSkill(ctx context.Context, name string) (string, string, error) {
	if r.ws == nil {
		return "", "", fmt.Errorf("workspace is nil")
	}

	resolvedAgent, err := r.ws.ResolveAgent(ctx, r.agentName)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve agent %q: %w", r.agentName, err)
	}

	var foundSkill *warp.Skill
	// Match skill name in the agent's resolved skills
	for _, s := range resolvedAgent.Skills {
		if strings.EqualFold(s.Metadata.Name, name) ||
			strings.EqualFold(filepath.Base(s.Metadata.Name), name) ||
			strings.EqualFold(strings.TrimSuffix(filepath.Base(s.Metadata.Name), ".md"), name) {
			foundSkill = &s
			break
		}
	}

	// Match in all workspace resources as a fallback
	if foundSkill == nil {
		for _, res := range r.ws.Resources() {
			if res.GetKind() == warp.KindSkill {
				if strings.EqualFold(res.GetName(), name) ||
					strings.EqualFold(filepath.Base(res.GetName()), name) ||
					strings.EqualFold(strings.TrimSuffix(filepath.Base(res.GetName()), ".md"), name) {
					if skill, ok := res.(*warp.Skill); ok {
						foundSkill = skill
						break
					}
				}
			}
		}
	}

	if foundSkill == nil {
		return "", "", fmt.Errorf("skill %q not found for agent %q", name, r.agentName)
	}

	// Render instructions with variables context
	instructions, err := prompt.RenderSkill(
		foundSkill,
		resolvedAgent.Agent,
		r.ws.WorkspaceSpec(),
		r.ws.Project(),
		nil,
	)
	if err != nil {
		return "", "", fmt.Errorf("failed to render skill %q: %w", name, err)
	}

	return instructions, foundSkill.Directory, nil
}

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
