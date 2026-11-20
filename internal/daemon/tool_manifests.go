package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// resourceAPIError maps the workspace tool/MCP patch layer's sentinel errors
// onto HTTP semantics so clients can distinguish "no such resource" from
// "read-only manifest" or "an exclusion pattern still matches" without parsing
// message text.
func resourceAPIError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, workspace.ErrToolNotFound), errors.Is(err, workspace.ErrMCPNotFound):
		return &api.APIError{StatusCode: http.StatusNotFound, Code: "not_found", Message: err.Error()}
	case errors.Is(err, workspace.ErrManifestReadOnly):
		return &api.APIError{StatusCode: http.StatusConflict, Code: "manifest_read_only", Message: err.Error()}
	case errors.Is(err, workspace.ErrNoSafePolicyEdit):
		return &api.APIError{StatusCode: http.StatusConflict, Code: "policy_pattern_conflict", Message: err.Error()}
	default:
		return err
	}
}

// SetToolEnabled patches availability for the named tool — spec.enabled on its
// own manifest when writable, otherwise a WORKSPACE.md tools policy exclusion —
// and hot-reloads the workspace so sessions, listings, and the deferred tool
// catalog agree immediately.
func (d *Daemon) SetToolEnabled(ctx context.Context, rawWS, name string, enabled bool) ([]api.Tool, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, &api.APIError{StatusCode: http.StatusBadRequest, Code: "bad_request", Message: "tool name is required"}
	}
	rt.reloadMu.Lock()
	defer rt.reloadMu.Unlock()
	if err := resourceAPIError(rt.Workspace().SetToolEnabled(name, enabled)); err != nil {
		return nil, err
	}
	if err := rt.reloadLocked(ctx); err != nil {
		return nil, fmt.Errorf("daemon: reload workspace after tool update: %w", err)
	}
	return d.ListTools(ctx, rawWS, "")
}

// SetMCPEnabled patches availability for the named MCP server and hot-reloads
// the workspace. The reload's MCP sync closes a newly disabled server (and
// unregisters its tools), while re-enabling spawns and connects it.
func (d *Daemon) SetMCPEnabled(ctx context.Context, rawWS, name string, enabled bool) ([]api.MCPServer, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, &api.APIError{StatusCode: http.StatusBadRequest, Code: "bad_request", Message: "MCP server name is required"}
	}
	rt.reloadMu.Lock()
	defer rt.reloadMu.Unlock()
	if err := resourceAPIError(rt.RootWorkspace().SetMCPEnabled(name, enabled)); err != nil {
		return nil, err
	}
	if err := rt.reloadLocked(ctx); err != nil {
		return nil, fmt.Errorf("daemon: reload workspace after MCP update: %w", err)
	}
	return d.ListMCPs(ctx, rawWS)
}
