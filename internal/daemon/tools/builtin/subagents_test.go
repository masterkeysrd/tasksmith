package builtin_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

// subagentAllowedTools returns the full catalog tool names plus the dynamic
// subagent tool names, so fail-closed allowlists can register subagent tools
// in tests that previously relied on the nil-means-all semantics.
func subagentAllowedTools(ws *workspace.Workspace) []string {
	names := make([]string, 0, len(ws.Catalog().ToolNames())+2)
	names = append(names, ws.Catalog().ToolNames()...)
	names = append(names, builtin.ToolInvokeAgent, builtin.ToolManageAgents)
	return names
}

type mockAgentManager struct {
	invokeFunc func(ctx context.Context, in api.InvokeAgentInput) (api.InvokeAgentOutput, error)
	manageFunc func(ctx context.Context, in api.ManageAgentsInput) (api.ManageAgentsOutput, error)
}

func (m *mockAgentManager) InvokeSubagent(ctx context.Context, in api.InvokeAgentInput) (api.InvokeAgentOutput, error) {
	if m.invokeFunc != nil {
		return m.invokeFunc(ctx, in)
	}
	return api.InvokeAgentOutput{
		SessionID: "child-123",
		Agent:     in.Agent,
		Title:     in.Title,
		WorkDir:   "/path/to/workdir",
		Status:    "running",
		Message:   "spawned",
	}, nil
}

func (m *mockAgentManager) ManageSubagents(ctx context.Context, in api.ManageAgentsInput) (api.ManageAgentsOutput, error) {
	if m.manageFunc != nil {
		return m.manageFunc(ctx, in)
	}
	return api.ManageAgentsOutput{Message: "ok"}, nil
}

func TestSubagentToolsRegistration(t *testing.T) {
	dir, _, stdLib, sb, ws := setupBuiltinEnv(t, true)
	guard := shellguard.New(shellguard.Config{WorkspaceDir: dir})

	cfg := builtin.Config{
		Std:          stdLib,
		Sandbox:      sb,
		Guard:        guard,
		Workspace:    ws,
		AllowedTools: subagentAllowedTools(ws),
	}

	// Without Agents or Subagents, invoke_agent and manage_agents should NOT be present.
	toolsList, err := builtin.New(cfg)
	if err != nil {
		t.Fatalf("builtin.New failed: %v", err)
	}
	for _, tool := range toolsList {
		if tool.Name() == builtin.ToolInvokeAgent || tool.Name() == builtin.ToolManageAgents {
			t.Fatalf("unexpected subagent tool %q present when subagents are not configured", tool.Name())
		}
	}

	// With Agents and Subagents, they should be present.
	exploreAgent := &warp.Agent{
		BaseResource: warp.BaseResource{
			Metadata: warp.Metadata{
				Name: "explore",
			},
		},
	}
	mgr := &mockAgentManager{}
	cfg.Agents = mgr
	cfg.Subagents = []*warp.Agent{exploreAgent}

	toolsWithSubagents, err := builtin.New(cfg)
	if err != nil {
		t.Fatalf("builtin.New with subagents failed: %v", err)
	}

	var hasInvoke, hasManage bool
	for _, tool := range toolsWithSubagents {
		if tool.Name() == builtin.ToolInvokeAgent {
			hasInvoke = true
		}
		if tool.Name() == builtin.ToolManageAgents {
			hasManage = true
		}
	}
	if !hasInvoke {
		t.Errorf("expected %q to be registered", builtin.ToolInvokeAgent)
	}
	if !hasManage {
		t.Errorf("expected %q to be registered", builtin.ToolManageAgents)
	}
}

