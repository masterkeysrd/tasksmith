package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/formatter"
	"github.com/masterkeysrd/tasksmith/internal/agent/resolver"
)

func cleanPath(path string) string {
	// 1. Trim surrounding quotes
	path = strings.Trim(path, "\"'` ")

	// 2. Unescape backslashes before spaces and other common characters
	path = strings.ReplaceAll(path, "\\ ", " ")
	path = strings.ReplaceAll(path, "\\(", "(")
	path = strings.ReplaceAll(path, "\\)", ")")
	path = strings.ReplaceAll(path, "\\[", "[")
	path = strings.ReplaceAll(path, "\\]", "]")
	path = strings.ReplaceAll(path, "\\&", "&")
	path = strings.ReplaceAll(path, "\\*", "*")
	path = strings.ReplaceAll(path, "\\?", "?")
	path = strings.ReplaceAll(path, "\\|", "|")
	path = strings.ReplaceAll(path, "\\;", ";")
	path = strings.ReplaceAll(path, "\\<", "<")
	path = strings.ReplaceAll(path, "\\>", ">")
	path = strings.ReplaceAll(path, "\\'", "'")
	path = strings.ReplaceAll(path, "\\\"", "\"")

	return path
}

// View views the contents of a file by delegating to the resolver and formatter services.
func (h *ToolHandlers) View(ctx context.Context, in ViewArgs) (ViewOutput, error) {
	path := cleanPath(in.Path)

	// Format line bounds as a path hash anchor (e.g. #L10-L20) for the resolver
	targetPath := path
	if in.StartLine > 0 {
		if in.EndLine > 0 {
			targetPath = fmt.Sprintf("%s#L%d-L%d", targetPath, in.StartLine, in.EndLine)
		} else {
			targetPath = fmt.Sprintf("%s#L%d", targetPath, in.StartLine)
		}
	}

	// 1. Resolve file via the core resolver
	r := resolver.New(resolver.Config{
		Lsp:         h.LspManager,
		Cwd:         h.CWD,
		FileTracker: h.FileTracker,
		Storage:     h.Storage,
	})
	absPath, err := r.ResolvePath(ctx, targetPath, resolver.TypeFile)
	if err != nil {
		return ViewOutput{}, err
	}
	res, err := r.LoadResource(ctx, absPath, resolver.TypeFile)
	if err != nil {
		return ViewOutput{}, err
	}

	fileRes, ok := res.(*resolver.ResolvedFile)
	if !ok {
		return ViewOutput{}, fmt.Errorf("failed to type assert ResolvedFile")
	}

	// 2. Formatter diagnostics list string
	diagsStr := formatter.FormatDiagnostics(fileRes.Diagnostics)

	// 3. Map resolver output back to the expected ViewOutput structure
	return ViewOutput{
		Content:     formatter.FormatFileContent(fileRes.Content, fileRes.StartLine),
		StartLine:   fileRes.StartLine,
		EndLine:     fileRes.EndLine,
		TotalLines:  fileRes.TotalLines,
		Source:      fileRes.FilePath,
		Truncated:   fileRes.Truncated,
		MimeType:    fileRes.MimeType,
		IsBinary:    fileRes.IsBinary,
		CachedPath:  fileRes.CachedPath,
		Diagnostics: diagsStr,
	}, nil
}

// ToolContent implements the loom tool.ContentProvider interface.
func (v ViewOutput) ToolContent() message.Content {
	if v.IsBinary {
		if strings.HasPrefix(v.MimeType, "image/") {
			return message.Content{
				&message.ImageBlock{
					MIMEType: v.MimeType,
					URL:      v.Source,
				},
			}
		}
		if v.MimeType == "application/pdf" {
			return message.Content{
				&message.DocumentBlock{
					MIMEType: v.MimeType,
					URL:      v.Source,
				},
			}
		}
		return message.Content{
			&message.TextBlock{
				Text: fmt.Sprintf("[Binary document: %s (%s)]", filepath.Base(v.Source), v.MimeType),
			},
		}
	}

	filename := filepath.Base(v.Source)
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s (%d-%d of %d)\n", filename, v.StartLine, v.EndLine, v.TotalLines)
	sb.WriteString(v.Content)

	if v.Truncated {
		fmt.Fprintf(&sb, "\n[SYSTEM NOTE: File truncated at line %d due to size limits. To read further, call view_file again with start_line=%d]", v.EndLine, v.EndLine+1)
	}

	if v.Diagnostics != "" {
		sb.WriteString("\n\n")
		sb.WriteString(v.Diagnostics)
	}

	return message.Content{
		&message.TextBlock{
			Text: sb.String(),
		},
	}
}

// GetFileCacheMetadata implements the FileCacheProvider interface.
func (v ViewOutput) GetFileCacheMetadata() []FileCacheMetadata {
	return []FileCacheMetadata{
		{
			Source:     v.Source,
			CachedPath: v.CachedPath,
			MimeType:   v.MimeType,
			IsBinary:   v.IsBinary,
		},
	}
}
