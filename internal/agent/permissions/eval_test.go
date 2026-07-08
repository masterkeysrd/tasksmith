package permissions

import (
	"context"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/preview"
)

func TestGenericPermissionHandler(t *testing.T) {
	// 1. Check strict mode requires auth
	h := &GenericPermissionHandler{ToolName: "web_search", IsDangerous: false, IsOpenWorld: true, IsReadOnly: false}
	res := h.Evaluate(context.Background(), ToolCallRequest{Args: map[string]any{"query": "hello"}}, ModeStrict, nil)
	if res.State != StateRequiresAuth {
		t.Errorf("strict mode should require auth, got %v", res.State)
	}

	// 2. Check default mode allows read-only local tools
	h = &GenericPermissionHandler{ToolName: "ls", IsDangerous: false, IsOpenWorld: false, IsReadOnly: true}
	res = h.Evaluate(context.Background(), ToolCallRequest{Args: map[string]any{"path": "."}}, ModeDefault, nil)
	if res.State != StateExplicitAllow {
		t.Errorf("default mode should auto-approve read-only local, got %v", res.State)
	}

	// 3. Check default mode prompts for write/dangerous tools
	h = &GenericPermissionHandler{ToolName: "write", IsDangerous: true, IsOpenWorld: false, IsReadOnly: false}
	res = h.Evaluate(context.Background(), ToolCallRequest{Args: map[string]any{"path": "foo"}}, ModeDefault, nil)
	if res.State != StateRequiresAuth {
		t.Errorf("default mode should prompt for dangerous/write tools, got %v", res.State)
	}

	// 4. Check auto mode allows non-dangerous tools
	h = &GenericPermissionHandler{ToolName: "ls", IsDangerous: false, IsOpenWorld: false, IsReadOnly: true}
	res = h.Evaluate(context.Background(), ToolCallRequest{Args: map[string]any{"path": "."}}, ModeAuto, nil)
	if res.State != StateAuto {
		t.Errorf("auto mode should approve non-dangerous tools, got %v", res.State)
	}

	// 5. Check auto mode prompts for dangerous/open-world tools
	h = &GenericPermissionHandler{ToolName: "bash", IsDangerous: true, IsOpenWorld: false, IsReadOnly: false}
	res = h.Evaluate(context.Background(), ToolCallRequest{Args: map[string]any{"command": "rm"}}, ModeAuto, nil)
	if res.State != StateRequiresAuth {
		t.Errorf("auto mode should prompt for dangerous tools, got %v", res.State)
	}

	// 6. Check saved grants override mode
	h = &GenericPermissionHandler{ToolName: "write", IsDangerous: true, IsOpenWorld: false, IsReadOnly: false}
	grants := []Permission{
		{Group: "edit_file", Target: "*", MatchMethod: "wildcard", Action: ActionAllow},
	}
	res = h.Evaluate(context.Background(), ToolCallRequest{Args: map[string]any{"path": "foo"}}, ModeDefault, grants)
	if res.State != StateExplicitAllow {
		t.Errorf("saved allow grant should override default prompt, got %v", res.State)
	}

	// 7. Check saved grants do NOT override ModeStrict
	h = &GenericPermissionHandler{ToolName: "write", IsDangerous: true, IsOpenWorld: false, IsReadOnly: false}
	res = h.Evaluate(context.Background(), ToolCallRequest{Args: map[string]any{"path": "foo"}}, ModeStrict, grants)
	if res.State != StateRequiresAuth {
		t.Errorf("saved allow grant should NOT override strict mode, got %v", res.State)
	}
}

type mockPermissionManager struct {
	mode   PermissionMode
	grants []Permission
	saved  []Permission
}

func (m *mockPermissionManager) GetGrants(ctx context.Context, group string) []Permission {
	return m.grants
}

func (m *mockPermissionManager) GetMode(ctx context.Context) PermissionMode {
	return m.mode
}

func (m *mockPermissionManager) SavePermission(ctx context.Context, scope PermissionScope, perm Permission) error {
	m.saved = append(m.saved, perm)
	return nil
}

