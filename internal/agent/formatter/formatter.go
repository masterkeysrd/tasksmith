package formatter

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/lspx"
	"github.com/masterkeysrd/tasksmith/internal/agent/resolver"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
)

// FormatResource converts any resolver.ResolvedResource into the appropriate Loom blocks.
func FormatResource(res resolver.ResolvedResource) []message.Block {
	if res == nil {
		return nil
	}

	switch val := res.(type) {
	case *resolver.ResolvedFile:
		return FormatFile(val)
	case *resolver.ResolvedSymbol:
		return FormatSymbol(val)
	case *resolver.ResolvedSkill:
		return FormatSkill(val)
	}

	return nil
}

// FormatFile formats a ResolvedFile structure into text blocks, image blocks, or document blocks.
func FormatFile(f *resolver.ResolvedFile) []message.Block {
	if f.IsDir {
		return []message.Block{
			&message.TextBlock{
				Text: fmt.Sprintf("[Directory: %s]", filepath.Base(f.FilePath)),
			},
		}
	}
	diagsStr := FormatDiagnostics(f.Diagnostics)
	return FormatFileBlocks(f.FilePath, f.Content, f.StartLine, f.EndLine, f.TotalLines, f.Truncated, f.MimeType, f.IsBinary, f.CachedPath, diagsStr)
}

// FormatFileBlocks converts raw file parameters into Loom blocks. Exported for reuse by the view tool.
func FormatFileBlocks(filePath string, content string, startLine, endLine, totalLines int, truncated bool, mimeType string, isBinary bool, cachedPath string, diagnostics string) []message.Block {
	if isBinary {
		if strings.HasPrefix(mimeType, "image/") {
			return []message.Block{
				&message.ImageBlock{
					MIMEType: mimeType,
					URL:      filePath,
				},
			}
		}
		if mimeType == "application/pdf" {
			return []message.Block{
				&message.DocumentBlock{
					MIMEType: mimeType,
					URL:      filePath,
				},
			}
		}
		return []message.Block{
			&message.TextBlock{
				Text: fmt.Sprintf("[Binary document: %s (%s)]", filepath.Base(filePath), mimeType),
			},
		}
	}

	// 1. Add row line numbering to the content
	numberedContent := FormatFileContent(content, startLine)

	filename := filepath.Base(filePath)
	var sb strings.Builder

	if startLine <= 0 {
		startLine = 1
	}
	fmt.Fprintf(&sb, "%s (%d-%d of %d)\n", filename, startLine, endLine, totalLines)
	sb.WriteString(numberedContent)

	if truncated {
		fmt.Fprintf(&sb, "\n[SYSTEM NOTE: File truncated at line %d due to size limits. To read further, call view_file again with start_line=%d]", endLine, endLine+1)
	}

	if diagnostics != "" {
		sb.WriteString("\n\n")
		sb.WriteString(diagnostics)
	}

	return []message.Block{
		&message.TextBlock{
			Text: sb.String(),
		},
	}
}

// FormatFileContent processes text content, applying row line numbers starting from startLine.
func FormatFileContent(truncatedContent string, startLine int) string {
	if startLine <= 0 {
		startLine = 1
	}
	if len(truncatedContent) == 0 {
		return ""
	}

	lines := strings.Split(truncatedContent, "\n")
	var formattedLines []string
	currentLine := startLine

	for _, line := range lines {
		formattedLines = append(formattedLines, fmt.Sprintf("%d | %s", currentLine, line))
		currentLine++
	}

	return strings.Join(formattedLines, "\n")
}

