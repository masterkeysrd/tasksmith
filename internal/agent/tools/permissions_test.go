package tools

import (
	"context"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
)

func TestWebFetchPermissionHandler(t *testing.T) {
	h := &WebFetchPermissionHandler{}
	ctx := context.Background()

	// 1. Evaluate with no grants, ModeDefault -> StateRequiresAuth
	res := h.Evaluate(ctx, permissions.ToolCallRequest{ToolName: "web_fetch", Args: map[string]any{"url": "https://example.com/foo"}}, permissions.ModeDefault, nil)
	if res.State != permissions.StateRequiresAuth {
		t.Errorf("expected StateRequiresAuth, got %v", res.State)
	}

	// 2. Evaluate with matching allow grant -> StateExplicitAllow
	grants := []permissions.Permission{
		{Group: "web_fetch", Target: "https://example.com/foo", MatchMethod: "exact", Action: permissions.ActionAllow},
	}
	res = h.Evaluate(ctx, permissions.ToolCallRequest{ToolName: "web_fetch", Args: map[string]any{"url": "https://example.com/foo"}}, permissions.ModeDefault, grants)
	if res.State != permissions.StateExplicitAllow {
		t.Errorf("expected StateExplicitAllow, got %v", res.State)
	}

	// 3. Evaluate with matching deny grant -> StateExplicitDeny
	grants = []permissions.Permission{
		{Group: "web_fetch", Target: "https://example.com/foo", MatchMethod: "exact", Action: permissions.ActionDeny},
	}
	res = h.Evaluate(ctx, permissions.ToolCallRequest{ToolName: "web_fetch", Args: map[string]any{"url": "https://example.com/foo"}}, permissions.ModeDefault, grants)
	if res.State != permissions.StateExplicitDeny {
		t.Errorf("expected StateExplicitDeny, got %v", res.State)
	}

	// 4. Options check
	opts := h.GetOptions(permissions.ToolCallRequest{ToolName: "web_fetch", Args: map[string]any{"url": "https://example.com/foo"}})
	if len(opts) < 2 {
		t.Errorf("expected at least 2 options, got %d", len(opts))
	}
	if opts[0].Target != "https://example.com/foo" {
		t.Errorf("expected target %q, got %q", "https://example.com/foo", opts[0].Target)
	}

	// 5. Preview check
	prev, err := h.GetPreview(ctx, permissions.ToolCallRequest{ToolName: "web_fetch", Args: map[string]any{"url": "https://example.com/foo"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prev == "" {
		t.Error("preview should not be empty")
	}
}

func TestDownloadPermissionHandler(t *testing.T) {
	h := &DownloadPermissionHandler{}
	ctx := permissions.ContextWithWorkspaceCWD(context.Background(), "/test/workspace")

	// 1. Evaluate unsafe destination (outside workspace) -> StateRequiresAuth with warning
	res := h.Evaluate(ctx, permissions.ToolCallRequest{ToolName: "web_fetch", Args: map[string]any{"url": "https://example.com/foo", "destination": "/outside/file.zip"}}, permissions.ModeDefault, nil)
	if res.State != permissions.StateRequiresAuth {
		t.Errorf("expected StateRequiresAuth, got %v", res.State)
	}
	foundWarning := false
	for _, hint := range res.Hints {
		if len(hint) > 0 {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected safety warning hint for outside path")
	}

	// 2. Evaluate safe destination, ModeDefault -> StateRequiresAuth (no warning, needs auth)
	res = h.Evaluate(ctx, permissions.ToolCallRequest{ToolName: "web_fetch", Args: map[string]any{"url": "https://example.com/foo", "destination": "safe/file.zip"}}, permissions.ModeDefault, nil)
	if res.State != permissions.StateRequiresAuth {
		t.Errorf("expected StateRequiresAuth, got %v", res.State)
	}

	// 3. Evaluate safe destination, ModeAuto -> StateExplicitAllow
	res = h.Evaluate(ctx, permissions.ToolCallRequest{ToolName: "web_fetch", Args: map[string]any{"url": "https://example.com/foo", "destination": "safe/file.zip"}}, permissions.ModeAuto, nil)
	if res.State != permissions.StateExplicitAllow {
		t.Errorf("expected StateExplicitAllow in auto mode, got %v", res.State)
	}

	// 4. Options check
	opts := h.GetOptions(permissions.ToolCallRequest{ToolName: "web_fetch", Args: map[string]any{"url": "https://example.com/foo", "destination": "safe/file.zip"}})
	if len(opts) < 3 {
		t.Errorf("expected at least 3 options, got %d", len(opts))
	}
}

func TestWebSearchPermissionHandler(t *testing.T) {
	h := &WebSearchPermissionHandler{}
	ctx := context.Background()

	// 1. Evaluate query -> StateRequiresAuth
	res := h.Evaluate(ctx, permissions.ToolCallRequest{ToolName: "web_fetch", Args: map[string]any{"query": "golang framework"}}, permissions.ModeDefault, nil)
	if res.State != permissions.StateRequiresAuth {
		t.Errorf("expected StateRequiresAuth, got %v", res.State)
	}

	// 2. Evaluate with matching grant -> StateExplicitAllow
	grants := []permissions.Permission{
		{Group: "web_search", Target: "golang framework", MatchMethod: "exact", Action: permissions.ActionAllow},
	}
	res = h.Evaluate(ctx, permissions.ToolCallRequest{ToolName: "web_fetch", Args: map[string]any{"query": "golang framework"}}, permissions.ModeDefault, grants)
	if res.State != permissions.StateExplicitAllow {
		t.Errorf("expected StateExplicitAllow, got %v", res.State)
	}

	// 3. Options check
	opts := h.GetOptions(permissions.ToolCallRequest{ToolName: "web_fetch", Args: map[string]any{"query": "golang framework"}})
	if len(opts) != 2 {
		t.Errorf("expected exactly 2 options, got %d", len(opts))
	}
}

func TestMatchHelpers(t *testing.T) {
	// 1. Test matchFile
	fileTests := []struct {
		grantTarget string
		matchMethod string
		targetValue string
		expected    bool
	}{
		{"*", "wildcard", "foo", true},
		{"foo", "exact", "foo", true},
		{"foo", "exact", "bar", false},
		{"/a/b", "prefix", "/a/b/c", true},
		{"/a/b", "prefix", "/a/c", false},
		{"/a/b", "path", "/a/b/c", true},
		{"/a/b", "path", "/a/bc", false},
		{"*.txt", "wildcard", "foo.txt", true},
		{"*.txt", "wildcard", "foo.log", false},
	}

	for _, tc := range fileTests {
		res := matchFile(tc.grantTarget, tc.matchMethod, tc.targetValue)
		if res != tc.expected {
			t.Errorf("matchFile(%q, %q, %q) = %v; expected %v", tc.grantTarget, tc.matchMethod, tc.targetValue, res, tc.expected)
		}
	}

	// 2. Test matchGeneric
	genericTests := []struct {
		grantTarget string
		matchMethod string
		targetValue string
		expected    bool
	}{
		{"*", "wildcard", "https://example.com/foo", true},
		{"https://example.com/foo", "exact", "https://example.com/foo", true},
		{"https://example.com/foo", "exact", "https://example.com/bar", false},
		{"https://example.com", "prefix", "https://example.com/foo", true},
		{"https://example.com", "prefix", "https://google.com", false},
		{"https://example.com/*", "wildcard", "https://example.com/foo", true},
	}

	for _, tc := range genericTests {
		res := matchGeneric(tc.grantTarget, tc.matchMethod, tc.targetValue)
		if res != tc.expected {
			t.Errorf("matchGeneric(%q, %q, %q) = %v; expected %v", tc.grantTarget, tc.matchMethod, tc.targetValue, res, tc.expected)
		}
	}
}