func TestEvaluateToolCallMultiGrant(t *testing.T) {
	mockHandler := &mockMultiGrantHandler{}
	RegisterHandler("multi_mock", mockHandler)

	pm := &mockPermissionManager{
		mode: ModeDefault,
	}

	decision := &AuthorizationDecision{
		Approved: true,
		Scope:    ScopeSession,
		GrantDecisions: []GrantDecision{
			{RequestID: "cmd_1", SelectedTarget: "git status", Scope: ScopeSession},
			{RequestID: "cmd_2", SelectedTarget: "rm -rf tmp", Scope: ScopeSession},
		},
	}

	req := ToolCallRequest{
		ToolName: "multi_mock",
		Args:     map[string]any{"command": "git status && rm -rf tmp"},
	}

	state, _, _, _ := EvaluateToolCall(context.Background(), pm, req, decision)
	if state != StateExplicitAllow {
		t.Errorf("expected StateExplicitAllow, got %v", state)
	}

	if len(pm.saved) != 2 {
		t.Fatalf("expected 2 saved permissions, got %d", len(pm.saved))
	}

	if pm.saved[0].Target != "git status" || pm.saved[0].Action != ActionAllow {
		t.Errorf("expected first saved permission to be 'git status' allow, got %+v", pm.saved[0])
	}

	if pm.saved[1].Target != "rm -rf tmp" || pm.saved[1].Action != ActionAllow {
		t.Errorf("expected second saved permission to be 'rm -rf tmp' allow, got %+v", pm.saved[1])
	}
}

type mockMultiGrantHandler struct{}

func (h *mockMultiGrantHandler) GetPermissionGroup() string {
	return "mock_group"
}

func (h *mockMultiGrantHandler) Evaluate(ctx context.Context, req ToolCallRequest, mode PermissionMode, grants []Permission) EvaluationResult {
	return EvaluationResult{State: StateRequiresAuth}
}

func (h *mockMultiGrantHandler) GetOptions(req ToolCallRequest) []PermissionOption {
	return nil
}

func (h *mockMultiGrantHandler) GetPreview(ctx context.Context, req ToolCallRequest) (preview.ToolPreview, error) {
	return preview.DefaultTextPreview{Text: "preview"}, nil
}

func (h *mockMultiGrantHandler) GetGrantRequests(ctx context.Context, req ToolCallRequest, mode PermissionMode, grants []Permission) []PermissionGrantRequest {
	return []PermissionGrantRequest{
		{
			ID:          "cmd_1",
			Description: "Permission required for: git status",
			Options: []PermissionOption{
				{Target: "git status", MatchMethod: "exact", Action: ActionAllow},
			},
		},
		{
			ID:          "cmd_2",
			Description: "Permission required for: rm -rf tmp",
			Options: []PermissionOption{
				{Target: "rm -rf tmp", MatchMethod: "exact", Action: ActionAllow},
			},
		},
	}
}

func TestGetFallbackActionDescription(t *testing.T) {
	tests := []struct {
		toolName string
		args     map[string]any
		expected string
	}{
		{
			toolName: "ls",
			args:     map[string]any{"path": "/some/dir"},
			expected: "List directory: /some/dir",
		},
		{
			toolName: "list_dir",
			args:     map[string]any{"DirectoryPath": "/some/dir"},
			expected: "List directory: /some/dir",
		},
		{
			toolName: "grep",
			args:     map[string]any{"path": "/some/dir"},
			expected: "Search path: /some/dir",
		},
		{
			toolName: "grep_search",
			args:     map[string]any{"SearchPath": "/some/dir"},
			expected: "Search path: /some/dir",
		},
		{
			toolName: "glob",
			args:     map[string]any{"pattern": "*.go"},
			expected: "Glob pattern: *.go",
		},
		{
			toolName: "edit",
			args:     map[string]any{"path": "/some/file"},
			expected: "Modify file: /some/file",
		},
	}

	for _, tc := range tests {
		req := ToolCallRequest{
			ToolName: tc.toolName,
			Args:     tc.args,
		}
		got := getFallbackActionDescription(req)
		if got != tc.expected {
			t.Errorf("getFallbackActionDescription(%s, %v) = %q; expected %q", tc.toolName, tc.args, got, tc.expected)
		}
	}
}
