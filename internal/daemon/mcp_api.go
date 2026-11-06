package daemon

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/mcp"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

// ErrCodeMCPUnavailable is the API error code returned when an MCP operation is
// requested but the workspace has no MCP runtime attached to connect its
// configured servers.
const ErrCodeMCPUnavailable = "mcp_unavailable"

// The workspace MCP manager is the runtime every MCP endpoint below delegates to.
var _ workspace.MCPService = (*mcp.Manager)(nil)

// mcpPolicySections maps an allow-policy kind to the manifest policy section it
// governs, which is also the name surfaced in policy rejection messages.
var mcpPolicySections = map[string]string{
	api.MCPAllowPolicyKindTool:     "spec.policies.tools",
	api.MCPAllowPolicyKindPrompt:   "spec.policies.prompts",
	api.MCPAllowPolicyKindResource: "spec.policies.resources",
}

// mcpRuntime returns the MCP runtime connected to the workspace's root, or a
// structured 503 error explaining that the workspace's MCP servers are not
// connected. Sibling worktrees share their root's runtime, because MCP tools are
// registered against the root's tool registry.
func mcpRuntime(rt *workspaceRuntime) (workspace.MCPService, error) {
	if srv := rt.MCPRuntime(); srv != nil {
		return srv, nil
	}
	return nil, &api.APIError{
		StatusCode: http.StatusServiceUnavailable,
		Code:       ErrCodeMCPUnavailable,
		Message:    "workspace has no MCP runtime attached; MCP servers are discovered from manifests but not connected",
	}
}

// mcpServerNotFound builds the 404 error for an unknown MCP server name.
func mcpServerNotFound(name string) error {
	return &api.APIError{
		StatusCode: http.StatusNotFound,
		Code:       "not_found",
		Message:    fmt.Sprintf("MCP server %q not found in workspace", name),
	}
}

// mcpPolicyBlocked builds the 403 error naming the manifest path and policy
// section that prohibits the item, so both the model and the CLI can explain and
// resolve the block.
func mcpPolicyBlocked(ws *workspace.Workspace, kind, item, manifestPath string) error {
	section, ok := mcpPolicySections[kind]
	if !ok {
		section = "spec.policies"
	}
	message := fmt.Sprintf("MCP %s %q is prohibited by %s", kind, item, section)
	if portable := strings.TrimSpace(ws.PortablePath(manifestPath)); portable != "" {
		message += " in " + portable
	}
	return &api.APIError{
		StatusCode: http.StatusForbidden,
		Code:       "mcp_policy_prohibited",
		Message:    message,
	}
}

// resolveMCP resolves the effective MCP server definition for name from the
// workspace root, returning a 404 error when no such server is declared.
func (d *Daemon) resolveMCP(ctx context.Context, rawWS, name string) (*workspaceRuntime, *warp.MCP, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil, &api.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "bad_request",
			Message:    "MCP server name must not be empty",
		}
	}
	snap, ok := rt.RootWorkspace().Catalog().GetMCPSnapshot(name)
	if !ok || snap.Def == nil {
		return nil, nil, mcpServerNotFound(name)
	}
	return rt, snap.Def, nil
}

// ListMCPs returns every MCP server configured in the workspace sorted by name.
// When an MCP runtime is attached, its live connection state and discovered
// tools, prompts, and resources are reported; otherwise the servers are reported
// from their manifests with a disconnected status.
func (d *Daemon) ListMCPs(ctx context.Context, rawWS string) ([]api.MCPServer, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}
	ws := rt.RootWorkspace()

	if srv := rt.MCPRuntime(); srv != nil {
		listed := srv.ListMCPs(ctx)
		out := make([]api.MCPServer, 0, len(listed))
		for _, server := range listed {
			server.Path = ws.PortablePath(server.Path)
			out = append(out, server)
		}
		slices.SortFunc(out, func(a, b api.MCPServer) int {
			return strings.Compare(a.Name, b.Name)
		})
		return out, nil
	}

	snaps := ws.Catalog().MCPSnapshots()
	out := make([]api.MCPServer, 0, len(snaps))
	for _, snap := range snaps {
		server := snap.MCPServer
		server.Path = ws.PortablePath(server.Path)
		out = append(out, server)
	}
	return out, nil
}

// RestartMCP reconnects the named MCP server, re-discovers its tools, prompts,
// and resources, and returns the updated server state.
func (d *Daemon) RestartMCP(ctx context.Context, rawWS, name string) (api.MCPServer, error) {
	rt, def, err := d.resolveMCP(ctx, rawWS, name)
	if err != nil {
		return api.MCPServer{}, err
	}
	srv, err := mcpRuntime(rt)
	if err != nil {
		return api.MCPServer{}, err
	}
	server, err := srv.RestartMCP(ctx, def.Metadata.Name)
	if err != nil {
		return api.MCPServer{}, err
	}
	server.Path = rt.RootWorkspace().PortablePath(server.Path)
	return server, nil
}

