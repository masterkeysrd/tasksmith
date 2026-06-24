package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// McpReadResources reads resources from MCP.
func (h *ToolHandlers) McpReadResources(ctx context.Context, in McpReadResourcesArgs) (McpReadResourcesOutput, error) {
	cmd := exec.CommandContext(ctx, "echo", "mcp_read_resources", in.Uri)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return McpReadResourcesOutput{Success: false}, nil
	}
	return McpReadResourcesOutput{Content: string(out), Success: true}, nil
}

const MaxEditDiffLines = 500

func truncateDiff(diffStr string) (string, string) {
	lines := strings.Split(diffStr, "\n")
	if len(lines) <= MaxEditDiffLines {
		return diffStr, ""
	}
	truncated := strings.Join(lines[:MaxEditDiffLines], "\n")
	return fmt.Sprintf("%s\n\n[SYSTEM NOTE: Diff truncated to save tokens. Showing first %d of %d lines of diff. The full diff was successfully applied to the file.]", truncated, MaxEditDiffLines, len(lines)), diffStr
}
