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

type loadRuleTool struct {
	rules map[string]*warp.Rule
	ws    *workspace.Workspace
	agent *warp.Agent
}

func newLoadRuleTool(rules []warp.Rule, ws *workspace.Workspace, agent *warp.Agent) *loadRuleTool {
	allowed := make(map[string]*warp.Rule, len(rules)*3)
	for i := range rules {
		rl := &rules[i]
		// Index each rule by its fully qualified "namespace/Kind/name"
		// reference, the "namespace/name" shorthand, and the short name so
		// load_rule accepts any of the three forms. The short name is added
		// last so it wins when a namespace collides with a short name.
		if qualified := rl.QualifiedName(); qualified != "" {
			allowed[strings.ToLower(qualified)] = rl
		}
		if rl.Namespace != "" {
			allowed[strings.ToLower(rl.Namespace+"/"+rl.Metadata.Name)] = rl
		}
		allowed[strings.ToLower(rl.Metadata.Name)] = rl
	}

	return &loadRuleTool{
		rules: allowed,
		ws:    ws,
		agent: agent,
	}
}

func (t *loadRuleTool) Prepare(_ context.Context, in api.LoadRuleInput) (*tools.Plan, error) {
	name := strings.TrimSpace(in.Rule)
	if name == "" {
		return nil, fmt.Errorf("%w: rule name is required", std.ErrInvalidArgument)
	}

	return &tools.Plan{
		ToolName: ToolLoadRule,
		Action:   api.ActionRead,
		Safety:   api.SafetySafe,
		Summary:  fmt.Sprintf("Load rule %q", name),
		ReadOnly: true,
	}, nil
}

func (t *loadRuleTool) Execute(_ context.Context, in api.LoadRuleInput) (api.LoadRuleOutput, error) {
	name := strings.TrimSpace(in.Rule)
	if name == "" {
		return api.LoadRuleOutput{}, fmt.Errorf("%w: rule name is required", std.ErrInvalidArgument)
	}

	rl, ok := t.rules[strings.ToLower(name)]
	if !ok {
		return api.LoadRuleOutput{}, fmt.Errorf("rule %q is not accessible to this agent", name)
	}

	var wsSpec *warp.Workspace
	var project *warp.Project
	if t.ws != nil && t.ws.Catalog() != nil {
		wsSpec = t.ws.Catalog().WorkspaceSpec()
		project = t.ws.Catalog().Project()
	}

	rendered, err := rl.Render(&warp.RuleRenderOptions{
		Workspace: wsSpec,
		Project:   project,
		Agent:     t.agent,
	})
	if err != nil {
		return api.LoadRuleOutput{}, err
	}

	var rulePath string
	if t.ws != nil {
		rulePath = t.ws.PortablePath(rl.GetPath())
	}

	head, truncated := strutils.TruncateHead(rendered, tools.DefaultSpillMaxLines, tools.DefaultSpillMaxBytes)

	return api.LoadRuleOutput{
		Rule:         rl.Metadata.Name,
		Instructions: head,
		Path:         rulePath,
		Priority:     rl.Spec.Priority,
		Truncated:    truncated,
	}, nil
}
