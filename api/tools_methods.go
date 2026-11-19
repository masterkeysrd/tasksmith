package api

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/loom/message"
)

// TextContent returns the rendered instructions for the loaded skill to provide to the LLM.
func (o LoadSkillOutput) TextContent() string {
	if o.Path != "" {
		return fmt.Sprintf("Base directory: %s\n\n%s", o.Path, o.Instructions)
	}
	return o.Instructions
}

// TextContent returns the rendered instructions for the loaded rule to provide to the LLM.
func (o LoadRuleOutput) TextContent() string {
	if o.Path != "" {
		return fmt.Sprintf("Rule path: %s\n\n%s", o.Path, o.Instructions)
	}
	return o.Instructions
}

// TextContent renders web search results as a numbered list so the model reads
// titles, URLs, and snippets instead of a raw JSON blob.
func (o WebSearchOutput) TextContent() string {
	if len(o.Results) == 0 {
		return "No search results found."
	}
	var sb strings.Builder
	for i, res := range o.Results {
		fmt.Fprintf(&sb, "%d. %s\n   URL: %s\n", i+1, res.Title, res.URL)
		if res.Snippet != "" {
			fmt.Fprintf(&sb, "   %s\n", res.Snippet)
		}
		sb.WriteString("\n")
	}
	return strings.TrimSuffix(sb.String(), "\n\n")
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

// ToolContent returns the viewed content as a text block plus a lightweight
// media descriptor block when the file is supported multimodal media (and was
// not omitted for the active model). Plain text views (MimeType == "") return
// nil so StructuredContent remains the sole payload without duplicating text.
func (o ViewOutput) ToolContent() message.Content {
	if o.MimeType == "" {
		return nil
	}
	content := message.Content{&message.TextBlock{Text: o.Content}}
	if o.Omitted || o.Hash == "" || o.ByteSize <= 0 {
		return content
	}
	extras := map[string]any{
		MediaExtraSHA256:      o.Hash,
		MediaExtraByteSize:    o.ByteSize,
		MediaExtraDisplayName: filepath.Base(o.Path),
	}
	if o.Pages != "" {
		extras[MediaExtraPages] = o.Pages
	}
	switch o.Modality {
	case "image":
		content = append(content, &message.ImageBlock{MIMEType: o.MimeType, Extras: extras})
	case "pdf":
		content = append(content, &message.DocumentBlock{MIMEType: o.MimeType, Extras: extras})
	case "audio":
		content = append(content, &message.AudioBlock{MIMEType: o.MimeType, Extras: extras})
	}
	return content
}
