package tools

import (
	"context"
	"fmt"
	"strings"
)

const maxMcpListResourcesLimit = 50

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
	totalCount := 0
	for _, server := range serversToList {
		resList, err := multiClient.Resources(ctx, server)
		if err != nil {
			if in.ServerName != "" {
				return McpListResourcesOutput{Success: false, Error: fmt.Sprintf("failed to list resources for %q: %v", server, err)}, nil
			}
			continue
		}

		for _, r := range resList {
			totalCount++
			if len(resources) < maxMcpListResourcesLimit {
				resources = append(resources, McpListResourcesOutputResourcesItem{
					Server:      server,
					Name:        r.Name,
					Uri:         r.URI,
					Description: r.Description,
					MimeType:    r.MIMEType,
				})
			}
		}
	}

	return McpListResourcesOutput{
		Success:    true,
		Resources:  resources,
		TotalCount: totalCount,
		Truncated:  totalCount > maxMcpListResourcesLimit,
	}, nil
}

// TextContent formats the resources into a dense list:
// - [ServerName] ResourceName (URI): Description.
func (o McpListResourcesOutput) TextContent() string {
	if o.Error != "" {
		return fmt.Sprintf("Error: %s", o.Error)
	}

	if len(o.Resources) == 0 {
		return "No resources found."
	}

	var sb strings.Builder
	sb.WriteString("MCP Resources:\n")
	for _, r := range o.Resources {
		desc := r.Description
		if desc != "" {
			desc = ": " + desc
		}
		fmt.Fprintf(&sb, "- [%s] %s (%s)%s\n", r.Server, r.Name, r.Uri, desc)
	}

	res := sb.String()
	res = strings.TrimSuffix(res, "\n")

	if o.Truncated {
		res += fmt.Sprintf("\n\n[SYSTEM NOTE: Showing %d of %d resources. Call mcp_list_resources again with a specific server_name.]", len(o.Resources), o.TotalCount)
	}

	return res
}
