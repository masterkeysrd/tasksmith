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

// AgentManager defines the interface for invoking and managing child subagent sessions.
type AgentManager interface {
	InvokeSubagent(ctx context.Context, in api.InvokeAgentInput) (api.InvokeAgentOutput, error)
	ManageSubagents(ctx context.Context, in api.ManageAgentsInput) (api.ManageAgentsOutput, error)
}

type invokeAgentTool struct {
	agents    AgentManager
	subagents map[string]*warp.Agent
}

func newInvokeAgentTool(mgr AgentManager, subagents []*warp.Agent) *invokeAgentTool {
	return &invokeAgentTool{
		agents:    mgr,
		subagents: subagentRosterIndex(subagents),
	}
}

// subagentRosterIndex indexes roster agents by short and qualified name,
// case-insensitively, so tools can validate and canonicalize agent
// references identically.
func subagentRosterIndex(subagents []*warp.Agent) map[string]*warp.Agent {
	allowed := make(map[string]*warp.Agent, len(subagents)*2)
	for _, ag := range subagents {
		if ag == nil {
			continue
		}
		if ag.Metadata.Name != "" {
			allowed[strings.ToLower(ag.Metadata.Name)] = ag
		}
		if qn := ag.QualifiedName(); qn != "" {
			allowed[strings.ToLower(qn)] = ag
		}
	}
	return allowed
}

func (t *invokeAgentTool) Prepare(_ context.Context, in api.InvokeAgentInput) (*tools.Plan, error) {
	agent := strings.TrimSpace(in.Agent)
	title := strings.TrimSpace(in.Title)
	prompt := strings.TrimSpace(in.Prompt)
	if agent == "" {
		return nil, fmt.Errorf("%w: agent is required", std.ErrInvalidArgument)
	}
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", std.ErrInvalidArgument)
	}
	if prompt == "" {
		return nil, fmt.Errorf("%w: prompt is required", std.ErrInvalidArgument)
	}

	wsMode := strings.TrimSpace(in.Workspace)
	if wsMode != "" && wsMode != "inherit" && wsMode != "worktree" {
		return nil, fmt.Errorf("%w: invalid workspace mode %q, must be 'inherit' or 'worktree'", std.ErrInvalidArgument, in.Workspace)
	}

	return &tools.Plan{
		ToolName: ToolInvokeAgent,
		Action:   api.ActionRead,
		Safety:   api.SafetySafe,
		Summary:  fmt.Sprintf("Delegate to %s: %s", agent, title),
		ReadOnly: true,
	}, nil
}

func (t *invokeAgentTool) Execute(ctx context.Context, in api.InvokeAgentInput) (api.InvokeAgentOutput, error) {
	agent := strings.TrimSpace(in.Agent)
	title := strings.TrimSpace(in.Title)
	prompt := strings.TrimSpace(in.Prompt)
	if agent == "" {
		return api.InvokeAgentOutput{}, fmt.Errorf("%w: agent is required", std.ErrInvalidArgument)
	}
	if title == "" {
		return api.InvokeAgentOutput{}, fmt.Errorf("%w: title is required", std.ErrInvalidArgument)
	}
	if prompt == "" {
		return api.InvokeAgentOutput{}, fmt.Errorf("%w: prompt is required", std.ErrInvalidArgument)
	}

	wsMode := strings.TrimSpace(in.Workspace)
	if wsMode != "" && wsMode != "inherit" && wsMode != "worktree" {
		return api.InvokeAgentOutput{}, fmt.Errorf("%w: invalid workspace mode %q", std.ErrInvalidArgument, in.Workspace)
	}

	resolvedAgent, ok := t.subagents[strings.ToLower(agent)]
	if !ok {
		return api.InvokeAgentOutput{}, fmt.Errorf("subagent %q is not in the allowed subagents roster", agent)
	}

	in.Agent = resolvedAgent.Metadata.Name
	in.Title = title
	in.Prompt = prompt
	in.Workspace = wsMode
	in.Model = strings.TrimSpace(in.Model)

	return t.agents.InvokeSubagent(ctx, in)
}

type manageAgentsTool struct {
	agents    AgentManager
	subagents map[string]*warp.Agent
}

func newManageAgentsTool(mgr AgentManager, subagents []*warp.Agent) *manageAgentsTool {
	return &manageAgentsTool{
		agents:    mgr,
		subagents: subagentRosterIndex(subagents),
	}
}

// resolveRosterAgent canonicalizes an agent reference against the roster,
// mirroring invoke_agent so both tools accept and reject the same names.
func (t *manageAgentsTool) resolveRosterAgent(agent string) (*warp.Agent, error) {
	resolved, ok := t.subagents[strings.ToLower(agent)]
	if !ok || resolved == nil {
		return nil, fmt.Errorf("%w: subagent %q is not in the allowed subagents roster", std.ErrInvalidArgument, agent)
	}
	return resolved, nil
}

