package formatter

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/masterkeysrd/lspx"
	"github.com/masterkeysrd/loom/message"
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
	if s.Container != "" {
		fmt.Fprintf(&sb, "Defined in: %s\n", s.Container)
	}
	fmt.Fprintf(&sb, "Location: %s (lines %d-%d)\n\n", filepath.Base(s.FilePath), s.StartLine, s.EndLine)
	sb.WriteString(s.Snippet)

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
