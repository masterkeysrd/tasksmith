package tools

import (
	"context"
	"strings"
)

// McpReadResources reads resources from MCP.
func (h *ToolHandlers) McpReadResources(ctx context.Context, in McpReadResourcesArgs) (McpReadResourcesOutput, error) {
	if h.McpManager == nil {
		return McpReadResourcesOutput{Success: false, Content: "No MCP manager configured"}, nil
	}

	multiClient := h.McpManager.MultiClient()

	// Parse scheme/server name
	parts := strings.SplitN(in.Uri, "://", 2)
	if len(parts) < 2 {
		return McpReadResourcesOutput{Success: false, Content: "Invalid URI format: expected scheme://path"}, nil
	}

	scheme := parts[0]

	// Find the matching server. We try the scheme first.
	var matchingServer string
	for _, sName := range h.McpManager.ServerNames() {
		if strings.EqualFold(sName, scheme) {
			matchingServer = sName
			break
		}
	}

	var content string
	var err error

	if matchingServer != "" {
		res, errVal := multiClient.GetResources(ctx, matchingServer, []string{in.Uri})
		if errVal == nil {
			content = res.Text()
		} else {
			err = errVal
		}
	}

	// If scheme did not match or GetResources failed, fallback to querying other servers
	if content == "" {
		for _, sName := range h.McpManager.ServerNames() {
			if sName == matchingServer {
				continue
			}
			res, errTry := multiClient.GetResources(ctx, sName, []string{in.Uri})
			if errTry == nil && len(res) > 0 {
				content = res.Text()
				err = nil
				break
			}
		}
	}

	if err != nil {
		return McpReadResourcesOutput{Success: false, Content: err.Error()}, nil
	}

	if content == "" {
		return McpReadResourcesOutput{Success: false, Content: "Resource not found or empty"}, nil
	}

	return McpReadResourcesOutput{Content: content, Success: true}, nil
}
