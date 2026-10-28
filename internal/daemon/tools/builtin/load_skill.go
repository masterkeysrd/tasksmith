package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/warp"
)

// SkillRenderer defines the function signature for rendering a skill template.
type SkillRenderer func(name string, args map[string]string) (string, error)

type loadSkillTool struct {
	skills map[string]struct{}
	render SkillRenderer
}

func newLoadSkillTool(skills []warp.Skill, render SkillRenderer) *loadSkillTool {
	allowed := make(map[string]struct{}, len(skills))
	for _, sk := range skills {
		allowed[strings.ToLower(sk.Metadata.Name)] = struct{}{}
	}

	return &loadSkillTool{
		skills: allowed,
		render: render,
	}
}

func (t *loadSkillTool) Prepare(_ context.Context, in api.LoadSkillInput) (*tools.Plan, error) {
	name := strings.TrimSpace(in.Skill)
	if name == "" {
		return nil, fmt.Errorf("%w: skill name is required", std.ErrInvalidArgument)
	}

	return &tools.Plan{
		ToolName: ToolLoadSkill,
		Action:   tools.ActionRead,
		Safety:   tools.SafetySafe,
		Summary:  fmt.Sprintf("Load skill %q", name),
		ReadOnly: true,
	}, nil
}

func (t *loadSkillTool) Execute(_ context.Context, in api.LoadSkillInput) (api.LoadSkillOutput, error) {
	name := strings.TrimSpace(in.Skill)
	if name == "" {
		return api.LoadSkillOutput{}, fmt.Errorf("%w: skill name is required", std.ErrInvalidArgument)
	}

	if _, ok := t.skills[strings.ToLower(name)]; !ok {
		return api.LoadSkillOutput{}, fmt.Errorf("skill %q is not accessible to this agent", name)
	}

	rendered, err := t.render(name, in.Arguments)
	if err != nil {
		return api.LoadSkillOutput{}, err
	}

	return api.LoadSkillOutput{
		Skill:        name,
		Instructions: rendered,
	}, nil
}
