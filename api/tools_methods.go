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

// ToolContent renders the loaded skill's instructions as a text block followed by
// tool-reference blocks for each deferred skill tool, so the deferred tools are
// activated into the model's active toolset when the skill is loaded. It keeps
// the rendered instructions intact (also exposed via TextContent) for callers
// that inspect the text directly.
func (o LoadSkillOutput) ToolContent() message.Content {
	content := make(message.Content, 0, len(o.Tools)+1)
	content = append(content, &message.TextBlock{Text: o.TextContent()})
	for _, name := range o.Tools {
		content = append(content, &message.ToolReferenceBlock{ToolName: name})
	}
	return content
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
