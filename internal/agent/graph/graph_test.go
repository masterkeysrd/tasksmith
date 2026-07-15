package graph_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	agentgraph "github.com/masterkeysrd/tasksmith/internal/agent/graph"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/core/preview"
)

type mockLLMModel struct {
	invokeFn func(ctx context.Context, messages []message.Message) (*message.Assistant, error)
}

func (m *mockLLMModel) Invoke(ctx context.Context, messages []message.Message) (*message.Assistant, error) {
	return m.invokeFn(ctx, messages)
}

func (m *mockLLMModel) BindTools(tools ...*tool.Tool) agentgraph.LLMModel {
	return m
}

func TestAgentGraph_Execution(t *testing.T) {
	// Tracks LLM calls
	callCount := 0

	// Mock LLM Model
	mockModel := &mockLLMModel{
		invokeFn: func(ctx context.Context, messages []message.Message) (*message.Assistant, error) {
			callCount++
			if callCount == 1 {
				return &message.Assistant{
					Content: message.Content{
						&message.TextBlock{Text: "TaskSmith is defined in GEMINI.md and README.md"},
					},
				}, nil
			}
			return nil, errors.New("unexpected LLM invocation")
		},
	}

	// Construct agent graph
	ag, err := agentgraph.New(context.Background(), agentgraph.Options{
		Model: mockModel,
	})
	if err != nil {
		t.Fatalf("failed to construct agent graph: %v", err)
	}
	g, err := ag.Build(nil)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	// Create initial state
	initialState := agentgraph.AgentState{
		Messages: message.MessageList{
			message.NewUserText("Where is TaskSmith defined?"),
		},
	}

	// We wrap the initial state in a graph.Update command to set the initial state
	initCmd := graph.Update[agentgraph.AgentState](func(s agentgraph.AgentState) agentgraph.AgentState {
		return initialState
	})

	// Execute graph
	snapshot, err := g.Execute(context.Background(), initCmd, nil)
	if err != nil {
		t.Fatalf("graph execution failed: %v", err)
	}

	// Assertions
	if !snapshot.IsDone() {
		t.Error("expected snapshot to be done")
	}
	lastMsg := snapshot.State.Messages[len(snapshot.State.Messages)-1]
	if lastMsg.GetContent().Text() != "TaskSmith is defined in GEMINI.md and README.md" {
		t.Errorf("unexpected output: %q", lastMsg.GetContent().Text())
	}
	if len(snapshot.State.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(snapshot.State.Messages))
	}
	// user, assistant (final answer)
	if snapshot.State.Messages[0].Role() != message.RoleUser {
		t.Errorf("msg 0 role: expected user, got %s", snapshot.State.Messages[0].Role())
	}
	if snapshot.State.Messages[1].Role() != message.RoleAssistant {
		t.Errorf("msg 1 role: expected assistant, got %s", snapshot.State.Messages[1].Role())
	}
}

func TestAgentGraph_SystemPrompt(t *testing.T) {
	promptVerified := false

	mockModel := &mockLLMModel{
		invokeFn: func(ctx context.Context, messages []message.Message) (*message.Assistant, error) {
			if len(messages) > 0 && messages[0].Role() == message.RoleSystem {
				if messages[0].GetContent().Text() == "You are a helpful assistant" {
					promptVerified = true
				}
			}
			return &message.Assistant{
				Content: message.Content{
					&message.TextBlock{Text: "Response"},
				},
			}, nil
		},
	}

	ag, err := agentgraph.New(context.Background(), agentgraph.Options{
		Model:        mockModel,
		SystemPrompt: "You are a helpful assistant",
	})
	if err != nil {
		t.Fatalf("failed to construct agent graph: %v", err)
	}
	g, err := ag.Build(nil)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	initialState := agentgraph.AgentState{
		Messages: message.MessageList{
			message.NewUserText("Hello"),
		},
	}

	initCmd := graph.Update[agentgraph.AgentState](func(s agentgraph.AgentState) agentgraph.AgentState {
		return initialState
	})

	_, err = g.Execute(context.Background(), initCmd, nil)
	if err != nil {
		t.Fatalf("graph execution failed: %v", err)
	}

	if !promptVerified {
		t.Error("expected system prompt to be injected as the first message")
	}
}

type mockPermissionManager struct {
	grants []permissions.Permission
	mode   permissions.PermissionMode
}

func (m *mockPermissionManager) GetGrants(ctx context.Context, groupName string) []permissions.Permission {
	var matched []permissions.Permission
	for _, g := range m.grants {
		if g.Group == groupName {
			matched = append(matched, g)
		}
	}
	return matched
}