// AuthenticateMCP starts or resumes the interactive authorization flow for the
// named MCP server and returns the URL the user should open.
func (d *Daemon) AuthenticateMCP(ctx context.Context, rawWS, name string) (api.MCPAuthResponse, error) {
	rt, def, err := d.resolveMCP(ctx, rawWS, name)
	if err != nil {
		return api.MCPAuthResponse{}, err
	}
	srv, err := mcpRuntime(rt)
	if err != nil {
		return api.MCPAuthResponse{}, err
	}
	return srv.AuthenticateMCP(ctx, def.Metadata.Name)
}

// CompleteMCPAuth feeds a redirect URL the user pasted to the authorization flow
// in progress for the named MCP server. It exists for the common case where the
// browser cannot reach the daemon's loopback callback listener — the user is on a
// remote host, or localhost is blocked — and can only copy the address bar.
func (d *Daemon) CompleteMCPAuth(ctx context.Context, rawWS, name string, req api.MCPAuthCallbackRequest) error {
	rt, def, err := d.resolveMCP(ctx, rawWS, name)
	if err != nil {
		return err
	}
	callbackURL := strings.TrimSpace(req.CallbackURL)
	if callbackURL == "" {
		return &api.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "bad_request",
			Message:    "MCP callback URL must not be empty",
		}
	}
	srv, err := mcpRuntime(rt)
	if err != nil {
		return err
	}
	return srv.SubmitMCPAuthCallback(ctx, def.Metadata.Name, callbackURL)
}

// GetMCPPrompt renders a prompt discovered on the named MCP server. A prompt
// prohibited by the server manifest's policies is rejected with HTTP 403 unless
// the user-authorized AllowProhibited flag is set.
func (d *Daemon) GetMCPPrompt(ctx context.Context, rawWS, name, prompt string, req api.MCPPromptRequest) (api.MCPPromptResponse, error) {
	rt, def, err := d.resolveMCP(ctx, rawWS, name)
	if err != nil {
		return api.MCPPromptResponse{}, err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return api.MCPPromptResponse{}, &api.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "bad_request",
			Message:    "MCP prompt name must not be empty",
		}
	}
	ws := rt.RootWorkspace()
	if !req.AllowProhibited && !def.AllowsPrompt(prompt) {
		return api.MCPPromptResponse{}, mcpPolicyBlocked(ws, api.MCPAllowPolicyKindPrompt, prompt, def.GetPath())
	}
	srv, err := mcpRuntime(rt)
	if err != nil {
		return api.MCPPromptResponse{}, err
	}
	return srv.GetMCPPrompt(ctx, def.Metadata.Name, prompt, req.Args, req.AllowProhibited)
}

// ReadMCPResource reads a resource URI from the named MCP server. A resource
// prohibited by the server manifest's policies is rejected with HTTP 403 unless
// the user-authorized AllowProhibited flag is set.
func (d *Daemon) ReadMCPResource(ctx context.Context, rawWS, name string, req api.MCPResourceRequest) (api.MCPResourceResponse, error) {
	rt, def, err := d.resolveMCP(ctx, rawWS, name)
	if err != nil {
		return api.MCPResourceResponse{}, err
	}
	uri := strings.TrimSpace(req.URI)
	if uri == "" {
		return api.MCPResourceResponse{}, &api.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "bad_request",
			Message:    "MCP resource URI must not be empty",
		}
	}
	req.URI = uri
	ws := rt.RootWorkspace()
	if !req.AllowProhibited && !def.AllowsResource(uri) {
		return api.MCPResourceResponse{}, mcpPolicyBlocked(ws, api.MCPAllowPolicyKindResource, uri, def.GetPath())
	}
	srv, err := mcpRuntime(rt)
	if err != nil {
		return api.MCPResourceResponse{}, err
	}
	return srv.ReadMCPResource(ctx, def.Metadata.Name, req)
}

// AllowMCPPolicy updates the named server's writable manifest on disk so
// req.Value is permitted under the matching spec.policies section, reloads the
// server, and returns the updated state.
func (d *Daemon) AllowMCPPolicy(ctx context.Context, rawWS, name string, req api.MCPAllowPolicyRequest) (api.MCPServer, error) {
	rt, def, err := d.resolveMCP(ctx, rawWS, name)
	if err != nil {
		return api.MCPServer{}, err
	}
	req.Kind = strings.ToLower(strings.TrimSpace(req.Kind))
	if _, ok := mcpPolicySections[req.Kind]; !ok {
		return api.MCPServer{}, &api.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "bad_request",
			Message: fmt.Sprintf("MCP policy kind %q must be one of %s, %s, or %s", req.Kind,
				api.MCPAllowPolicyKindTool, api.MCPAllowPolicyKindPrompt, api.MCPAllowPolicyKindResource),
		}
	}
	req.Value = strings.TrimSpace(req.Value)
	if req.Value == "" {
		return api.MCPServer{}, &api.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "bad_request",
			Message:    "MCP policy value must not be empty",
		}
	}
	srv, err := mcpRuntime(rt)
	if err != nil {
		return api.MCPServer{}, err
	}
	server, err := srv.AllowPolicyItem(ctx, def.Metadata.Name, req)
	if err != nil {
		return api.MCPServer{}, err
	}
	server.Path = rt.RootWorkspace().PortablePath(server.Path)
	return server, nil
}
