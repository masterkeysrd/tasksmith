package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/loom/message"
)

const (
	MaxTotalChars = 16000
	MaxLineChars  = 500
)

// ViewHandler views the contents of a file.
func ViewHandler(ctx context.Context, in ViewArgs) (ViewOutput, error) {
	file, err := os.Open(in.Path)
	if err != nil {
		return ViewOutput{}, fmt.Errorf("failed to open file %s: %w", in.Path, err)
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
		Path:       in.Path,
		Truncated:  truncated,
	}, nil
}

// ToolContent implements the loom tool.ContentProvider interface.
func (v ViewOutput) ToolContent() message.Content {
	if v.TotalLines == 0 {
		return message.Content{
			&message.TextBlock{
				Text: v.Content,
			},
		}
	}

	filename := filepath.Base(v.Path)
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