func (m *mockPermissionManager) GetMode(ctx context.Context) permissions.PermissionMode {
	return m.mode
}

func (m *mockPermissionManager) SavePermission(ctx context.Context, scope permissions.PermissionScope, perm permissions.Permission) error {
	m.grants = append(m.grants, perm)
	return nil
}

func (m *mockPermissionManager) GetAllPermissions(ctx context.Context) (map[permissions.PermissionScope][]permissions.Permission, error) {
	res := make(map[permissions.PermissionScope][]permissions.Permission)
	res[permissions.ScopeSession] = m.grants
	return res, nil
}

func (m *mockPermissionManager) DeletePermission(ctx context.Context, scope permissions.PermissionScope, perm permissions.Permission) error {
	var remaining []permissions.Permission
	for _, p := range m.grants {
		if p.Group == perm.Group && p.Target == perm.Target && p.MatchMethod == perm.MatchMethod && p.Action == perm.Action && p.AllowedDirectory == perm.AllowedDirectory {
			continue
		}
		remaining = append(remaining, p)
	}
	m.grants = remaining
	return nil
}

type dummyToolHandler struct {
	evalRes permissions.EvaluationResult
}

func (h *dummyToolHandler) GetPermissionGroup() string {
	return "dummy_group"
}

func (h *dummyToolHandler) Evaluate(ctx context.Context, req permissions.ToolCallRequest, mode permissions.PermissionMode, grants []permissions.Permission) permissions.EvaluationResult {
	// If there is an explicit allow in grants, return allow
	for _, g := range grants {
		if g.Action == permissions.ActionAllow {
			return permissions.EvaluationResult{State: permissions.StateExplicitAllow}
		}
	}
	return h.evalRes
}

func (h *dummyToolHandler) GetOptions(req permissions.ToolCallRequest) []permissions.PermissionOption {
	return []permissions.PermissionOption{{Label: "Allow todos", Target: "*", MatchMethod: "wildcard", Action: permissions.ActionAllow}}
}

func (h *dummyToolHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (preview.ToolPreview, error) {
	return preview.DefaultTextPreview{Text: "Dummy Preview"}, nil
}

type mockCheckpointer struct {
	checkpoints map[string][]graph.Checkpoint
	mu          sync.Mutex
}

func newMockCheckpointer() *mockCheckpointer {
	return &mockCheckpointer{
		checkpoints: make(map[string][]graph.Checkpoint),
	}
}

func (cp *mockCheckpointer) Load(ctx context.Context, loc graph.Location) (*graph.Checkpoint, error) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	list, ok := cp.checkpoints[loc.ThreadID]
	if !ok || len(list) == 0 {
		return nil, nil
	}

	if loc.CheckpointID == "" {
		latest := list[len(list)-1]
		return &latest, nil
	}

	for _, c := range list {
		if c.Location.CheckpointID == loc.CheckpointID {
			return &c, nil
		}
	}

	return nil, nil
}

func (cp *mockCheckpointer) Record(ctx context.Context, checkpoint graph.Checkpoint) error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	tid := checkpoint.Location.ThreadID
	cp.checkpoints[tid] = append(cp.checkpoints[tid], checkpoint)
	return nil
}

