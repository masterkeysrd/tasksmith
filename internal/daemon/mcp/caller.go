package mcp

import (
	"context"

	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/api"
)

// ToolRuntime adapts a [Manager] to the narrow MCP contract the tool container
// needs. The manager's own resource reader takes an [api.MCPResourceRequest]
// because it backs the HTTP API, while the container calls with plain arguments, so
// the adapter keeps each side's surface honest instead of widening either one.
//
// A nil *Manager adapts to a runtime that reports no resources and rejects calls,
// so wiring a session never needs a nil check at every call site.
type ToolRuntime struct {
	manager *Manager
}

// NewToolRuntime returns the tool-facing view of m. It returns nil when m is nil so
// a workspace without an MCP manager leaves the container's MCP tools unregistered.
func NewToolRuntime(m *Manager) *ToolRuntime {
	if m == nil {
		return nil
	}
	return &ToolRuntime{manager: m}
}

// CallMCPTool executes rawToolName on the named server.
func (r *ToolRuntime) CallMCPTool(ctx context.Context, serverName, rawToolName string, args map[string]any) (tool.ToolStream, error) {
	if r == nil {
		return nil, ErrManagerClosed
	}
	return r.manager.CallMCPTool(ctx, serverName, rawToolName, args)
}

// ReadMCPResource reads one resource URI from the named server.
func (r *ToolRuntime) ReadMCPResource(ctx context.Context, serverName, uri string, allowProhibited bool) (api.MCPResourceResponse, error) {
	if r == nil {
		return api.MCPResourceResponse{}, ErrManagerClosed
	}
	return r.manager.ReadMCPResource(ctx, serverName, api.MCPResourceRequest{URI: uri, AllowProhibited: allowProhibited})
}

// HasAllowedMCPResources reports whether any server exposes a permitted resource.
func (r *ToolRuntime) HasAllowedMCPResources() bool {
	if r == nil {
		return false
	}
	return r.manager.HasAllowedMCPResources()
}
