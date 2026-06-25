package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/warp"
)

// LoadActiveTools discovers, filters, and loads all active tools (built-in and MCP-based) based on the workspace configurations.
func LoadActiveTools(ctx context.Context, handlers *ToolHandlers, allowedTools map[string]bool, mcps []*warp.MCP) ([]*tool.Tool, error) {
	allLoomTools, err := LoomTools(handlers)
	if err != nil {
		return nil, fmt.Errorf("failed to load loom tools: %w", err)
	}

	// Discover and merge MCP tools
	if handlers.McpManager != nil {
		mcpTools, err := handlers.McpManager.DiscoverTools(ctx, mcps)
		if err != nil {
			log.ForComponent("mcp").Error("failed to discover MCP tools", log.Err(err))
		} else {
			allLoomTools = append(allLoomTools, mcpTools...)
		}
	}

	var activeTools []*tool.Tool
	for _, lt := range allLoomTools {
		if IsToolAllowed(lt.Definition.Name, allowedTools) {
			activeTools = append(activeTools, lt)
		}
	}

	return activeTools, nil
}

// IsToolAllowed returns true if the tool matches the whitelisted tools (supporting suffix wildcards).
func IsToolAllowed(toolName string, allowed map[string]bool) bool {
	if allowed == nil {
		return true
	}
	if allowed[toolName] {
		return true
	}
	for pattern := range allowed {
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(toolName, prefix) {
				return true
			}
		}
	}
	return false
}
