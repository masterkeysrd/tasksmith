package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/loom/message"
)

// ProcessMcpOutput post-processes the output of an MCP tool, truncating it if it is too long
// and saving the full contents to cache storage.
func ProcessMcpOutput(ctx context.Context, tc *message.ToolCall, toolMsg *message.Tool, storage FileStorage) *message.Tool {
	if toolMsg == nil || toolMsg.IsError || storage == nil {
		return toolMsg
	}

	if !strings.HasPrefix(tc.Name, "mcp__") {
		return toolMsg
	}

	var fullTextBuilder strings.Builder
	for _, block := range toolMsg.Content {
		if tb, ok := block.(*message.TextBlock); ok {
			fullTextBuilder.WriteString(tb.Text)
		}
	}
	fullText := fullTextBuilder.String()
	const maxMcpTextLength = 8000

	if len(fullText) <= maxMcpTextLength {
		return toolMsg
	}

	var truncatedContent message.Content
	accumulated := 0
	for _, block := range toolMsg.Content {
		if tb, ok := block.(*message.TextBlock); ok {
			if accumulated >= maxMcpTextLength {
				continue
			}
			remaining := maxMcpTextLength - accumulated
			if len(tb.Text) > remaining {
				truncatedContent = append(truncatedContent, &message.TextBlock{
					Text: tb.Text[:remaining],
				})
				accumulated += remaining
			} else {
				truncatedContent = append(truncatedContent, &message.TextBlock{
					Text: tb.Text,
				})
				accumulated += len(tb.Text)
			}
		} else {
			truncatedContent = append(truncatedContent, block)
		}
	}

	filename := fmt.Sprintf("%s_mcp_output.txt", tc.ID)
	cachedPath, errSave := storage.Save(ctx, filename, strings.NewReader(fullText))
	if errSave == nil {
		note := fmt.Sprintf("\n\n[SYSTEM NOTE: MCP tool output was too long and was truncated. The complete output is saved at: %s. You can view the full file using 'view' or search it using 'grep'.]", cachedPath)
		truncatedContent = append(truncatedContent, &message.TextBlock{
			Text: note,
		})
		toolMsg.Content = truncatedContent

		meta := toolMsg.GetMetadata()
		if meta == nil {
			meta = make(map[string]any)
		}
		meta["truncated"] = true
		meta["full_content_path"] = cachedPath
		toolMsg.SetMetadata(meta)
	}

	return toolMsg
}
