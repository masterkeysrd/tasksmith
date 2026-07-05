package resolver

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/agent/prompt"
	"github.com/masterkeysrd/warp"
)

// loadResourceSkill loads skill instructions from the resolved skill path.
func (r *Resolver) loadResourceSkill(ctx context.Context, skillPath string, agentName string) (ResolvedResource, error) {
	skillName := filepath.Base(skillPath)
	skill, resolvedAgent, err := r.findAgentSkill(ctx, skillName, agentName)
	if err != nil {
		return nil, err
	}

	absPath := skillPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(r.Cwd, absPath)
	}
	absPath = filepath.Clean(absPath)

	name := skill.Metadata.Name
	if name == "" {
		name = filepath.Base(skill.Directory)
	}

	var wsSpec *warp.Workspace
	var proj *warp.Project
	if wsSpecGetter, ok := r.Workspace.(interface{ WorkspaceSpec() *warp.Workspace }); ok {
		wsSpec = wsSpecGetter.WorkspaceSpec()
	}
	if projGetter, ok := r.Workspace.(interface{ Project() *warp.Project }); ok {
		proj = projGetter.Project()
	}

	var agent *warp.Agent
	if resolvedAgent != nil {
		agent = resolvedAgent.Agent
	}

	instructions, err := prompt.RenderSkill(skill, agent, wsSpec, proj, nil)
	if err != nil {
		instructions = skill.Spec.Instructions
	}

	return &ResolvedSkill{
		Name:         strings.TrimSuffix(name, filepath.Ext(name)),
		SkillPath:    absPath,
		Instructions: instructions,
	}, nil
}
