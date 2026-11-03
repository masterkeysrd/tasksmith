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
	skills      map[string]*warp.Skill
	ws          *workspace.Workspace
	agentName   string
	agent       *warp.Agent
	deferredSet map[string]bool
}

func newLoadSkillTool(skills []warp.Skill, ws *workspace.Workspace, agentName string, agent *warp.Agent, deferredTools []string) *loadSkillTool {
	allowed := make(map[string]*warp.Skill, len(skills)*3)
	for i := range skills {
		sk := &skills[i]
		// Index each skill by its short name, its fully qualified
		// "namespace/Kind/name" reference, and the "namespace/name" shorthand so
		// load_skill accepts any of the three forms. The short name is added
		// last so it wins when a namespace collides with a short name.
		if qualified := sk.QualifiedName(); qualified != "" {
			allowed[strings.ToLower(qualified)] = sk
		}
		if sk.Namespace != "" {
			allowed[strings.ToLower(sk.Namespace+"/"+sk.Metadata.Name)] = sk
		}
		allowed[strings.ToLower(sk.Metadata.Name)] = sk
	}

	deferredSet := make(map[string]bool, len(deferredTools))
	for _, name := range deferredTools {
		deferredSet[strings.ToLower(name)] = true
	}

	return &loadSkillTool{
		skills:      allowed,
		ws:          ws,
		agentName:   agentName,
		agent:       agent,
		deferredSet: deferredSet,
	}
}

func (t *loadSkillTool) Prepare(_ context.Context, in api.LoadSkillInput) (*tools.Plan, error) {
	name := strings.TrimSpace(in.Skill)
	if name == "" {
		return nil, fmt.Errorf("%w: skill name is required", std.ErrInvalidArgument)
	}

	return &tools.Plan{
		ToolName: ToolLoadSkill,
		Action:   api.ActionRead,
		Safety:   api.SafetySafe,
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
		Agent:     t.agent,
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

	out := api.LoadSkillOutput{
		Skill:        sk.Metadata.Name,
		Path:         skillPath,
		Instructions: instructions,
		Truncated:    truncated,
	}

	// Resolve the skill's declared tools against the workspace and agent policy
	// ceilings and activate the deferred ones into the model's context via
	// tool-reference blocks (rendered by LoadSkillOutput.ToolContent).
	if t.ws != nil && t.ws.Catalog() != nil {
		skillRef := sk.QualifiedName()
		if skillRef == "" {
			skillRef = sk.Metadata.Name
		}
		if skillTools, err := t.ws.Catalog().ToolsForSkill(t.agentName, skillRef); err == nil {
			out.Tools = t.deferredSkillToolNames(skillTools)
		}
	}

	return out, nil
}

// deferredSkillToolNames returns the names of the policy-allowed skill tools that
// are currently deferred (DeferLoading: true), deduplicated and in deterministic
// order. When an explicit deferred set was configured, a tool is activated only
// when it appears in that set; otherwise a tool is activated when it is not part
// of the active agent's eager resolved tools.
func (t *loadSkillTool) deferredSkillToolNames(skillTools []*warp.Tool) []string {
	if len(skillTools) == 0 {
		return nil
	}

	deferred := t.deferredSet
	if len(deferred) == 0 {
		deferred = t.derivedDeferredSet()
	}
	if len(deferred) == 0 {
		return nil
	}

	var names []string
	seen := make(map[string]bool)
	for _, wt := range skillTools {
		name := wt.Metadata.Name
		if !deferred[strings.ToLower(name)] || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// derivedDeferredSet computes the deferred tool set from the active agent's
// partition when no explicit deferred set was configured on the tool.
func (t *loadSkillTool) derivedDeferredSet() map[string]bool {
	set := make(map[string]bool)
	if t.ws == nil || t.ws.Catalog() == nil {
		return set
	}
	resolved, err := t.ws.Catalog().ResolveAgent(t.agentName)
	if err != nil || resolved == nil {
		return set
	}
	for _, name := range t.ws.Catalog().PartitionAgentTools(resolved).Deferred {
		set[strings.ToLower(name)] = true
	}
	return set
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
