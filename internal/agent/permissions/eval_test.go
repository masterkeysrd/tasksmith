package permissions

import (
	"context"
	"testing"
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
	if res.State != StateExplicitAllow {
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
}