func TestInvokeAgentPrepareAndExecute(t *testing.T) {
	ctx := context.Background()
	dir, _, stdLib, sb, ws := setupBuiltinEnv(t, true)
	guard := shellguard.New(shellguard.Config{WorkspaceDir: dir})

	exploreAgent := &warp.Agent{
		BaseResource: warp.BaseResource{
			Metadata: warp.Metadata{
				Name: "explore",
			},
		},
	}
	mgr := &mockAgentManager{}
	cfg := builtin.Config{
		Std:          stdLib,
		Sandbox:      sb,
		Guard:        guard,
		Workspace:    ws,
		Agents:       mgr,
		Subagents:    []*warp.Agent{exploreAgent},
		AllowedTools: subagentAllowedTools(ws),
	}

	builtinTools, err := builtin.New(cfg)
	if err != nil {
		t.Fatalf("builtin.New failed: %v", err)
	}

	container, err := tools.NewContainer(tools.Config{
		Tools:        builtinTools,
		Sandbox:      sb,
		Workspace:    ws,
		Logger:       log.Default(),
		AllowedTools: subagentAllowedTools(ws),
	})
	if err != nil {
		t.Fatalf("tools.NewContainer failed: %v", err)
	}

	// Missing agent
	_, err = container.Prepare(ctx, &message.ToolCall{
		Name: builtin.ToolInvokeAgent,
		Args: map[string]any{"agent": "", "title": "Some title", "prompt": "Do work"},
	})
	if !errors.Is(err, std.ErrInvalidArgument) {
		t.Errorf("Prepare with empty agent error = %v, want %v", err, std.ErrInvalidArgument)
	}

	// Missing title
	_, err = container.Prepare(ctx, &message.ToolCall{
		Name: builtin.ToolInvokeAgent,
		Args: map[string]any{"agent": "explore", "title": "  ", "prompt": "Do work"},
	})
	if !errors.Is(err, std.ErrInvalidArgument) {
		t.Errorf("Prepare with empty title error = %v, want %v", err, std.ErrInvalidArgument)
	}

	// Missing prompt
	_, err = container.Prepare(ctx, &message.ToolCall{
		Name: builtin.ToolInvokeAgent,
		Args: map[string]any{"agent": "explore", "title": "Some title", "prompt": ""},
	})
	if !errors.Is(err, std.ErrInvalidArgument) {
		t.Errorf("Prepare with empty prompt error = %v, want %v", err, std.ErrInvalidArgument)
	}

	// Invalid workspace
	_, err = container.Prepare(ctx, &message.ToolCall{
		Name: builtin.ToolInvokeAgent,
		Args: map[string]any{"agent": "explore", "title": "Some title", "prompt": "Do work", "workspace": "invalid_mode"},
	})
	if !errors.Is(err, std.ErrInvalidArgument) {
		t.Errorf("Prepare with invalid workspace error = %v, want %v", err, std.ErrInvalidArgument)
	}

	// Valid prepare
	plan, err := container.Prepare(ctx, &message.ToolCall{
		Name: builtin.ToolInvokeAgent,
		Args: map[string]any{"agent": "explore", "title": "Find auth", "prompt": "Search auth tokens", "workspace": "inherit"},
	})
	if err != nil {
		t.Fatalf("Prepare valid failed: %v", err)
	}
	if plan.ToolName != builtin.ToolInvokeAgent {
		t.Errorf("plan.ToolName = %q, want %q", plan.ToolName, builtin.ToolInvokeAgent)
	}
	if !plan.ReadOnly {
		t.Errorf("plan.ReadOnly = %v, want true", plan.ReadOnly)
	}

	// Execute with unauthorized agent
	res, err := container.Execute(ctx, &message.ToolCall{
		ID:   "call-unauthorized",
		Name: builtin.ToolInvokeAgent,
		Args: map[string]any{"agent": "unknown-agent", "title": "Title", "prompt": "Prompt"},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("Execute with unknown subagent expected error result")
	}

	// Execute with allowed agent
	var calledWith api.InvokeAgentInput
	mgr.invokeFunc = func(c context.Context, in api.InvokeAgentInput) (api.InvokeAgentOutput, error) {
		calledWith = in
		return api.InvokeAgentOutput{
			SessionID: "child-456",
			Agent:     in.Agent,
			Title:     in.Title,
			WorkDir:   filepath.Join(dir, "workdir"),
			Status:    "running",
			Message:   "spawned child",
		}, nil
	}

	callRes, err := container.Execute(ctx, &message.ToolCall{
		ID:   "call-ok",
		Name: builtin.ToolInvokeAgent,
		Args: map[string]any{"agent": "EXPLORE", "title": "Find auth", "prompt": "Search tokens", "workspace": "worktree"},
	})
	if err != nil || callRes.IsError {
		t.Fatalf("Execute failed: %v, content=%s", err, callRes.Content.Text())
	}
	out, ok := callRes.StructuredContent.(api.InvokeAgentOutput)
	if !ok {
		t.Fatalf("unexpected structured output type: %T", callRes.StructuredContent)
	}
	if out.SessionID != "child-456" {
		t.Errorf("out.SessionID = %q, want %q", out.SessionID, "child-456")
	}
	if calledWith.Agent != "explore" {
		t.Errorf("calledWith.Agent = %q, want %q", calledWith.Agent, "explore")
	}
	if calledWith.Workspace != "worktree" {
		t.Errorf("calledWith.Workspace = %q, want %q", calledWith.Workspace, "worktree")
	}
}

func TestManageAgentsPrepareAndExecute(t *testing.T) {
	ctx := context.Background()
	dir, _, stdLib, sb, ws := setupBuiltinEnv(t, true)
	guard := shellguard.New(shellguard.Config{WorkspaceDir: dir})

	generalAgent := &warp.Agent{
		BaseResource: warp.BaseResource{
			Metadata: warp.Metadata{
				Name: "general",
			},
		},
	}
	mgr := &mockAgentManager{}
	cfg := builtin.Config{
		Std:          stdLib,
		Sandbox:      sb,
		Guard:        guard,
		Workspace:    ws,
		Agents:       mgr,
		Subagents:    []*warp.Agent{generalAgent},
		AllowedTools: subagentAllowedTools(ws),
	}

	builtinTools, err := builtin.New(cfg)
	if err != nil {
		t.Fatalf("builtin.New failed: %v", err)
	}

	container, err := tools.NewContainer(tools.Config{
		Tools:        builtinTools,
		Sandbox:      sb,
		Workspace:    ws,
		Logger:       log.Default(),
		AllowedTools: subagentAllowedTools(ws),
	})
	if err != nil {
		t.Fatalf("tools.NewContainer failed: %v", err)
	}

	// Unsupported action
	_, err = container.Prepare(ctx, &message.ToolCall{
		Name: builtin.ToolManageAgents,
		Args: map[string]any{"action": "destroy"},
	})
	if !errors.Is(err, std.ErrInvalidArgument) {
		t.Errorf("Prepare with unknown action error = %v, want %v", err, std.ErrInvalidArgument)
	}

	// Action status missing session_id
	_, err = container.Prepare(ctx, &message.ToolCall{
		Name: builtin.ToolManageAgents,
		Args: map[string]any{"action": "status"},
	})
	if !errors.Is(err, std.ErrInvalidArgument) {
		t.Errorf("Prepare status without session_id error = %v, want %v", err, std.ErrInvalidArgument)
	}

	// Action send missing message
	_, err = container.Prepare(ctx, &message.ToolCall{
		Name: builtin.ToolManageAgents,
		Args: map[string]any{"action": "send", "session_id": "child-1"},
	})
	if !errors.Is(err, std.ErrInvalidArgument) {
		t.Errorf("Prepare send without message error = %v, want %v", err, std.ErrInvalidArgument)
	}

	// Action kill missing session_id
	_, err = container.Prepare(ctx, &message.ToolCall{
		Name: builtin.ToolManageAgents,
		Args: map[string]any{"action": "kill"},
	})
	if !errors.Is(err, std.ErrInvalidArgument) {
		t.Errorf("Prepare kill without session_id error = %v, want %v", err, std.ErrInvalidArgument)
	}

	// Prepare list
	planList, err := container.Prepare(ctx, &message.ToolCall{
		Name: builtin.ToolManageAgents,
		Args: map[string]any{"action": "list"},
	})
	if err != nil {
		t.Fatalf("Prepare list failed: %v", err)
	}
	if !planList.ReadOnly {
		t.Errorf("planList.ReadOnly = %v, want true", planList.ReadOnly)
	}

	// Prepare send
	planSend, err := container.Prepare(ctx, &message.ToolCall{
		Name: builtin.ToolManageAgents,
		Args: map[string]any{"action": "send", "session_id": "child-1", "message": "Continue"},
	})
	if err != nil {
		t.Fatalf("Prepare send failed: %v", err)
	}
	if planSend.ReadOnly {
		t.Errorf("planSend.ReadOnly = %v, want false", planSend.ReadOnly)
	}

	// Execute list
	mgr.manageFunc = func(c context.Context, in api.ManageAgentsInput) (api.ManageAgentsOutput, error) {
		if in.Action == "list" {
			return api.ManageAgentsOutput{
				Agents: []api.ManageAgentsAgent{
					{
						SessionID: "child-1",
						Agent:     "general",
						Title:     "Build task",
						Status:    "running",
						WorkDir:   filepath.Join(dir, "sub"),
					},
				},
				Message: "Found 1 subagents",
			}, nil
		}
		return api.ManageAgentsOutput{Message: "ok"}, nil
	}

	callRes, err := container.Execute(ctx, &message.ToolCall{
		ID:   "call-list",
		Name: builtin.ToolManageAgents,
		Args: map[string]any{"action": "list"},
	})
	if err != nil || callRes.IsError {
		t.Fatalf("Execute list failed: %v, content=%s", err, callRes.Content.Text())
	}
	out, ok := callRes.StructuredContent.(api.ManageAgentsOutput)
	if !ok {
		t.Fatalf("unexpected structured output: %T", callRes.StructuredContent)
	}
	if len(out.Agents) != 1 || out.Agents[0].SessionID != "child-1" {
		t.Errorf("unexpected list output: %+v", out)
	}
}
