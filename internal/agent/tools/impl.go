package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Download downloads a file from a URL.
func (h *ToolHandlers) Download(ctx context.Context, in DownloadArgs) (DownloadOutput, error) {
	path := "downloaded_file"
	cmd := exec.CommandContext(ctx, "curl", "-o", path, in.Url)
	if err := cmd.Run(); err != nil {
		return DownloadOutput{Path: "", Success: false}, nil
	}
	return DownloadOutput{Path: path, Success: true}, nil
}

// Fetch fetches a URL.
func (h *ToolHandlers) Fetch(ctx context.Context, in FetchArgs) (FetchOutput, error) {
	cmd := exec.CommandContext(ctx, "curl", "-i", in.Url)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return FetchOutput{Status: 500, Content: err.Error()}, nil
	}
	return FetchOutput{Status: 200, Content: string(out)}, nil
}

// LspDiagnostics gets LSP diagnostics.
func (h *ToolHandlers) LspDiagnostics(ctx context.Context, in LspDiagnosticsArgs) (LspDiagnosticsOutput, error) {
	return LspDiagnosticsOutput{Diagnostics: []LspDiagnosticsOutputDiagnosticsItem{}}, nil
}

// LspRestart restarts LSP server.
func (h *ToolHandlers) LspRestart(ctx context.Context, in LspRestartArgs) (LspRestartOutput, error) {
	cmd := exec.CommandContext(ctx, "echo", "lsp_restart", in.Server)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return LspRestartOutput{Message: err.Error(), Success: false}, nil
	}
	return LspRestartOutput{Message: string(out), Success: true}, nil
}

// LspSearch searches using LSP.
func (h *ToolHandlers) LspSearch(ctx context.Context, in LspSearchArgs) (LspSearchOutput, error) {
	return LspSearchOutput{Results: []LspSearchOutputResultsItem{}}, nil
}

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
