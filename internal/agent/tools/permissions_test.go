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
	if prev == nil {
		t.Error("preview should not be nil")
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

	// 3. Evaluate safe destination, ModeAuto -> StateAuto
	res = h.Evaluate(ctx, permissions.ToolCallRequest{ToolName: "web_fetch", Args: map[string]any{"url": "https://example.com/foo", "destination": "safe/file.zip"}}, permissions.ModeAuto, nil)
	if res.State != permissions.StateAuto {
		t.Errorf("expected StateAuto in auto mode, got %v", res.State)
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

func TestBashPermissionHandler(t *testing.T) {
	h := &BashPermissionHandler{}
	ctx := permissions.ContextWithWorkspaceCWD(context.Background(), "/test/workspace")

	// 1. Running a normal command with no grants, ModeDefault -> should generate a grant request
	req := permissions.ToolCallRequest{
		ToolName: "bash",
		Args:     map[string]any{"command": "git status"},
	}
	reqs := h.GetGrantRequests(ctx, req, permissions.ModeDefault, nil)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 grant request, got %d", len(reqs))
	}
	if reqs[0].Options[0].Target != "git status" {
		t.Errorf("expected target 'git status', got %q", reqs[0].Options[0].Target)
	}

	// 2. git status with matching grant -> should NOT generate any request (auto-approved via grant)
	grants := []permissions.Permission{
		{Group: "command", Target: "git *", MatchMethod: "wildcard", Action: permissions.ActionAllow},
	}
	reqs = h.GetGrantRequests(ctx, req, permissions.ModeDefault, grants)
	if len(reqs) != 0 {
		t.Errorf("expected 0 grant requests for git status with git * grant, got %d", len(reqs))
	}

	// 3. Running rm -rf in ModeAuto (no grants) -> should be intercepted because it is Destructive
	reqRM := permissions.ToolCallRequest{
		ToolName: "bash",
		Args:     map[string]any{"command": "rm -rf tmp"},
	}
	reqsRM := h.GetGrantRequests(ctx, reqRM, permissions.ModeAuto, nil)
	if len(reqsRM) != 1 {
		t.Fatalf("expected rm -rf in ModeAuto to be intercepted, got %d requests", len(reqsRM))
	}
	if reqsRM[0].Options[0].Target != "rm -rf tmp" {
		t.Errorf("expected target 'rm -rf tmp', got %q", reqsRM[0].Options[0].Target)
	}

	// 4. Running a chained command like echo hi && sudo rm -rf / -> should identify the destructive wrapper and request rm
	reqChain := permissions.ToolCallRequest{
		ToolName: "bash",
		Args:     map[string]any{"command": "echo hi && sudo rm -rf /"},
	}
	grantsEcho := []permissions.Permission{
		{Group: "command", Target: "echo hi", MatchMethod: "exact", Action: permissions.ActionAllow},
	}
	reqsChain := h.GetGrantRequests(ctx, reqChain, permissions.ModeDefault, grantsEcho)
	if len(reqsChain) != 1 {
		t.Fatalf("expected 1 request for chained command, got %d", len(reqsChain))
	}
	if reqsChain[0].Description != "rm -rf /" {
		t.Errorf("expected request description for rm -rf /, got %q", reqsChain[0].Description)
	}

	// 5. Chained command with cd /test/workspace && go test ./... -> should ignore cd and only request go test ./...
	reqCd := permissions.ToolCallRequest{
		ToolName: "bash",
		Args:     map[string]any{"command": "cd /test/workspace && go test ./..."},
	}
	reqsCd := h.GetGrantRequests(ctx, reqCd, permissions.ModeDefault, nil)
	if len(reqsCd) != 1 {
		t.Fatalf("expected 1 request for command chain with cd, got %d", len(reqsCd))
	}
	if reqsCd[0].Options[0].Target != "go test ./..." {
		t.Errorf("expected target 'go test ./...', got %q", reqsCd[0].Options[0].Target)
	}

	// 6. cd /etc -> should NOT be ignored because it is outside the workspace
	reqUnsafeCd := permissions.ToolCallRequest{
		ToolName: "bash",
		Args:     map[string]any{"command": "cd /etc"},
	}
	reqsUnsafeCd := h.GetGrantRequests(ctx, reqUnsafeCd, permissions.ModeDefault, nil)
	if len(reqsUnsafeCd) != 1 {
		t.Fatalf("expected 1 request for cd outside workspace, got %d", len(reqsUnsafeCd))
	}
	if reqsUnsafeCd[0].Options[0].Target != "cd /etc" {
		t.Errorf("expected target 'cd /etc', got %q", reqsUnsafeCd[0].Options[0].Target)
	}

	// 7. cd .. -> should NOT be ignored because it goes outside /test/workspace
	reqCdParent := permissions.ToolCallRequest{
		ToolName: "bash",
		Args:     map[string]any{"command": "cd .."},
	}
	reqsCdParent := h.GetGrantRequests(ctx, reqCdParent, permissions.ModeDefault, nil)
	if len(reqsCdParent) != 1 {
		t.Fatalf("expected 1 request for cd .., got %d", len(reqsCdParent))
	}
	if reqsCdParent[0].Options[0].Target != "cd .." {
		t.Errorf("expected target 'cd ..', got %q", reqsCdParent[0].Options[0].Target)
	}

	// 8. Pipeline test with same executables and arg count but different arguments (e.g. ollama ps && ollama ls)
	// Both should generate individual requests, and if one has a grant, only the other is requested.
	reqPipeline := permissions.ToolCallRequest{
		ToolName: "bash",
		Args:     map[string]any{"command": "cd /test/workspace && ollama ps && ollama ls"},
	}
	reqsPipeline := h.GetGrantRequests(ctx, reqPipeline, permissions.ModeDefault, nil)
	if len(reqsPipeline) != 2 {
		t.Fatalf("expected 2 requests for pipeline, got %d", len(reqsPipeline))
	}
	if reqsPipeline[0].Options[0].Target != "ollama ps" {
		t.Errorf("expected first target 'ollama ps', got %q", reqsPipeline[0].Options[0].Target)
	}
	if reqsPipeline[1].Options[0].Target != "ollama ls" {
		t.Errorf("expected second target 'ollama ls', got %q", reqsPipeline[1].Options[0].Target)
	}

	// Chained with one already granted
	grantsPipeline := []permissions.Permission{
		{Group: "command", Target: "ollama ps", MatchMethod: "exact", Action: permissions.ActionAllow},
	}
	reqsPipelineWithGrant := h.GetGrantRequests(ctx, reqPipeline, permissions.ModeDefault, grantsPipeline)
	if len(reqsPipelineWithGrant) != 1 {
		t.Fatalf("expected 1 request when one command is granted, got %d", len(reqsPipelineWithGrant))
	}
	if reqsPipelineWithGrant[0].Options[0].Target != "ollama ls" {
		t.Errorf("expected remaining request target 'ollama ls', got %q", reqsPipelineWithGrant[0].Options[0].Target)
	}
}