// FormatDiagnostics formats a list of raw LSP diagnostics into a clean markdown diagnostic list.
func FormatDiagnostics(diags []lsp.Diagnostic) string {
	if len(diags) == 0 {
		return ""
	}

	// Sort diagnostics by priority: error -> warning -> line number
	sort.Slice(diags, func(i, j int) bool {
		sevI := getSeverityValue(diags[i].Severity)
		sevJ := getSeverityValue(diags[j].Severity)
		if sevI != sevJ {
			return sevI < sevJ
		}
		return diags[i].Range.Start.Line < diags[j].Range.Start.Line
	})

	var sb strings.Builder
	sb.WriteString("Active Diagnostics:")
	limit := 10
	totalCount := len(diags)

	for i, d := range diags {
		if i >= limit {
			break
		}
		var severity string
		if d.Severity != nil {
			switch *d.Severity {
			case 1:
				severity = "error"
			case 2:
				severity = "warning"
			case 3:
				severity = "info"
			case 4:
				severity = "hint"
			}
		}

		var msg string
		if d.Message.String != nil {
			msg = *d.Message.String
		} else if d.Message.MarkupContent != nil {
			msg = d.Message.MarkupContent.Value
		}

		fmt.Fprintf(&sb, "\n- [%s] line %d: %s", severity, d.Range.Start.Line+1, msg)
	}

	if totalCount > limit {
		fmt.Fprintf(&sb, "\n*(Plus %d more diagnostics)*\n", totalCount-limit)
	}

	return sb.String()
}

func getSeverityValue(sev *lspx.DiagnosticSeverity) int {
	if sev == nil {
		return 99
	}
	return int(*sev)
}

// FormatSymbol formats a ResolvedSymbol into a TextBlock.
func FormatSymbol(s *resolver.ResolvedSymbol) []message.Block {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Symbol: %s (%s)\n", s.Name, s.Kind)
	if s.Signature != "" {
		fmt.Fprintf(&sb, "Signature: %s\n", s.Signature)
	}
	if s.TypeDefinedAt != "" {
		fmt.Fprintf(&sb, "Type Defined at: %s\n", s.TypeDefinedAt)
	}
	if s.Container != "" {
		fmt.Fprintf(&sb, "Defined in: %s\n", s.Container)
	}
	fmt.Fprintf(&sb, "Location: %s (lines %d-%d)\n\n", filepath.Base(s.FilePath), s.StartLine, s.EndLine)
	sb.WriteString(s.Snippet)

	if s.Docs != "" {
		sb.WriteString("\n\n")
		sb.WriteString(s.Docs)
		if s.DocsTruncated {
			sb.WriteString("\n\n[Truncated — full report available at: `")
			sb.WriteString(s.FullReportPath)
			sb.WriteString("`]")
		}
		sb.WriteString("\n\n")
	}

	if len(s.References) > 0 {
		sb.WriteString("**References** (")
		fmt.Fprintf(&sb, "%d total", s.ReferencesTotal)
		sb.WriteString("):\n")
		for _, ref := range s.References {
			fmt.Fprintf(&sb, "- `%s`\n", ref)
		}
		sb.WriteString("\n")
	}

	if len(s.Implementations) > 0 {
		sb.WriteString("**Implementations** (")
		fmt.Fprintf(&sb, "%d total", s.ImplementationsTotal)
		sb.WriteString("):\n")
		for _, impl := range s.Implementations {
			fmt.Fprintf(&sb, "- `%s`\n", impl)
		}
		sb.WriteString("\n")
	}

	if len(s.Diagnostics) > 0 {
		diagsStr := FormatDiagnostics(s.Diagnostics)
		if diagsStr != "" {
			sb.WriteString("\n\n")
			sb.WriteString(diagsStr)
		}
	}

	return []message.Block{
		&message.TextBlock{
			Text: sb.String(),
		},
	}
}

// FormatSkill formats a ResolvedSkill into a TextBlock.
func FormatSkill(sk *resolver.ResolvedSkill) []message.Block {
	return []message.Block{
		&message.TextBlock{
			Text: fmt.Sprintf("# Skill: %s\n\n%s", sk.Name, sk.Instructions),
		},
	}
}

