package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
)

const (
	MaxLspDiagnosticsItems             = 100
	MaxLspFileDiagnosticsItems         = 50
	MaxLspTopWorkspaceDiagnosticsItems = 10
)

// LspDiagnostics gets LSP diagnostics.
func (h *ToolHandlers) LspDiagnostics(ctx context.Context, in LspDiagnosticsArgs) (LspDiagnosticsOutput, error) {
	if h.LspManager == nil {
		return LspDiagnosticsOutput{}, fmt.Errorf("LSP manager is not initialized")
	}
	client, err := h.LspManager.GetClient(ctx, h.CWD)
	if err != nil {
		return LspDiagnosticsOutput{}, fmt.Errorf("failed to get LSP client: %w", err)
	}

	targetPath := in.Path
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(h.CWD, targetPath)
	}

	diags, err := client.GetDiagnostics(ctx, targetPath)
	if err != nil {
		return LspDiagnosticsOutput{}, err
	}

	outputDiags := make([]LspDiagnosticsOutputDiagnosticsItem, len(diags))
	for i, d := range diags {
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

		relPath, err := filepath.Rel(h.CWD, d.Path)
		if err != nil {
			relPath = d.Path
		}

		var msg string
		if d.Message.String != nil {
			msg = *d.Message.String
		} else if d.Message.MarkupContent != nil {
			msg = d.Message.MarkupContent.Value
		}

		outputDiags[i] = LspDiagnosticsOutputDiagnosticsItem{
			Path:     relPath,
			Message:  msg,
			Severity: severity,
			Range: LspDiagnosticsOutputDiagnosticsItemRange{
				Start: LspDiagnosticsOutputDiagnosticsItemRangeStart{
					Line:      int(d.Range.Start.Line),
					Character: int(d.Range.Start.Character),
				},
				End: LspDiagnosticsOutputDiagnosticsItemRangeEnd{
					Line:      int(d.Range.End.Line),
					Character: int(d.Range.End.Character),
				},
			},
		}
	}

	// Sort diagnostics by priority: error -> warning -> info -> hint
	sort.Slice(outputDiags, func(i, j int) bool {
		sevI := getSeverityValue(outputDiags[i].Severity)
		sevJ := getSeverityValue(outputDiags[j].Severity)
		if sevI != sevJ {
			return sevI < sevJ
		}
		if outputDiags[i].Path != outputDiags[j].Path {
			return outputDiags[i].Path < outputDiags[j].Path
		}
		if outputDiags[i].Range.Start.Line != outputDiags[j].Range.Start.Line {
			return outputDiags[i].Range.Start.Line < outputDiags[j].Range.Start.Line
		}
		return outputDiags[i].Range.Start.Character < outputDiags[j].Range.Start.Character
	})

	totalCount := len(outputDiags)
	truncated := false
	if totalCount > MaxLspDiagnosticsItems {
		outputDiags = outputDiags[:MaxLspDiagnosticsItems]
		truncated = true
	}

	return LspDiagnosticsOutput{
		Diagnostics: outputDiags,
		TotalCount:  totalCount,
		Truncated:   truncated,
	}, nil
}

func getSeverityValue(sev string) int {
	switch strings.ToLower(sev) {
	case "error":
		return 0
	case "warning":
		return 1
	case "info":
		return 2
	case "hint":
		return 3
	default:
		return 4
	}
}

// TextContent implements the loom tool.TextContentProvider interface.
func (o LspDiagnosticsOutput) TextContent() string {
	if len(o.Diagnostics) == 0 {
		return "No diagnostics found."
	}

	var sb strings.Builder

	for _, d := range o.Diagnostics {
		severityStr := "UNKNOWN"
		if d.Severity != "" {
			severityStr = strings.ToUpper(d.Severity)
		}
		sb.WriteString(fmt.Sprintf("- [%s] %s:%d:%d - %s\n",
			severityStr,
			d.Path,
			d.Range.Start.Line+1,
			d.Range.Start.Character+1,
			d.Message,
		))
	}

	if o.Truncated {
		fmt.Fprintf(&sb, "\n[SYSTEM NOTE: Diagnostics truncated in handler to conserve tokens. Showing first %d of %d diagnostics. Specify a single file path in the tool arguments to target diagnostics for a specific file.]",
			len(o.Diagnostics),
			o.TotalCount,
		)
	}
	return sb.String()
}

// ToolContent implements the loom tool.ContentProvider interface.
func (o LspDiagnosticsOutput) ToolContent() message.Content {
	return message.Content{
		&message.TextBlock{
			Text: o.TextContent(),
		},
	}
}

