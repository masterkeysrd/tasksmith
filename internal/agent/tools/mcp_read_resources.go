package tools

import (
	"context"
	"fmt"
	"path/filepath"
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

	truncated := false
	var cachedPath string
	if len(content) > MaxTotalChars {
		truncated = true
		fullContent := content
		content = fullContent[:MaxTotalChars]

		if h.Storage != nil {
			filename := filepath.Base(in.Uri)
			if filename == "" || filename == "." || filename == "/" {
				filename = "resource.txt"
			}
			if !strings.HasSuffix(filename, ".txt") && !strings.HasSuffix(filename, ".md") {
				filename += ".txt"
			}
			toolCallID, _ := ctx.Value("tool_call_id").(string)
			if toolCallID == "" {
				toolCallID = "unknown"
			}
			storagePath := fmt.Sprintf("%s_%s", toolCallID, filename)
			var errSave error
			cachedPath, errSave = h.Storage.Save(ctx, storagePath, strings.NewReader(fullContent))
			if errSave != nil {
				return McpReadResourcesOutput{Success: false, Content: fmt.Sprintf("failed to cache truncated content: %v", errSave)}, nil
			}
		}
	}

	return McpReadResourcesOutput{
		Content:    content,
		Success:    true,
		Truncated:  truncated,
		CachedPath: cachedPath,
	}, nil
}

// TextContent formats the read resource content.
func (o McpReadResourcesOutput) TextContent() string {
	if !o.Success {
		return fmt.Sprintf("Error: %s", o.Content)
	}

	var sb strings.Builder
	sb.WriteString(o.Content)

	if o.Truncated {
		fmt.Fprintf(&sb, "\n\n[SYSTEM NOTE: Content truncated due to size limits. Full content saved to cache. To read further, use the view tool with path=%s]", o.CachedPath)
	}

	return sb.String()
}
