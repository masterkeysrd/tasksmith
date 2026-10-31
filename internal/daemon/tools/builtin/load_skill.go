package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/strutils"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

type loadSkillTool struct {
	skills map[string]*warp.Skill
	ws     *workspace.Workspace
}

func newLoadSkillTool(skills []warp.Skill, ws *workspace.Workspace) *loadSkillTool {
	allowed := make(map[string]*warp.Skill, len(skills))
	for i := range skills {
		sk := &skills[i]
		allowed[strings.ToLower(sk.Metadata.Name)] = sk
	}

	return &loadSkillTool{
		skills: allowed,
		ws:     ws,
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

	sk, ok := t.skills[strings.ToLower(name)]
	if !ok {
		return api.LoadSkillOutput{}, fmt.Errorf("skill %q is not accessible to this agent", name)
	}

	var wsSpec *warp.Workspace
	var project *warp.Project
	if t.ws != nil && t.ws.Catalog() != nil {
		wsSpec = t.ws.Catalog().WorkspaceSpec()
		project = t.ws.Catalog().Project()
	}

	positionalArgs := workspace.BuildPositionalArgs(sk.Spec.Arguments, in.Arguments)
	globals := workspace.MergeStringArgs(in.Arguments, nil)
	rendered, err := sk.Render(&warp.SkillRenderOptions{
		Workspace: wsSpec,
		Project:   project,
		Args:      positionalArgs,
		Globals:   globals,
	})
	if err != nil {
		return api.LoadSkillOutput{}, err
	}

	var skillPath string
	if t.ws != nil {
		skillPath = t.ws.PortablePath(sk.Directory)
	}

	instructions, truncated := truncateSkillInstructions(rendered, skillPath)

	return api.LoadSkillOutput{
		Skill:        name,
		Path:         skillPath,
		Instructions: instructions,
		Truncated:    truncated,
	}, nil
}

func truncateSkillInstructions(rendered, skillPath string) (string, bool) {
	head, truncated := strutils.TruncateHead(rendered, tools.DefaultSpillMaxLines, tools.DefaultSpillMaxBytes)
	if !truncated {
		return rendered, false
	}

	if strings.TrimSpace(skillPath) != "" {
		return fmt.Sprintf("%s\n... [skill instructions truncated — use `ls`, `glob`, `grep`, and `view` (with offset) in %s to inspect the skill files and remaining content] ...", head, skillPath), true
	}
	return fmt.Sprintf("%s\n... [skill instructions truncated — use `ls`, `glob`, `grep`, and `view` (with offset) to inspect the skill files and remaining content] ...", head), true
}