func TestAgentGraph_PermissionsInterception(t *testing.T) {
	// Register a dummy handler for the "todos" tool to trigger interception
	dh := &dummyToolHandler{
		evalRes: permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{"Todos requires authorization"},
		},
	}
	permissions.RegisterHandler("todos", dh)

	// Mock LLM model returning a tool call
	mockModel := &mockLLMModel{
		invokeFn: func(ctx context.Context, messages []message.Message) (*message.Assistant, error) {
			hasToolCall := false
			for _, m := range messages {
				if m.Role() == message.RoleAssistant {
					hasToolCall = true
					break
				}
			}
			if !hasToolCall {
				return &message.Assistant{
					Content: message.Content{
						&message.ToolCall{
							ID:   "call_todos_123",
							Name: "todos",
							Args: map[string]any{
								"todos": []any{
									map[string]any{
										"description": "test task",
										"status":      "pending",
									},
								},
							},
						},
					},
				}, nil
			}
			// When resumed and completed, return final text
			return &message.Assistant{
				Content: message.Content{
					&message.TextBlock{Text: "Final answer after todos"},
				},
			}, nil
		},
	}

	pm := &mockPermissionManager{mode: permissions.ModeDefault}
	ag, err := agentgraph.New(context.Background(), agentgraph.Options{
		Model:             mockModel,
		PermissionManager: pm,
	})
	if err != nil {
		t.Fatalf("failed to construct agent graph: %v", err)
	}
	cp := newMockCheckpointer()
	g, err := ag.Build(cp)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	initialState := agentgraph.AgentState{
		Messages: message.MessageList{
			message.NewUserText("List todos"),
		},
	}

	initCmd := graph.Update[agentgraph.AgentState](func(s agentgraph.AgentState) agentgraph.AgentState {
		return initialState
	})

	// Run graph - should halt on todos tool call
	snapshot, err := g.Execute(context.Background(), initCmd, nil)
	if err != nil {
		t.Fatalf("graph execution failed: %v", err)
	}

	if snapshot.IsDone() {
		t.Error("expected execution to be halted, but it completed")
	}

	if len(snapshot.State.PendingAuthorizations) != 1 {
		t.Fatalf("expected 1 pending authorization, got %d", len(snapshot.State.PendingAuthorizations))
	}

	req := snapshot.State.PendingAuthorizations[0]
	if req.ToolCallID != "call_todos_123" || req.ToolName != "todos" {
		t.Errorf("unexpected pending authorization: %+v", req)
	}

	// Resume execution with Approved = true
	resumeCmd := graph.Update[agentgraph.AgentState](func(state agentgraph.AgentState) agentgraph.AgentState {
		state.Decisions = []permissions.AuthorizationDecision{
			{
				ToolCallID: "call_todos_123",
				Approved:   true,
			},
		}
		return state
	})

	finalSnapshot, err := g.Execute(context.Background(), resumeCmd, &snapshot.Location)
	if err != nil {
		t.Fatalf("resumed graph execution failed: %v", err)
	}

	if !finalSnapshot.IsDone() {
		t.Error("expected resumed execution to be completed")
	}

	// Verify that the tool was executed and result appended
	foundResult := false
	for _, msg := range finalSnapshot.State.Messages {
		if tm, ok := msg.(*message.Tool); ok && tm.ToolCallID == "call_todos_123" {
			foundResult = true
			break
		}
	}
	if !foundResult {
		t.Error("expected tool result for call_todos_123 in final messages list")
	}

	lastMsg := finalSnapshot.State.Messages[len(finalSnapshot.State.Messages)-1]
	if lastMsg.GetContent().Text() != "Final answer after todos" {
		t.Errorf("expected final answer, got %q", lastMsg.GetContent().Text())
	}
}

func TestAgentGraph_PermissionsInterceptionDenyWithFeedback(t *testing.T) {
	// Register a dummy handler for the "todos" tool to trigger interception
	dh := &dummyToolHandler{
		evalRes: permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{"Todos requires authorization"},
		},
	}
	permissions.RegisterHandler("todos", dh)

	// Mock LLM model returning a tool call
	mockModel := &mockLLMModel{
		invokeFn: func(ctx context.Context, messages []message.Message) (*message.Assistant, error) {
			hasToolCall := false
			for _, m := range messages {
				if m.Role() == message.RoleAssistant {
					hasToolCall = true
					break
				}
			}
			if !hasToolCall {
				return &message.Assistant{
					Content: message.Content{
						&message.ToolCall{
							ID:   "call_todos_456",
							Name: "todos",
							Args: map[string]any{
								"todos": []any{
									map[string]any{
										"description": "test task",
										"status":      "pending",
									},
								},
							},
						},
					},
				}, nil
			}
			// When resumed and completed, return final text
			return &message.Assistant{
				Content: message.Content{
					&message.TextBlock{Text: "Final answer after denial feedback"},
				},
			}, nil
		},
	}

	pm := &mockPermissionManager{mode: permissions.ModeDefault}
	ag, err := agentgraph.New(context.Background(), agentgraph.Options{
		Model:             mockModel,
		PermissionManager: pm,
	})
	if err != nil {
		t.Fatalf("failed to construct agent graph: %v", err)
	}
	cp := newMockCheckpointer()
	g, err := ag.Build(cp)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	initialState := agentgraph.AgentState{
		Messages: message.MessageList{
			message.NewUserText("List todos"),
		},
	}

	initCmd := graph.Update[agentgraph.AgentState](func(s agentgraph.AgentState) agentgraph.AgentState {
		return initialState
	})

	// Run graph - should halt on todos tool call
	snapshot, err := g.Execute(context.Background(), initCmd, nil)
	if err != nil {
		t.Fatalf("graph execution failed: %v", err)
	}

	if snapshot.IsDone() {
		t.Error("expected execution to be halted, but it completed")
	}

	// Resume execution with Approved = false and a Reason
	resumeCmd := graph.Update[agentgraph.AgentState](func(state agentgraph.AgentState) agentgraph.AgentState {
		state.Decisions = []permissions.AuthorizationDecision{
			{
				ToolCallID: "call_todos_456",
				Approved:   false,
				Reason:     "unsafe execution directory",
			},
		}
		return state
	})

	finalSnapshot, err := g.Execute(context.Background(), resumeCmd, &snapshot.Location)
	if err != nil {
		t.Fatalf("resumed graph execution failed: %v", err)
	}

	if !finalSnapshot.IsDone() {
		t.Error("expected resumed execution to be completed")
	}

	// Verify that the tool was denied and the reason was recorded
	var toolMsg *message.Tool
	for _, msg := range finalSnapshot.State.Messages {
		if tm, ok := msg.(*message.Tool); ok && tm.ToolCallID == "call_todos_456" {
			toolMsg = tm
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("expected tool message result for call_todos_456 in final messages list")
	}

	if !toolMsg.IsError {
		t.Error("expected denied tool message to have IsError = true")
	}

	expectedText := `Authorization denied by user for tool "todos": unsafe execution directory`
	if toolMsg.GetContent().Text() != expectedText {
		t.Errorf("expected text %q, got %q", expectedText, toolMsg.GetContent().Text())
	}

	meta := toolMsg.GetMetadata()
	if meta == nil {
		t.Fatal("expected metadata on denied tool message")
	}

	denyReason, ok := meta["deny_reason"].(string)
	if !ok || denyReason != "unsafe execution directory" {
		t.Errorf("expected metadata deny_reason 'unsafe execution directory', got %v", meta["deny_reason"])
	}
}

