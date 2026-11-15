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
	return &invokeAgentTool{
		agents:    mgr,
		subagents: allowed,
	}
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

	return t.agents.InvokeSubagent(ctx, in)
}

type manageAgentsTool struct {
	agents AgentManager
}

func (t *manageAgentsTool) Prepare(_ context.Context, in api.ManageAgentsInput) (*tools.Plan, error) {
	action := strings.TrimSpace(in.Action)
	sessionID := strings.TrimSpace(in.SessionID)
	msg := strings.TrimSpace(in.Message)

	switch action {
	case "list":
		return &tools.Plan{
			ToolName: ToolManageAgents,
			Action:   api.ActionRead,
			Safety:   api.SafetySafe,
			Summary:  "List subagents",
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
		return nil, fmt.Errorf("%w: unsupported action %q, must be 'list', 'status', 'send', or 'kill'", std.ErrInvalidArgument, in.Action)
	}
}

func (t *manageAgentsTool) Execute(ctx context.Context, in api.ManageAgentsInput) (api.ManageAgentsOutput, error) {
	action := strings.TrimSpace(in.Action)
	sessionID := strings.TrimSpace(in.SessionID)
	msg := strings.TrimSpace(in.Message)

	switch action {
	case "list":
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

	return t.agents.ManageSubagents(ctx, in)
}