// FormatAttachmentsBlock wraps all resolved resources into a single <attachments> XML block.
func FormatAttachmentsBlock(resources []resolver.ResolvedResource, r *resolver.Resolver) string {
	if len(resources) == 0 {
		return ""
	}

	hasSkill := false
	for _, res := range resources {
		if _, ok := res.(*resolver.ResolvedSkill); ok {
			hasSkill = true
			break
		}
	}

	reminder := "These attachments were loaded by the user. Use them to complete the request."
	if hasSkill {
		reminder += " Consider the attached skills as already active/activated; you do not need to call activate_skill for them."
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "<system_reminder>%s</system_reminder>\n", reminder)
	sb.WriteString("<attachments>\n")

	for _, res := range resources {
		if r.ShouldEmbed(res) {
			sb.WriteString(formatEmbedded(res))
		} else {
			sb.WriteString(formatReferenced(res))
		}
	}

	sb.WriteString("</attachments>")
	return sb.String()
}

// formatEmbedded formats a resource that is small enough to be embedded in full.
func formatEmbedded(res resolver.ResolvedResource) string {
	switch val := res.(type) {
	case *resolver.ResolvedFile:
		return formatEmbeddedFile(val)
	case *resolver.ResolvedSymbol:
		return formatEmbeddedSymbol(val)
	case *resolver.ResolvedSkill:
		return formatEmbeddedSkill(val)
	}
	return ""
}

// formatEmbedded formats a ResolvedFile with content and optional diagnostics.
func formatEmbeddedFile(f *resolver.ResolvedFile) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<file path=\"%s\" lines=\"%d\">\n", escapeXML(f.FilePath), f.TotalLines)
	sb.WriteString("<content>\n")
	sb.WriteString(escapeXML(FormatFileContent(f.Content, f.StartLine)))
	sb.WriteString("\n</content>\n")

	if len(f.Diagnostics) > 0 {
		sb.WriteString("<diagnostics>\n")
		for _, d := range f.Diagnostics {
			severity := "unknown"
			if d.Severity != nil {
				switch *d.Severity {
				case lspx.DiagnosticSeverityError:
					severity = "error"
				case lspx.DiagnosticSeverityWarning:
					severity = "warning"
				case lspx.DiagnosticSeverityInformation:
					severity = "info"
				case lspx.DiagnosticSeverityHint:
					severity = "hint"
				}
			}
			msg := ""
			if d.Message.String != nil {
				msg = *d.Message.String
			} else if d.Message.MarkupContent != nil {
				msg = d.Message.MarkupContent.Value
			}
			fmt.Fprintf(&sb, "- [%s] line %d: %s\n", severity, d.Range.Start.Line+1, escapeXML(msg))
		}
		sb.WriteString("</diagnostics>\n")
	}

	sb.WriteString("</file>")
	return sb.String()
}

