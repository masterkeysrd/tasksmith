package workspace

import (
	"context"
	"errors"
	"fmt"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/warp"
)

// MCPService is the runtime contract for an MCP server manager attached to a
// Workspace. The MCP manager introduced in the runtime milestone implements it;
// until a manager is attached, a Workspace reports the service as unavailable
// and every MCP endpoint answers with a descriptive unavailable error.
//
// Implementations must be safe for concurrent use. ListMCPs returns one entry per
// effective warp.MCP carrying live connection state and everything discovered on
// the server, including items blocked by spec.policies with Allowed set to false.
type MCPService interface {
	// ListMCPs returns every configured MCP server with live status and
	// discovered tools, prompts, and resources, sorted by name.
	ListMCPs(ctx context.Context) []api.MCPServer
	// RestartMCP reconnects the named server, re-discovers its tools, prompts,
	// and resources, refreshes the workspace catalog, and returns the result.
	RestartMCP(ctx context.Context, name string) (api.MCPServer, error)
	// AuthenticateMCP starts or resumes the interactive authorization flow for
	// the named server and reports the URL the user should open.
	AuthenticateMCP(ctx context.Context, name string) (api.MCPAuthResponse, error)
	// SubmitMCPAuthCallback completes the authorization flow in progress for the
	// named server with a redirect URL pasted by the user, for the case where
	// their browser could not reach the daemon's loopback callback listener.
	SubmitMCPAuthCallback(ctx context.Context, name, callbackURL string) error
	// GetMCPPrompt renders the named prompt. When allowProhibited is false, a
	// prompt blocked by spec.policies.prompts is rejected.
	GetMCPPrompt(ctx context.Context, name, prompt string, args map[string]string, allowProhibited bool) (api.MCPPromptResponse, error)
	// ReadMCPResource reads a resource URI. When allowProhibited is false, a
	// resource blocked by spec.policies.resources is rejected.
	ReadMCPResource(ctx context.Context, name string, req api.MCPResourceRequest) (api.MCPResourceResponse, error)
	// AllowPolicyItem updates the named server's writable manifest on disk so
	// kind/value is permitted, reloads the server, and returns the result. Kind
	// is "tool", "prompt", or "resource".
	AllowPolicyItem(ctx context.Context, name string, req api.MCPAllowPolicyRequest) (api.MCPServer, error)
}

// SetMCPService attaches srv as the workspace's MCP runtime, replacing any
// previously attached service. Passing nil detaches the current service so MCP
// endpoints report that no MCP runtime is attached.
func (w *Workspace) SetMCPService(srv MCPService) {
	w.mu.Lock()
	w.mcp = srv
	w.mu.Unlock()
	w.setMCPResourceProbe(w.Catalog(), srv)
}

// setMCPResourceProbe wires catalog's activation of the resource-reading built-in
// tool to srv. The probe deliberately captures srv instead of re-reading the
// workspace's attached service, because the catalog evaluates it while the
// workspace write lock is held during a reload and locking there would deadlock.
// Every path that attaches or replaces a runtime re-installs the probe, so the
// captured value cannot go stale.
func (w *Workspace) setMCPResourceProbe(catalog *Catalog, srv MCPService) {
	if catalog == nil {
		return
	}
	probe, ok := srv.(interface{ HasAllowedMCPResources() bool })
	if !ok || srv == nil {
		catalog.SetMCPResourceProbe(nil)
		return
	}
	catalog.SetMCPResourceProbe(probe.HasAllowedMCPResources)
}

// MCP returns the workspace's attached MCP runtime, or nil when no manager is
// attached (MCP servers are then discoverable as manifests but not connected).
func (w *Workspace) MCP() MCPService {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.mcp
}

// MCPDef resolves the effective warp.MCP definition with the given short or
// qualified name in this workspace's scope.
func (w *Workspace) MCPDef(name string) (*warp.MCP, bool) {
	return w.Catalog().GetMCP(name)
}

// MCPDefinitions returns every effective MCP definition in scope, satisfying the
// MCP manager's definition resolver.
func (w *Workspace) MCPDefinitions() []*warp.MCP {
	return w.Catalog().MCPs()
}

// RenderOptions builds the MCP render context for this workspace, supplying the
// workspace and project definitions commands and endpoints may reference.
func (w *Workspace) RenderOptions() warp.MCPRenderOptions {
	catalog := w.Catalog()
	return warp.MCPRenderOptions{
		Workspace: catalog.WorkspaceSpec(),
		Project:   catalog.Project(),
	}
}

// SyncMCPTools registers the tools discovered on one MCP server with the
// workspace registry and refreshes the catalog so tool lists, statuses, and token
// counts reflect discovery. rawTools carries everything the server exposes; the
// registry registers only those spec.policies allows, and the remainder is recorded
// as policy-blocked so tool listings can show them as denied rather than omit them.
// Passing an empty slice clears the server's tools. It satisfies the MCP manager's
// syncer contract.
func (w *Workspace) SyncMCPTools(def *warp.MCP, rawTools []*warp.Tool) error {
	if def == nil {
		return errors.New("workspace: MCP definition is nil")
	}
	registry := w.Catalog().Registry()
	if registry == nil {
		return errors.New("workspace: registry is not loaded")
	}
	var allowed, blocked []*warp.Tool
	for _, t := range rawTools {
		if t != nil && !def.AllowsTool(t.Metadata.Name) {
			blocked = append(blocked, t)
			continue
		}
		allowed = append(allowed, t)
	}
	if err := registry.SetMCPTools(def.QualifiedName(), allowed); err != nil {
		return fmt.Errorf("workspace: register tools for MCP %q: %w", def.Metadata.Name, err)
	}
	w.Catalog().SetDeniedMCPTools(def, blocked)
	return w.Catalog().Refresh()
}
