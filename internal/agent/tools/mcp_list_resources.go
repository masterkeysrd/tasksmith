package tools

import (
	"context"
	"fmt"
	"strings"
)

// McpListResources lists resources from MCP servers.
func (h *ToolHandlers) McpListResources(ctx context.Context, in McpListResourcesArgs) (McpListResourcesOutput, error) {
	if h.McpManager == nil {
		return McpListResourcesOutput{Success: false, Error: "No MCP manager configured"}, nil
	}

	multiClient := h.McpManager.MultiClient()
	if multiClient == nil {
		return McpListResourcesOutput{Success: false, Error: "MCP MultiClient is not initialized"}, nil
	}

	var serversToList []string
	if in.ServerName != "" {
		found := false
		for _, sName := range h.McpManager.ServerNames() {
			if strings.EqualFold(sName, in.ServerName) {
				serversToList = append(serversToList, sName)
				found = true
				break
			}
		}
		if !found {
			return McpListResourcesOutput{Success: false, Error: fmt.Sprintf("MCP server %q not found", in.ServerName)}, nil
		}
	} else {
		serversToList = h.McpManager.ServerNames()
	}

	var resources []McpListResourcesOutputResourcesItem
	for _, server := range serversToList {
		resList, err := multiClient.Resources(ctx, server)
		if err != nil {
			if in.ServerName != "" {
				return McpListResourcesOutput{Success: false, Error: fmt.Sprintf("failed to list resources for %q: %v", server, err)}, nil
			}
			continue
		}

		for _, r := range resList {
			resources = append(resources, McpListResourcesOutputResourcesItem{
				Server:      server,
				Name:        r.Name,
				Uri:         r.URI,
				Description: r.Description,
				MimeType:    r.MIMEType,
			})
		}
	}

	return McpListResourcesOutput{
		Success:   true,
		Resources: resources,
	}, nil
}