func (t *manageAgentsTool) Prepare(_ context.Context, in api.ManageAgentsInput) (*tools.Plan, error) {
	action := strings.TrimSpace(in.Action)
	sessionID := strings.TrimSpace(in.SessionID)
	msg := strings.TrimSpace(in.Message)
	agent := strings.TrimSpace(in.Agent)

	switch action {
	case "list":
		return &tools.Plan{
			ToolName: ToolManageAgents,
			Action:   api.ActionRead,
			Safety:   api.SafetySafe,
			Summary:  "List subagents",
			ReadOnly: true,
		}, nil
	case "models":
		q := strings.TrimSpace(in.Query)
		if agent != "" {
			if _, err := t.resolveRosterAgent(agent); err != nil {
				return nil, err
			}
		}
		if q != "" {
			return &tools.Plan{
				ToolName: ToolManageAgents,
				Action:   api.ActionRead,
				Safety:   api.SafetySafe,
				Summary:  fmt.Sprintf("Search models %q", q),
				ReadOnly: true,
			}, nil
		}
		return &tools.Plan{
			ToolName: ToolManageAgents,
			Action:   api.ActionRead,
			Safety:   api.SafetySafe,
			Summary:  "List models",
			ReadOnly: true,
		}, nil
	case "status":
		if sessionID == "" {
			return nil, fmt.Errorf("%w: session_id is required for status action", std.ErrInvalidArgument)
		}
		return &tools.Plan{
			ToolName: ToolManageAgents,
			Action:   api.ActionRead,
			Safety:   api.SafetySafe,
			Summary:  fmt.Sprintf("Check status of subagent %s", sessionID),
			ReadOnly: true,
		}, nil
	case "send":
		if sessionID == "" {
			return nil, fmt.Errorf("%w: session_id is required for send action", std.ErrInvalidArgument)
		}
		if msg == "" {
			return nil, fmt.Errorf("%w: message is required for send action", std.ErrInvalidArgument)
		}
		return &tools.Plan{
			ToolName: ToolManageAgents,
			Action:   api.ActionExec,
			Safety:   api.SafetySafe,
			Summary:  fmt.Sprintf("Send message to subagent %s", sessionID),
			ReadOnly: false,
		}, nil
	case "kill":
		if sessionID == "" {
			return nil, fmt.Errorf("%w: session_id is required for kill action", std.ErrInvalidArgument)
		}
		return &tools.Plan{
			ToolName: ToolManageAgents,
			Action:   api.ActionExec,
			Safety:   api.SafetySafe,
			Summary:  fmt.Sprintf("Kill subagent %s", sessionID),
			ReadOnly: false,
		}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported action %q, must be 'list', 'status', 'send', 'kill', or 'models'", std.ErrInvalidArgument, in.Action)
	}
}

func (t *manageAgentsTool) Execute(ctx context.Context, in api.ManageAgentsInput) (api.ManageAgentsOutput, error) {
	action := strings.TrimSpace(in.Action)
	sessionID := strings.TrimSpace(in.SessionID)
	msg := strings.TrimSpace(in.Message)
	agent := strings.TrimSpace(in.Agent)

	switch action {
	case "list":
	case "models":
		if agent != "" {
			resolved, err := t.resolveRosterAgent(agent)
			if err != nil {
				return api.ManageAgentsOutput{}, err
			}
			agent = resolved.Metadata.Name
		}
	case "status":
		if sessionID == "" {
			return api.ManageAgentsOutput{}, fmt.Errorf("%w: session_id is required for status action", std.ErrInvalidArgument)
		}
	case "send":
		if sessionID == "" {
			return api.ManageAgentsOutput{}, fmt.Errorf("%w: session_id is required for send action", std.ErrInvalidArgument)
		}
		if msg == "" {
			return api.ManageAgentsOutput{}, fmt.Errorf("%w: message is required for send action", std.ErrInvalidArgument)
		}
	case "kill":
		if sessionID == "" {
			return api.ManageAgentsOutput{}, fmt.Errorf("%w: session_id is required for kill action", std.ErrInvalidArgument)
		}
	default:
		return api.ManageAgentsOutput{}, fmt.Errorf("%w: unsupported action %q", std.ErrInvalidArgument, in.Action)
	}

	in.Action = action
	in.SessionID = sessionID
	in.Message = msg
	in.Query = strings.TrimSpace(in.Query)
	in.Provider = strings.TrimSpace(in.Provider)
	in.Agent = agent

	return t.agents.ManageSubagents(ctx, in)
}
