package api

import (
	"fmt"

	"github.com/masterkeysrd/loom/message"
)

// TextContent returns the rendered instructions for the loaded skill to provide to the LLM.
func (o LoadSkillOutput) TextContent() string {
	if o.Path != "" {
		return fmt.Sprintf("Base directory: %s\n\n%s", o.Path, o.Instructions)
	}
	return o.Instructions
}

// ToolContent renders the matched tool names as tool-reference blocks so the
// matched deferred tools are loaded into the model's active toolset. When no
// tools match, it returns a single text block explaining that.
func (o ToolSearchOutput) ToolContent() message.Content {
	if len(o.Tools) == 0 {
		return message.Content{&message.TextBlock{Text: "No matching tools found."}}
	}
	content := make(message.Content, 0, len(o.Tools))
	for _, name := range o.Tools {
		content = append(content, &message.ToolReferenceBlock{ToolName: name})
	}
	return content
}
