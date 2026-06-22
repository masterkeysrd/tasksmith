package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/fs"
)

const (
	MaxTotalChars = 16000
	MaxLineChars  = 500
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

func normalizeSpacing(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r == ' ' || r == '\u202f' || r == '\u00a0' || (r >= '\u2000' && r <= '\u200a') {
			sb.WriteRune(' ')
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// View views the contents of a file.
func (h *ToolHandlers) View(ctx context.Context, in ViewArgs) (ViewOutput, error) {
	path := cleanPath(in.Path)

	// If path doesn't exist, try to find a matching file in the same directory by normalizing spacing
	if _, err := os.Stat(path); os.IsNotExist(err) {
		dir := filepath.Dir(path)
		base := filepath.Base(path)

		normalizedBase := normalizeSpacing(base)
		normalizedBaseLower := strings.ToLower(normalizedBase)

		files, readErr := os.ReadDir(dir)
		if readErr == nil {
			var bestMatch string
			for _, f := range files {
				if !f.IsDir() {
					normalizedName := normalizeSpacing(f.Name())
					if normalizedName == normalizedBase {
						bestMatch = filepath.Join(dir, f.Name())
						break // Exact casing matches are preferred
					}
					if strings.ToLower(normalizedName) == normalizedBaseLower {
						bestMatch = filepath.Join(dir, f.Name())
					}
				}
			}
			if bestMatch != "" {
				path = bestMatch
			}
		}
	}

	mimeType := fs.DetectMIMEType(path)
	isBinary := fs.IsBinaryMIME(mimeType)
	filename := filepath.Base(path)

	var cachedPath string
	if isBinary {
		if h.Storage != nil {
			file, err := os.Open(path)
			if err != nil {
				return ViewOutput{}, fmt.Errorf("failed to open binary file %s: %w", path, err)
			}
			defer file.Close()

			toolCallID, _ := ctx.Value("tool_call_id").(string)
			if toolCallID == "" {
				toolCallID = "unknown"
			}
			storagePath := fmt.Sprintf("%s_%s", toolCallID, filename)

			var errSave error
			cachedPath, errSave = h.Storage.Save(ctx, storagePath, file)
			if errSave != nil {
				return ViewOutput{}, fmt.Errorf("failed to cache binary file: %w", errSave)
			}
		}

		return ViewOutput{
			Source:     path,
			CachedPath: cachedPath,
			MimeType:   mimeType,
			IsBinary:   true,
		}, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return ViewOutput{}, fmt.Errorf("failed to open file %s: %w", path, err)
	}
	defer file.Close()

	startLine := max(in.StartLine, 1)
	endLine := in.EndLine

	var lines []string
	reader := bufio.NewReader(file)
	currentLine := 0
	totalChars := 0
	truncated := false
	lastAppendedLine := 0

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			currentLine++
			if !truncated && currentLine >= startLine && (endLine == 0 || currentLine <= endLine) {
				trimmedLine := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
				charCount := len(trimmedLine)

				var contentToAppend string
				if charCount > MaxLineChars {
					contentToAppend = trimmedLine[:MaxLineChars] + fmt.Sprintf(" ... [Line %d truncated: %d characters of minified/dense data]", currentLine, charCount)
				} else {
					contentToAppend = trimmedLine
				}

				formattedLine := fmt.Sprintf("%d | %s", currentLine, contentToAppend)

				lineLength := len(formattedLine)
				if len(lines) > 0 {
					lineLength += 1 // for the "\n" separator
				}

				if totalChars+lineLength > MaxTotalChars {
					truncated = true
				} else {
					lines = append(lines, formattedLine)
					totalChars += lineLength
					lastAppendedLine = currentLine
				}
			}
		}
		if err != nil {
			break
		}
	}

	content := strings.Join(lines, "\n")

	var actualStartLine, actualEndLine int
	if lastAppendedLine > 0 {
		actualStartLine = startLine
		actualEndLine = lastAppendedLine
	}

	return ViewOutput{
		Content:    content,
		StartLine:  actualStartLine,
		EndLine:    actualEndLine,
		TotalLines: currentLine,
		Source:     in.Path,
		Truncated:  truncated,
		MimeType:   mimeType,
		IsBinary:   false,
	}, nil
}

// ToolContent implements the loom tool.ContentProvider interface.
func (v ViewOutput) ToolContent() message.Content {
	if v.IsBinary {
		if strings.HasPrefix(v.MimeType, "image/") {
			return message.Content{
				&message.ImageBlock{
					MIMEType: v.MimeType,
					// Data is left as nil to prevent DB/checkpoint bloat.
					// It will be dynamically populated (re-hydrated) when calling the LLM.
				},
			}
		}

		if v.MimeType == "application/pdf" {
			return message.Content{
				&message.DocumentBlock{
					MIMEType: v.MimeType,
					// Data is left as nil to prevent DB/checkpoint bloat.
					// It will be dynamically populated (re-hydrated) when calling the LLM.
				},
			}
		}

		// Fallback for other documents or unsupported binaries
		return message.Content{
			&message.TextBlock{
				Text: fmt.Sprintf("[Binary document: %s (%s)]", filepath.Base(v.Source), v.MimeType),
			},
		}
	}

	if v.TotalLines == 0 {
		return message.Content{
			&message.TextBlock{
				Text: v.Content,
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