func TestAgentGraph_InvalidArgumentsValidation(t *testing.T) {
	// Register a dummy handler for the "todos" tool
	dh := &dummyToolHandler{
		evalRes: permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{"Todos requires authorization"},
		},
	}
	permissions.RegisterHandler("todos", dh)

	// Mock LLM model returning an invalid tool call (missing required 'todos' field)
	mockModel := &mockLLMModel{
		invokeFn: func(ctx context.Context, messages []message.Message) (*message.Assistant, error) {
			return &message.Assistant{
				Content: message.Content{
					&message.ToolCall{
						ID:   "call_todos_invalid",
						Name: "todos",
						Args: map[string]any{"invalid_field": 123},
					},
				},
			}, nil
		},
	}

	pm := &mockPermissionManager{mode: permissions.ModeDefault}
	ag, err := agentgraph.New(context.Background(), agentgraph.Options{
		Model:             mockModel,
		PermissionManager: pm,
	})
	if err != nil {
		t.Fatalf("failed to construct agent graph: %v", err)
	}
	cp := newMockCheckpointer()
	g, err := ag.Build(cp)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	initialState := agentgraph.AgentState{
		Messages: message.MessageList{
			message.NewUserText("List todos"),
		},
	}

	initCmd := graph.Update[agentgraph.AgentState](func(s agentgraph.AgentState) agentgraph.AgentState {
		return initialState
	})

	// Run graph - should not halt/intercept because validation fails before permission check.
	// It should instead complete and append a validation error message.
	snapshot, err := g.Execute(context.Background(), initCmd, nil)
	if err != nil {
		t.Fatalf("graph execution failed: %v", err)
	}

	if !snapshot.IsDone() {
		t.Error("expected execution to be completed (no halt for validation error), but it halted")
	}

	if len(snapshot.State.PendingAuthorizations) > 0 {
		t.Errorf("expected 0 pending authorizations, got %d", len(snapshot.State.PendingAuthorizations))
	}

	var toolMsg *message.Tool
	for _, msg := range snapshot.State.Messages {
		if tm, ok := msg.(*message.Tool); ok && tm.ToolCallID == "call_todos_invalid" {
			toolMsg = tm
			break
		}
	}

	if toolMsg == nil {
		t.Fatal("expected tool message result for call_todos_invalid in final messages list")
	}

	if !toolMsg.IsError {
		t.Error("expected invalid arguments tool message to have IsError = true")
	}

	if !strings.Contains(toolMsg.GetContent().Text(), "invalid arguments for tool") {
		t.Errorf("expected validation error text, got %q", toolMsg.GetContent().Text())
	}
}