// GetTopWorkspaceDiagnosticsString fetches the workspace diagnostics, filters for errors and warnings,
// limits to the top 10, and returns a formatted string. It returns an empty string if there are no errors or warnings.
func GetTopWorkspaceDiagnosticsString(ctx context.Context, lspManager *lsp.Manager, cwd string) string {
	if lspManager == nil {
		return ""
	}
	client, err := lspManager.GetClient(ctx, cwd)
	if err != nil || client == nil {
		return ""
	}

	diags, err := client.GetDiagnostics(ctx, cwd)
	if err != nil || len(diags) == 0 {
		return ""
	}

	var outputDiags []LspDiagnosticsOutputDiagnosticsItem
	for _, d := range diags {
		var severity string
		if d.Severity != nil {
			switch *d.Severity {
			case 1:
				severity = "error"
			case 2:
				severity = "warning"
			default:
				continue // skip info/hints for the top-10 summary
			}
		} else {
			continue // skip unknown severity
		}

		relPath, err := filepath.Rel(cwd, d.Path)
		if err != nil {
			relPath = d.Path
		}

		var msg string
		if d.Message.String != nil {
			msg = *d.Message.String
		} else if d.Message.MarkupContent != nil {
			msg = d.Message.MarkupContent.Value
		}

		outputDiags = append(outputDiags, LspDiagnosticsOutputDiagnosticsItem{
			Path:     relPath,
			Message:  msg,
			Severity: severity,
			Range: LspDiagnosticsOutputDiagnosticsItemRange{
				Start: LspDiagnosticsOutputDiagnosticsItemRangeStart{
					Line:      int(d.Range.Start.Line),
					Character: int(d.Range.Start.Character),
				},
				End: LspDiagnosticsOutputDiagnosticsItemRangeEnd{
					Line:      int(d.Range.End.Line),
					Character: int(d.Range.End.Character),
				},
			},
		})
	}

	if len(outputDiags) == 0 {
		return ""
	}

	// Sort diagnostics by priority: error -> warning -> path -> line
	sort.Slice(outputDiags, func(i, j int) bool {
		sevI := getSeverityValue(outputDiags[i].Severity)
		sevJ := getSeverityValue(outputDiags[j].Severity)
		if sevI != sevJ {
			return sevI < sevJ
		}
		if outputDiags[i].Path != outputDiags[j].Path {
			return outputDiags[i].Path < outputDiags[j].Path
		}
		if outputDiags[i].Range.Start.Line != outputDiags[j].Range.Start.Line {
			return outputDiags[i].Range.Start.Line < outputDiags[j].Range.Start.Line
		}
		return outputDiags[i].Range.Start.Character < outputDiags[j].Range.Start.Character
	})

	var sb strings.Builder
	sb.WriteString("Workspace Diagnostics Overview:\n")

	limit := MaxLspTopWorkspaceDiagnosticsItems
	if len(outputDiags) < limit {
		limit = len(outputDiags)
	}

	for i := 0; i < limit; i++ {
		d := outputDiags[i]
		severityStr := strings.ToUpper(d.Severity)
		sb.WriteString(fmt.Sprintf("- [%s] %s:%d:%d - %s\n",
			severityStr,
			d.Path,
			d.Range.Start.Line+1,
			d.Range.Start.Character+1,
			d.Message,
		))
	}

	if len(outputDiags) > limit {
		sb.WriteString(fmt.Sprintf("\n*(Plus %d more diagnostics. Use the `LspDiagnostics` tool to view them all.)*\n", len(outputDiags)-limit))
	}

	return sb.String()
}

// GetFileDiagnosticsString fetches diagnostics for a single file and returns a formatted string.
func GetFileDiagnosticsString(ctx context.Context, lspManager *lsp.Manager, cwd string, filePath string) string {
	if lspManager == nil {
		return ""
	}
	client, err := lspManager.GetClient(ctx, cwd)
	if err != nil || client == nil {
		return ""
	}

	diags, err := client.GetDiagnostics(ctx, filePath)
	if err != nil || len(diags) == 0 {
		return ""
	}

	var outputDiags []LspDiagnosticsOutputDiagnosticsItem
	for _, d := range diags {
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

		outputDiags = append(outputDiags, LspDiagnosticsOutputDiagnosticsItem{
			Path:     filepath.Base(d.Path),
			Message:  msg,
			Severity: severity,
			Range: LspDiagnosticsOutputDiagnosticsItemRange{
				Start: LspDiagnosticsOutputDiagnosticsItemRangeStart{
					Line:      int(d.Range.Start.Line),
					Character: int(d.Range.Start.Character),
				},
				End: LspDiagnosticsOutputDiagnosticsItemRangeEnd{
					Line:      int(d.Range.End.Line),
					Character: int(d.Range.End.Character),
				},
			},
		})
	}

	if len(outputDiags) == 0 {
		return ""
	}

	// Sort diagnostics by priority: error -> warning -> line
	sort.Slice(outputDiags, func(i, j int) bool {
		sevI := getSeverityValue(outputDiags[i].Severity)
		sevJ := getSeverityValue(outputDiags[j].Severity)
		if sevI != sevJ {
			return sevI < sevJ
		}
		if outputDiags[i].Range.Start.Line != outputDiags[j].Range.Start.Line {
			return outputDiags[i].Range.Start.Line < outputDiags[j].Range.Start.Line
		}
		return outputDiags[i].Range.Start.Character < outputDiags[j].Range.Start.Character
	})

	totalCount := len(outputDiags)
	if totalCount > MaxLspFileDiagnosticsItems {
		outputDiags = outputDiags[:MaxLspFileDiagnosticsItems]
	}

	var sb strings.Builder
	sb.WriteString("LSP Diagnostics:\n")

	for _, d := range outputDiags {
		severityStr := "UNKNOWN"
		if d.Severity != "" {
			severityStr = strings.ToUpper(d.Severity)
		}
		sb.WriteString(fmt.Sprintf("- [%s] Line %d:%d - %s\n",
			severityStr,
			d.Range.Start.Line+1,
			d.Range.Start.Character+1,
			d.Message,
		))
	}

	if totalCount > MaxLspFileDiagnosticsItems {
		sb.WriteString(fmt.Sprintf("\n*(Plus %d more diagnostics omitted for brevity)*\n", totalCount-MaxLspFileDiagnosticsItems))
	}

	return sb.String()
}
