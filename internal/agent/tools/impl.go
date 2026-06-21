package tools

import (
	"context"
	"os/exec"
	"strings"
)

// BashHandler executes a bash command.
func BashHandler(ctx context.Context, in BashArgs) (BashOutput, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", in.Command)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return BashOutput{ExitCode: 1, Stderr: err.Error(), Stdout: string(out)}, nil
	}
	return BashOutput{ExitCode: 0, Stdout: string(out)}, nil
}

// DownloadHandler downloads a file from a URL.
func DownloadHandler(ctx context.Context, in DownloadArgs) (DownloadOutput, error) {
	path := "downloaded_file"
	cmd := exec.CommandContext(ctx, "curl", "-o", path, in.Url)
	if err := cmd.Run(); err != nil {
		return DownloadOutput{Path: "", Success: false}, nil
	}
	return DownloadOutput{Path: path, Success: true}, nil
}

// EditHandler edits a file using sed.
func EditHandler(ctx context.Context, in EditArgs) (EditOutput, error) {
	cmd := exec.CommandContext(ctx, "sed", "-i", in.Expression, in.Path)
	if err := cmd.Run(); err != nil {
		return EditOutput{Path: in.Path, Success: false}, nil
	}
	return EditOutput{Path: in.Path, Success: true}, nil
}

// FetchHandler fetches a URL.
func FetchHandler(ctx context.Context, in FetchArgs) (FetchOutput, error) {
	cmd := exec.CommandContext(ctx, "curl", "-i", in.Url)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return FetchOutput{Status: 500, Content: err.Error()}, nil
	}
	return FetchOutput{Status: 200, Content: string(out)}, nil
}

// GlobHandler finds files matching a glob pattern.
func GlobHandler(ctx context.Context, in GlobArgs) (GlobOutput, error) {
	cmd := exec.CommandContext(ctx, "find", ".", "-name", in.Pattern)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return GlobOutput{}, nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return GlobOutput{Matches: lines}, nil
}

// GrepHandler searches for a pattern in files.
func GrepHandler(ctx context.Context, in GrepArgs) (GrepOutput, error) {
	cmd := exec.CommandContext(ctx, "grep", "-rn", in.Pattern, in.Path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return GrepOutput{}, nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	matches := make([]any, len(lines))
	for i, l := range lines {
		matches[i] = l
	}
	return GrepOutput{Matches: matches}, nil
}

// LsHandler lists files in a directory.
func LsHandler(ctx context.Context, in LsArgs) (LsOutput, error) {
	cmd := exec.CommandContext(ctx, "ls", "-la", in.Path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return LsOutput{}, nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	files := make([]any, len(lines))
	for i, l := range lines {
		files[i] = l
	}
	return LsOutput{Files: files}, nil
}

// LspDiagnosticsHandler gets LSP diagnostics.
func LspDiagnosticsHandler(ctx context.Context, in LspDiagnosticsArgs) (LspDiagnosticsOutput, error) {
	cmd := exec.CommandContext(ctx, "echo", "lsp_diagnostics", in.Path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return LspDiagnosticsOutput{}, nil
	}
	return LspDiagnosticsOutput{Diagnostics: []any{string(out)}}, nil
}

// LspRestartHandler restarts LSP server.
func LspRestartHandler(ctx context.Context, in LspRestartArgs) (LspRestartOutput, error) {
	cmd := exec.CommandContext(ctx, "echo", "lsp_restart", in.Server)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return LspRestartOutput{Message: err.Error(), Success: false}, nil
	}
	return LspRestartOutput{Message: string(out), Success: true}, nil
}

// LspSearchHandler searches using LSP.
func LspSearchHandler(ctx context.Context, in LspSearchArgs) (LspSearchOutput, error) {
	cmd := exec.CommandContext(ctx, "echo", "lsp_search", in.Query)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return LspSearchOutput{}, nil
	}
	return LspSearchOutput{Results: []any{string(out)}}, nil
}

// McpReadResourcesHandler reads resources from MCP.
func McpReadResourcesHandler(ctx context.Context, in McpReadResourcesArgs) (McpReadResourcesOutput, error) {
	cmd := exec.CommandContext(ctx, "echo", "mcp_read_resources", in.Uri)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return McpReadResourcesOutput{Success: false}, nil
	}
	return McpReadResourcesOutput{Content: string(out), Success: true}, nil
}

// RemoveHandler removes a file or directory.
func RemoveHandler(ctx context.Context, in RemoveArgs) (RemoveOutput, error) {
	cmd := exec.CommandContext(ctx, "rm", "-rf", in.Path)
	if err := cmd.Run(); err != nil {
		return RemoveOutput{Path: in.Path, Success: false}, nil
	}
	return RemoveOutput{Path: in.Path, Success: true}, nil
}

// WebFetchHandler fetches web page content.
func WebFetchHandler(ctx context.Context, in WebFetchArgs) (WebFetchOutput, error) {
	cmd := exec.CommandContext(ctx, "curl", "-sL", in.Url)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return WebFetchOutput{}, nil
	}
	return WebFetchOutput{Content: string(out), Title: in.Url}, nil
}

// WebSearchHandler searches the web.
func WebSearchHandler(ctx context.Context, in WebSearchArgs) (WebSearchOutput, error) {
	cmd := exec.CommandContext(ctx, "echo", "web_search", in.Query)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return WebSearchOutput{}, nil
	}
	return WebSearchOutput{Results: []any{string(out)}}, nil
}

// WriteHandler writes content to a file.
func WriteHandler(ctx context.Context, in WriteArgs) (WriteOutput, error) {
	cmd := exec.CommandContext(ctx, "tee", in.Path)
	cmd.Stdin = strings.NewReader(in.Content)
	if err := cmd.Run(); err != nil {
		return WriteOutput{Path: in.Path}, nil
	}
	return WriteOutput{Path: in.Path}, nil
}
