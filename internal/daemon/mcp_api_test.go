package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

const mcpManifestWithPolicies = `---
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: github
  displayName: GitHub
  description: GitHub MCP server
spec:
  type: stdio
  command: ["npx", "-y", "@modelcontextprotocol/server-github"]
  policies:
    tools:
      exclude: ["delete_*"]
    prompts:
      exclude: ["danger"]
    resources:
      exclude: ["file://.env*"]
---
`

const mcpRemoteManifest = `---
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: linear
  description: Linear MCP server
spec:
  type: http
  endpoint: https://mcp.linear.app/sse
  auth:
    type: oauth2
---
`

// writeMCPScripts writes MCP manifests into the workspace's .agents directory.
func writeMCPScripts(t *testing.T, wsDir string, manifests map[string]string) {
	t.Helper()
	mcpDir := filepath.Join(wsDir, ".agents")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range manifests {
		if err := os.WriteFile(filepath.Join(mcpDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// fakeMCPService records the calls reaching the MCP runtime so tests can assert
// that policy enforcement happens before any server is touched.
type fakeMCPService struct {
	list      []api.MCPServer
	restarted api.MCPServer
	auth      api.MCPAuthResponse
	prompt    api.MCPPromptResponse
	resource  api.MCPResourceResponse
	allowed   api.MCPServer

	restartName   string
	authName      string
	promptServer  string
	promptName    string
	promptArgs    map[string]string
	promptAllowed bool
	resourceSrv   string
	resourceReq   api.MCPResourceRequest
	allowName     string
	allowReq      api.MCPAllowPolicyRequest
	callbackName  string
	callbackURL   string
	callbackErr   error
}

func (f *fakeMCPService) ListMCPs(context.Context) []api.MCPServer { return f.list }

func (f *fakeMCPService) RestartMCP(_ context.Context, name string) (api.MCPServer, error) {
	f.restartName = name
	return f.restarted, nil
}

func (f *fakeMCPService) AuthenticateMCP(_ context.Context, name string) (api.MCPAuthResponse, error) {
	f.authName = name
	return f.auth, nil
}

func (f *fakeMCPService) SubmitMCPAuthCallback(_ context.Context, name, callbackURL string) error {
	f.callbackName = name
	f.callbackURL = callbackURL
	return f.callbackErr
}

func (f *fakeMCPService) GetMCPPrompt(_ context.Context, name, prompt string, args map[string]string, allowProhibited bool) (api.MCPPromptResponse, error) {
	f.promptServer = name
	f.promptName = prompt
	f.promptArgs = args
	f.promptAllowed = allowProhibited
	return f.prompt, nil
}

func (f *fakeMCPService) ReadMCPResource(_ context.Context, name string, req api.MCPResourceRequest) (api.MCPResourceResponse, error) {
	f.resourceSrv = name
	f.resourceReq = req
	return f.resource, nil
}

func (f *fakeMCPService) AllowPolicyItem(_ context.Context, name string, req api.MCPAllowPolicyRequest) (api.MCPServer, error) {
	f.allowName = name
	f.allowReq = req
	return f.allowed, nil
}

// setupMCPDaemon opens a workspace holding the given manifests and returns the
// daemon plus its runtime so tests can attach a fake MCP runtime.
func setupMCPDaemon(t *testing.T, manifests map[string]string) (*Daemon, *workspaceRuntime, string) {
	t.Helper()
	srv, _, wsDir := setupDaemonTest(t, nil)
	writeMCPScripts(t, wsDir, manifests)

	rt, err := srv.workspace(context.Background(), wsDir)
	if err != nil {
		t.Fatalf("daemon.workspace: %v", err)
	}
	if err := rt.Reload(context.Background()); err != nil {
		t.Fatalf("runtime reload: %v", err)
	}
	return srv, rt, wsDir
}

// detachMCPRuntime disconnects the workspace's MCP runtime so a test can exercise
// the manifest-only paths used when a workspace has no MCP manager attached.
func detachMCPRuntime(t *testing.T, rt *workspaceRuntime) {
	t.Helper()
	owner := rt.SessionRuntime()
	if owner == nil {
		t.Fatal("workspace runtime has no session owner")
	}
	owner.RootWorkspace().SetMCPService(nil)
	if owner.mcpManager != nil {
		if err := owner.mcpManager.Close(); err != nil {
			t.Fatalf("close MCP manager: %v", err)
		}
	}
}

// assertAPIError asserts that err is an *api.APIError with the expected HTTP
// status and machine-readable code.
func assertAPIError(t *testing.T, label string, err error, wantStatus int, wantCode string) {
	t.Helper()
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("%s: expected *api.APIError, got %v", label, err)
	}
	if apiErr.StatusCode != wantStatus || apiErr.Code != wantCode {
		t.Errorf("%s = (%d, %q), want (%d, %q)", label, apiErr.StatusCode, apiErr.Code, wantStatus, wantCode)
	}
}

func TestListMCPsFromManifestsWithoutRuntime(t *testing.T) {
	ctx := context.Background()
	srv, rt, wsDir := setupMCPDaemon(t, map[string]string{
		"github.mcp.yaml": mcpManifestWithPolicies,
		"linear.mcp.yaml": mcpRemoteManifest,
	})
	detachMCPRuntime(t, rt)

	mcps, err := srv.ListMCPs(ctx, wsDir)
	if err != nil {
		t.Fatalf("ListMCPs: %v", err)
	}
	if len(mcps) != 2 {
		t.Fatalf("expected 2 MCP servers, got %+v", mcps)
	}
	if mcps[0].Name != "github" || mcps[1].Name != "linear" {
		t.Fatalf("servers must be sorted by name, got %q then %q", mcps[0].Name, mcps[1].Name)
	}

	github := mcps[0]
	if github.Transport != "stdio" || github.DisplayName != "GitHub" {
		t.Errorf("github presentation = %+v", github)
	}
	if strings.Join(github.Command, " ") != "npx -y @modelcontextprotocol/server-github" {
		t.Errorf("github command = %v", github.Command)
	}
	if github.Status != api.MCPStatusDisconnected {
		t.Errorf("github status = %q, want %q", github.Status, api.MCPStatusDisconnected)
	}
	if github.Scope != "Project" {
		t.Errorf("github scope = %q, want Project", github.Scope)
	}
	if filepath.IsAbs(github.Path) || !strings.HasSuffix(github.Path, "github.mcp.yaml") {
		t.Errorf("github path must be workspace-relative, got %q", github.Path)
	}
	if len(github.Tools) != 0 || len(github.Prompts) != 0 || len(github.Resources) != 0 {
		t.Errorf("expected no discoveries without a runtime, got %+v", github)
	}

	linear := mcps[1]
	if linear.Transport != "http" || linear.Endpoint != "https://mcp.linear.app/sse" {
		t.Errorf("linear transport/endpoint = %+v", linear)
	}
	if linear.AuthType != "oauth2" {
		t.Errorf("linear authType = %q, want oauth2", linear.AuthType)
	}
	if len(linear.Command) != 0 {
		t.Errorf("expected no command for an HTTP server, got %v", linear.Command)
	}
}

func TestListMCPsOverlaysRuntimeState(t *testing.T) {
	ctx := context.Background()
	srv, rt, wsDir := setupMCPDaemon(t, map[string]string{"github.mcp.yaml": mcpManifestWithPolicies})

	fake := &fakeMCPService{list: []api.MCPServer{
		{
			Name:      "github",
			Transport: "stdio",
			Status:    api.MCPStatusConnected,
			Path:      filepath.Join(wsDir, ".agents", "github.mcp.yaml"),
			Tools: []api.MCPToolSummary{
				{Name: "github__create_issue", RawName: "create_issue", Allowed: true},
				{Name: "github__delete_repo", RawName: "delete_repo"},
			},
		},
		{Name: "zextra", Transport: "stdio", Status: api.MCPStatusError, Error: "handshake failed"},
	}}
	rt.Workspace().SetMCPService(fake)

	mcps, err := srv.ListMCPs(ctx, wsDir)
	if err != nil {
		t.Fatalf("ListMCPs: %v", err)
	}
	if len(mcps) != 2 || mcps[0].Name != "github" || mcps[1].Name != "zextra" {
		t.Fatalf("expected runtime listing sorted by name, got %+v", mcps)
	}
	if mcps[0].Status != api.MCPStatusConnected {
		t.Errorf("github status = %q, want %q", mcps[0].Status, api.MCPStatusConnected)
	}
	if filepath.IsAbs(mcps[0].Path) {
		t.Errorf("runtime path must be portable, got %q", mcps[0].Path)
	}
	if len(mcps[0].Tools) != 2 || !mcps[0].Tools[0].Allowed || mcps[0].Tools[1].Allowed {
		t.Errorf("tools = %+v", mcps[0].Tools)
	}
	if mcps[1].Error != "handshake failed" {
		t.Errorf("error passthrough = %+v", mcps[1])
	}
}

func TestMCPProhibitedPromptAndResourceAreRejected(t *testing.T) {
	ctx := context.Background()
	srv, rt, wsDir := setupMCPDaemon(t, map[string]string{"github.mcp.yaml": mcpManifestWithPolicies})

	fake := &fakeMCPService{
		prompt:   api.MCPPromptResponse{Messages: []api.MCPPromptMessage{{Role: api.RoleUser, Text: "rendered"}}},
		resource: api.MCPResourceResponse{URI: "file://CONTRIBUTING.md", Text: "# Contributing"},
	}
	rt.Workspace().SetMCPService(fake)

	// Allowed prompt reaches the runtime.
	if _, err := srv.GetMCPPrompt(ctx, wsDir, "github", "review-pr", api.MCPPromptRequest{Args: map[string]string{"pr": "42"}}); err != nil {
		t.Fatalf("GetMCPPrompt(allowed): %v", err)
	}
	if fake.promptName != "review-pr" || fake.promptArgs["pr"] != "42" || fake.promptAllowed {
		t.Errorf("prompt call = %q args=%v allowProhibited=%v", fake.promptName, fake.promptArgs, fake.promptAllowed)
	}

	// Prohibited prompt is rejected before the server is touched.
	fake.promptName = ""
	_, err := srv.GetMCPPrompt(ctx, wsDir, "github", "danger", api.MCPPromptRequest{})
	assertAPIError(t, "prohibited prompt", err, http.StatusForbidden, "mcp_policy_prohibited")
	if fake.promptName != "" {
		t.Errorf("prohibited prompt must not reach the MCP runtime, got %q", fake.promptName)
	}

	// The user may authorize a single prohibited use.
	if _, err := srv.GetMCPPrompt(ctx, wsDir, "github", "danger", api.MCPPromptRequest{AllowProhibited: true}); err != nil {
		t.Fatalf("GetMCPPrompt(allowProhibited): %v", err)
	}
	if !fake.promptAllowed {
		t.Errorf("expected AllowProhibited to reach the runtime")
	}

	// Allowed resource read.
	if _, err := srv.ReadMCPResource(ctx, wsDir, "github", api.MCPResourceRequest{URI: "file://CONTRIBUTING.md"}); err != nil {
		t.Fatalf("ReadMCPResource(allowed): %v", err)
	}
	if fake.resourceReq.URI != "file://CONTRIBUTING.md" || fake.resourceReq.AllowProhibited {
		t.Errorf("resource call = %+v", fake.resourceReq)
	}

	// Prohibited resource is rejected.
	_, err = srv.ReadMCPResource(ctx, wsDir, "github", api.MCPResourceRequest{URI: "file://.env.local"})
	assertAPIError(t, "prohibited resource", err, http.StatusForbidden, "mcp_policy_prohibited")
}

func TestMCPProhibitedMessageNamesManifestAndSection(t *testing.T) {
	ctx := context.Background()
	srv, _, wsDir := setupMCPDaemon(t, map[string]string{"github.mcp.yaml": mcpManifestWithPolicies})

	_, err := srv.GetMCPPrompt(ctx, wsDir, "github", "danger", api.MCPPromptRequest{})
	apiErr := &api.APIError{}
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %v", err)
	}
	if !strings.Contains(apiErr.Message, "spec.policies.prompts") {
		t.Errorf("message must name the policy section, got %q", apiErr.Message)
	}
	if !strings.Contains(apiErr.Message, "github.mcp.yaml") {
		t.Errorf("message must name the manifest path, got %q", apiErr.Message)
	}
	if strings.Contains(apiErr.Message, wsDir) {
		t.Errorf("message must not leak the absolute workspace path %q", apiErr.Message)
	}
}

func TestMCPUnknownServerAndInvalidRequests(t *testing.T) {
	ctx := context.Background()
	srv, _, wsDir := setupMCPDaemon(t, map[string]string{"github.mcp.yaml": mcpManifestWithPolicies})

	_, err := srv.RestartMCP(ctx, wsDir, "nope")
	assertAPIError(t, "restart unknown server", err, http.StatusNotFound, "not_found")

	_, err = srv.GetMCPPrompt(ctx, wsDir, "github", "   ", api.MCPPromptRequest{})
	assertAPIError(t, "empty prompt name", err, http.StatusBadRequest, "bad_request")

	_, err = srv.ReadMCPResource(ctx, wsDir, "github", api.MCPResourceRequest{})
	assertAPIError(t, "empty resource URI", err, http.StatusBadRequest, "bad_request")

	_, err = srv.AllowMCPPolicy(ctx, wsDir, "github", api.MCPAllowPolicyRequest{Kind: "widget", Value: "x"})
	assertAPIError(t, "unknown policy kind", err, http.StatusBadRequest, "bad_request")

	_, err = srv.AllowMCPPolicy(ctx, wsDir, "github", api.MCPAllowPolicyRequest{Kind: api.MCPAllowPolicyKindTool})
	assertAPIError(t, "empty policy value", err, http.StatusBadRequest, "bad_request")
}

func TestMCPOperationsWithoutRuntimeReportUnavailable(t *testing.T) {
	ctx := context.Background()
	srv, rt, wsDir := setupMCPDaemon(t, map[string]string{"github.mcp.yaml": mcpManifestWithPolicies})
	detachMCPRuntime(t, rt)

	_, err := srv.RestartMCP(ctx, wsDir, "github")
	assertAPIError(t, "restart without runtime", err, http.StatusServiceUnavailable, ErrCodeMCPUnavailable)

	_, err = srv.AuthenticateMCP(ctx, wsDir, "github")
	assertAPIError(t, "auth without runtime", err, http.StatusServiceUnavailable, ErrCodeMCPUnavailable)

	_, err = srv.AllowMCPPolicy(ctx, wsDir, "github", api.MCPAllowPolicyRequest{
		Kind:  api.MCPAllowPolicyKindTool,
		Value: "create_issue",
	})
	assertAPIError(t, "policies/allow without runtime", err, http.StatusServiceUnavailable, ErrCodeMCPUnavailable)
}

func TestMCPRestartAuthAndAllowPolicyDelegateToRuntime(t *testing.T) {
	ctx := context.Background()
	srv, rt, wsDir := setupMCPDaemon(t, map[string]string{"github.mcp.yaml": mcpManifestWithPolicies})

	fake := &fakeMCPService{
		restarted: api.MCPServer{
			Name:      "github",
			Transport: "stdio",
			Status:    api.MCPStatusConnected,
			Path:      filepath.Join(wsDir, ".agents", "github.mcp.yaml"),
		},
		auth:    api.MCPAuthResponse{AuthURL: "https://example.com/authorize", Status: api.MCPStatusAuthRequired},
		allowed: api.MCPServer{Name: "github", Transport: "stdio", Status: api.MCPStatusConnected},
	}
	rt.Workspace().SetMCPService(fake)

	restarted, err := srv.RestartMCP(ctx, wsDir, "github")
	if err != nil {
		t.Fatalf("RestartMCP: %v", err)
	}
	if fake.restartName != "github" || restarted.Status != api.MCPStatusConnected {
		t.Errorf("restart = %q %+v", fake.restartName, restarted)
	}
	if filepath.IsAbs(restarted.Path) {
		t.Errorf("restarted path must be portable, got %q", restarted.Path)
	}

	auth, err := srv.AuthenticateMCP(ctx, wsDir, "github")
	if err != nil {
		t.Fatalf("AuthenticateMCP: %v", err)
	}
	if fake.authName != "github" || auth.Status != api.MCPStatusAuthRequired {
		t.Errorf("auth = %q %+v", fake.authName, auth)
	}

	updated, err := srv.AllowMCPPolicy(ctx, wsDir, "github", api.MCPAllowPolicyRequest{
		Kind:  api.MCPAllowPolicyKindResource,
		Value: "file://.env.local",
	})
	if err != nil {
		t.Fatalf("AllowMCPPolicy: %v", err)
	}
	if fake.allowName != "github" || fake.allowReq.Kind != api.MCPAllowPolicyKindResource {
		t.Errorf("allow policy = %q %+v", fake.allowName, fake.allowReq)
	}
	if updated.Status != api.MCPStatusConnected {
		t.Errorf("updated server = %+v", updated)
	}
}

func TestWorkspaceMCPDefinitionsResolveByShortAndQualifiedName(t *testing.T) {
	ctx := context.Background()
	srv, rt, wsDir := setupMCPDaemon(t, map[string]string{"github.mcp.yaml": mcpManifestWithPolicies})

	ws := rt.Workspace()
	def, ok := ws.MCPDef("github")
	if !ok || def == nil {
		t.Fatalf("MCPDef(github) not resolved")
	}
	if !strings.HasSuffix(def.QualifiedName(), "/MCP/github") {
		t.Errorf("qualified name = %q, want suffix /MCP/github", def.QualifiedName())
	}
	if byQualified, ok := ws.MCPDef(def.QualifiedName()); !ok || byQualified != def {
		t.Errorf("MCPDef(qualified) = %v", ok)
	}
	if _, ok := ws.MCPDef("missing"); ok {
		t.Errorf("expected missing MCP server to be unresolved")
	}
	if _, ok := ws.MCPDef(""); ok {
		t.Errorf("expected empty MCP name to be unresolved")
	}

	// Policy filtering is enforced from the resolved manifest.
	if def.AllowsTool("delete_repo") {
		t.Errorf("expected delete_repo to be excluded by spec.policies.tools")
	}
	if !def.AllowsTool("create_issue") {
		t.Errorf("expected create_issue to be allowed")
	}
	if def.AllowsResource("file://.env.local") {
		t.Errorf("expected file://.env.local to be excluded by spec.policies.resources")
	}

	// The runtime stays attached across a workspace reload.
	if err := srv.ReloadWorkspace(ctx, wsDir); err != nil {
		t.Fatalf("ReloadWorkspace: %v", err)
	}
	rt2, err := srv.workspace(ctx, wsDir)
	if err != nil {
		t.Fatalf("daemon.workspace after reload: %v", err)
	}
	if _, ok := rt2.Workspace().Catalog().GetMCP("github"); !ok {
		t.Errorf("expected github MCP to survive reload")
	}
}

// TestGetWorkspaceExcludesMCPWarnings verifies MCP server health never reaches the
// workspace payload. The header banner is frozen once the conversation starts, so
// health that changes while the session runs would jitter it and then go stale; the
// CLI reports it in the timeline from the MCP listing instead. Workspace warnings
// that are genuinely durable still have to come through.
func TestGetWorkspaceExcludesMCPWarnings(t *testing.T) {
	ctx := context.Background()
	srv, rt, wsDir := setupMCPDaemon(t, map[string]string{"github.mcp.yaml": mcpManifestWithPolicies})

	rt.Workspace().SetMCPService(&fakeMCPService{list: []api.MCPServer{
		{Name: "github", Status: api.MCPStatusConnected},
		{Name: "linear", Status: api.MCPStatusAuthRequired, AuthType: api.MCPAuthOAuth2},
		{Name: "search", Status: api.MCPStatusError, Error: `connect MCP server "search": connection refused`},
	}})

	ws, err := srv.GetWorkspace(ctx, wsDir)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	for _, warning := range ws.Warnings {
		if strings.Contains(warning, "MCP server") ||
			strings.Contains(warning, "linear") ||
			strings.Contains(warning, "search") {
			t.Errorf("MCP server health must not be reported as a workspace warning: %q", warning)
		}
	}
}

// TestGetWorkspaceWithoutMCPRuntimeHasNoMCPWarnings confirms a workspace with no
// attached MCP manager is unaffected by the warning pass.
func TestGetWorkspaceWithoutMCPRuntimeHasNoMCPWarnings(t *testing.T) {
	ctx := context.Background()
	srv, rt, wsDir := setupMCPDaemon(t, map[string]string{"github.mcp.yaml": mcpManifestWithPolicies})
	detachMCPRuntime(t, rt)

	ws, err := srv.GetWorkspace(ctx, wsDir)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	for _, warning := range ws.Warnings {
		if strings.Contains(warning, "MCP server") {
			t.Errorf("no MCP runtime means no MCP warnings, got %q", warning)
		}
	}
}

// TestCompleteMCPAuthDelegatesToRuntime checks a pasted redirect reaches the MCP
// service trimmed but otherwise untouched, and that the daemon's own rejections
// (blank paste, unknown server, no runtime) never reach the service.
func TestCompleteMCPAuthDelegatesToRuntime(t *testing.T) {
	ctx := context.Background()
	srv, rt, wsDir := setupMCPDaemon(t, map[string]string{"github.mcp.yaml": mcpManifestWithPolicies})
	fake := &fakeMCPService{}
	rt.Workspace().SetMCPService(fake)

	pasted := "http://localhost:56014/callback?code=abc123&state=xyz"
	if err := srv.CompleteMCPAuth(ctx, wsDir, "github", api.MCPAuthCallbackRequest{CallbackURL: "  " + pasted + "  "}); err != nil {
		t.Fatalf("CompleteMCPAuth: %v", err)
	}
	if fake.callbackName != "github" {
		t.Errorf("callback server = %q, want the manifest name github", fake.callbackName)
	}
	if fake.callbackURL != pasted {
		t.Errorf("callback URL = %q, want the trimmed paste %q", fake.callbackURL, pasted)
	}

	// A service-side rejection surfaces unchanged.
	fake.callbackErr = errors.New("no authorization is in progress for github")
	err := srv.CompleteMCPAuth(ctx, wsDir, "github", api.MCPAuthCallbackRequest{CallbackURL: "code=abc"})
	if err == nil || !strings.Contains(err.Error(), "no authorization is in progress") {
		t.Fatalf("CompleteMCPAuth with a rejecting service = %v, want its error", err)
	}
}

func TestCompleteMCPAuthRejectsUnusableRequests(t *testing.T) {
	ctx := context.Background()
	srv, rt, wsDir := setupMCPDaemon(t, map[string]string{"github.mcp.yaml": mcpManifestWithPolicies})
	fake := &fakeMCPService{}
	rt.Workspace().SetMCPService(fake)

	err := srv.CompleteMCPAuth(ctx, wsDir, "github", api.MCPAuthCallbackRequest{CallbackURL: "   "})
	assertAPIError(t, "blank callback URL", err, http.StatusBadRequest, "bad_request")
	if fake.callbackName != "" {
		t.Errorf("callback server = %q, want the service untouched for a blank paste", fake.callbackName)
	}

	err = srv.CompleteMCPAuth(ctx, wsDir, "nope", api.MCPAuthCallbackRequest{CallbackURL: "code=abc"})
	assertAPIError(t, "unknown server", err, http.StatusNotFound, "not_found")
}

func TestCompleteMCPAuthWithoutRuntimeReportsUnavailable(t *testing.T) {
	ctx := context.Background()
	srv, rt, wsDir := setupMCPDaemon(t, map[string]string{"github.mcp.yaml": mcpManifestWithPolicies})
	detachMCPRuntime(t, rt)

	err := srv.CompleteMCPAuth(ctx, wsDir, "github", api.MCPAuthCallbackRequest{CallbackURL: "code=abc"})
	assertAPIError(t, "callback without runtime", err, http.StatusServiceUnavailable, ErrCodeMCPUnavailable)
}
