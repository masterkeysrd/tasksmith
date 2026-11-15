package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// readMCPResourceTool reads one resource URI from a configured MCP server.
//
// It always reads with prohibited resources disallowed: authorizing a prohibited
// read is an interactive user decision made in the /mcp browser, never something the
// model can request by setting a flag. Large resource text is spilled through the
// container's spiller so a bulky document becomes a preview plus a path rather than
// a context overflow.
type readMCPResourceTool struct {
	workspace  *workspace.Workspace
	caller     tools.MCPCaller
	annotation tool.Annotation
}

func newReadMCPResourceTool(ws *workspace.Workspace, caller tools.MCPCaller, annotation tool.Annotation) *readMCPResourceTool {
	return &readMCPResourceTool{workspace: ws, caller: caller, annotation: annotation}
}

func (t *readMCPResourceTool) Prepare(_ context.Context, in api.ReadMCPResourceInput) (*tools.Plan, error) {
	server := strings.TrimSpace(in.Server)
	uri := strings.TrimSpace(in.URI)
	if server == "" {
		return nil, fmt.Errorf("%w: server is required", std.ErrInvalidArgument)
	}
	if uri == "" {
		return nil, fmt.Errorf("%w: uri is required", std.ErrInvalidArgument)
	}
	if err := tools.CheckMCPResourceAllowed(t.workspace, server, uri); err != nil {
		return nil, err
	}

	// The target pairs server with URI so a granted rule approves one resource (or
	// one server's resources with a trailing glob) instead of every URI the tool can
	// reach.
	return tools.PlanFromAnnotation(ToolReadMCPResource, t.annotation, server+"/"+uri,
		fmt.Sprintf("Read MCP resource: %s", uri)), nil
}

func (t *readMCPResourceTool) Execute(ctx context.Context, in api.ReadMCPResourceInput) (api.ReadMCPResourceOutput, error) {
	server := strings.TrimSpace(in.Server)
	uri := strings.TrimSpace(in.URI)

	resp, err := tools.ReadMCPResource(ctx, t.workspace, t.caller, server, uri, false)
	if err != nil {
		return api.ReadMCPResourceOutput{}, err
	}

	spilled := tools.Spill(ctx, resp.Text)
	return api.ReadMCPResourceOutput{
		Server:    server,
		URI:       resp.URI,
		Text:      spilled,
		MimeType:  resp.MIMEType,
		Truncated: spilled != resp.Text,
	}, nil
}
