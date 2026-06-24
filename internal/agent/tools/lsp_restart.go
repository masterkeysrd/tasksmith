package tools

import (
	"context"
	"fmt"

	"github.com/masterkeysrd/loom/message"
)

// LspRestart restarts LSP server.
func (h *ToolHandlers) LspRestart(ctx context.Context, in LspRestartArgs) (LspRestartOutput, error) {
	if h.LspManager == nil {
		return LspRestartOutput{Message: "LSP manager is not initialized", Success: false}, nil
	}
	err := h.LspManager.RestartClient(ctx, h.CWD)
	if err != nil {
		return LspRestartOutput{Message: err.Error(), Success: false}, nil
	}
	return LspRestartOutput{Message: fmt.Sprintf("Restarted LSP client for workspace root: %s", h.CWD), Success: true}, nil
}

// TextContent implements the loom tool.TextContentProvider interface.
func (o LspRestartOutput) TextContent() string {
	if o.Success {
		return fmt.Sprintf("Success: %s", o.Message)
	}
	return fmt.Sprintf("Failure: %s", o.Message)
}

// ToolContent implements the loom tool.ContentProvider interface.
func (o LspRestartOutput) ToolContent() message.Content {
	return message.Content{
		&message.TextBlock{
			Text: o.TextContent(),
		},
	}
}