// formatEmbedded formats a ResolvedSymbol with content and optional diagnostics.
func formatEmbeddedSymbol(s *resolver.ResolvedSymbol) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<symbol name=\"%s\" kind=\"%s\" file=\"%s\" lines=\"%d-%d\">\n",
		escapeXML(s.Name), escapeXML(s.Kind), escapeXML(filepath.Base(s.FilePath)), s.StartLine, s.EndLine)
	sb.WriteString("<content>\n")
	sb.WriteString(escapeXML(s.Snippet))
	sb.WriteString("\n</content>\n")

	if s.TypeDefinedAt != "" {
		fmt.Fprintf(&sb, "<type_defined_at>%s</type_defined_at>\n", escapeXML(s.TypeDefinedAt))
	}

	if s.Docs != "" {
		sb.WriteString("<docs>\n")
		sb.WriteString(escapeXML(s.Docs))
		sb.WriteString("\n</docs>\n")
		if s.DocsTruncated {
			fmt.Fprintf(&sb, "<docs_truncated>true</docs_truncated>\n")
			fmt.Fprintf(&sb, "<full_report_path>%s</full_report_path>\n", escapeXML(s.FullReportPath))
		}
	}

	if len(s.References) > 0 {
		sb.WriteString("<references>\n")
		for _, ref := range s.References {
			fmt.Fprintf(&sb, "<reference>%s</reference>\n", escapeXML(ref))
		}
		sb.WriteString("</references>\n")
		fmt.Fprintf(&sb, "<references_total>%d</references_total>\n", s.ReferencesTotal)
	}

	if len(s.Implementations) > 0 {
		sb.WriteString("<implementations>\n")
		for _, impl := range s.Implementations {
			fmt.Fprintf(&sb, "<implementation>%s</implementation>\n", escapeXML(impl))
		}
		sb.WriteString("</implementations>\n")
		fmt.Fprintf(&sb, "<implementations_total>%d</implementations_total>\n", s.ImplementationsTotal)
	}

	if len(s.Diagnostics) > 0 {
		sb.WriteString("<diagnostics>\n")
		for _, d := range s.Diagnostics {
			severity := "unknown"
			if d.Severity != nil {
				switch *d.Severity {
				case 1:
					severity = "error"
				case 2:
					severity = "warning"
				case 3:
					severity = "info"
				case 4:
					severity = "hint"
				}
			}
			msg := ""
			if d.Message.String != nil {
				msg = *d.Message.String
			} else if d.Message.MarkupContent != nil {
				msg = d.Message.MarkupContent.Value
			}
			fmt.Fprintf(&sb, "- [%s] line %d: %s\n", severity, d.Range.Start.Line+1, escapeXML(msg))
		}
		sb.WriteString("</diagnostics>\n")
	}

	sb.WriteString("</symbol>")
	return sb.String()
}

// formatEmbedded formats a ResolvedSkill with its instructions content.
func formatEmbeddedSkill(sk *resolver.ResolvedSkill) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<skill name=\"%s\">\n", escapeXML(sk.Name))
	sb.WriteString("<content>\n")
	sb.WriteString(escapeXML(sk.Instructions))
	sb.WriteString("\n</content>\n")
	sb.WriteString("</skill>")
	return sb.String()
}

// formatReferenced formats a resource that is too large or binary as a self-closing tag.
func formatReferenced(res resolver.ResolvedResource) string {
	switch val := res.(type) {
	case *resolver.ResolvedFile:
		return formatReferencedFile(val)
	case *resolver.ResolvedSymbol:
		return formatReferencedSymbol(val)
	}
	return ""
}

func formatReferencedSymbol(s *resolver.ResolvedSymbol) string {
	reason := "too large to embed"
	if len(s.Snippet) == 0 {
		reason = "snippet empty or unavailable"
	}
	return fmt.Sprintf("<symbol name=\"%s\" kind=\"%s\" file=\"%s\" lines=\"%d-%d\" reason=\"%s\" />",
		escapeXML(s.Name), escapeXML(s.Kind), escapeXML(filepath.Base(s.FilePath)), s.StartLine, s.EndLine, escapeXML(reason))
}

// formatReferencedFile formats a ResolvedFile as a self-closing tag with reason.
func formatReferencedFile(f *resolver.ResolvedFile) string {
	if f.IsDir {
		return fmt.Sprintf("<file path=\"%s\" is_dir=\"true\" reason=\"directory, use ls to list contents\" />\n", escapeXML(f.FilePath))
	}
	if f.IsBinary {
		mime := f.MimeType
		if mime == "" {
			mime = "application/octet-stream"
		}
		return fmt.Sprintf("<file path=\"%s\" mime=\"%s\" reason=\"binary file, use view_file\" />\n", escapeXML(f.FilePath), escapeXML(mime))
	}
	return fmt.Sprintf("<file path=\"%s\" lines=\"%d\" reason=\"file too large, use view_file to read\" />\n", escapeXML(f.FilePath), f.TotalLines)
}

// escapeXML escapes special XML characters in the given string.
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
