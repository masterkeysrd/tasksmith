package store

import (
	"context"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
)

// RefreshWorkspaceWarnings fetches the latest workspace warnings from the
// daemon on a background goroutine so runtime auth-health diagnostics appear
// in the statusline immediately.
func RefreshWorkspaceWarnings() {
	c := Client()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ws, err := c.GetWorkspace(ctx)
		if err != nil || ws == nil {
			return
		}
		SetWorkspaceWarnings(ws.Warnings, ws.SandboxWarnings...)
	}()
}

type Warning struct {
	Source  string
	Message string
}

type warningSources struct {
	workspace *[]string
	mcp       *[]api.MCPServer
}

func UseWarnings() []Warning {
	sources := UseApp(func(s AppState) warningSources {
		return warningSources{workspace: s.Warnings, mcp: s.MCPServers}
	})
	return sources.warnings()
}

func (s warningSources) warnings() []Warning {
	var warnings []Warning
	if s.workspace != nil {
		for _, message := range *s.workspace {
			warnings = append(warnings, Warning{Source: "Workspace", Message: message})
		}
	}
	if s.mcp != nil {
		for _, server := range *s.mcp {
			if message := api.MCPServerWarning(server); message != "" {
				warnings = append(warnings, Warning{Source: "MCP", Message: message})
			}
		}
	}
	return warnings
}
