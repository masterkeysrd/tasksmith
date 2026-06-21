package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
)

func TestViewHandler(t *testing.T) {
	// Create a temp directory for tests
	tmpDir, err := os.MkdirTemp("", "tasksmith-view-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	t.Run("read small file to EOF", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "small.txt")
		var sb strings.Builder
		for i := 1; i <= 50; i++ {
			sb.WriteString(fmt.Sprintf("Line %d\n", i))
		}
		if err := os.WriteFile(filePath, []byte(sb.String()), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		out, err := ViewHandler(ctx, ViewArgs{Path: filePath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		lines := strings.Split(out.Content, "\n")
		if len(lines) != 50 {
			t.Errorf("expected 50 lines, got %d", len(lines))
		}
		if lines[0] != "1 | Line 1" {
			t.Errorf("expected line 1 to be '1 | Line 1', got %q", lines[0])
		}
		if lines[49] != "50 | Line 50" {
			t.Errorf("expected line 50 to be '50 | Line 50', got %q", lines[49])
		}
		if out.TotalLines != 50 {
			t.Errorf("expected TotalLines to be 50, got %d", out.TotalLines)
		}
		if out.EndLine != 50 {
			t.Errorf("expected EndLine to be 50, got %d", out.EndLine)
		}
		if out.Truncated {
			t.Errorf("expected Truncated to be false, got true")
		}
	})

	t.Run("horizontal safety valve for dense lines", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "dense.txt")
		longLine := strings.Repeat("A", 600)
		content := fmt.Sprintf("Short line 1\n%s\nShort line 3\n", longLine)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		out, err := ViewHandler(ctx, ViewArgs{Path: filePath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		lines := strings.Split(out.Content, "\n")
		if len(lines) != 3 {
			t.Fatalf("expected 3 lines, got %d", len(lines))
		}
		if lines[0] != "1 | Short line 1" {
			t.Errorf("expected line 1, got %q", lines[0])
		}
		expectedOmitted := fmt.Sprintf("2 | %s ... [Line 2 truncated: 600 characters of minified/dense data]", strings.Repeat("A", 500))
		if lines[1] != expectedOmitted {
			t.Errorf("expected line 2 to show inline safety valve, got %q", lines[1])
		}
		if lines[2] != "3 | Short line 3" {
			t.Errorf("expected line 3, got %q", lines[2])
		}
	})

	t.Run("vertical truncation budget of 16000 chars", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "large.txt")
		var sb strings.Builder
		for i := 1; i <= 1000; i++ {
			sb.WriteString(fmt.Sprintf("Line %d: %s\n", i, strings.Repeat("x", 20)))
		}
		if err := os.WriteFile(filePath, []byte(sb.String()), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		out, err := ViewHandler(ctx, ViewArgs{Path: filePath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !out.Truncated {
			t.Fatalf("expected Truncated to be true")
		}

		// Ensure content itself does not contain the system note
		if strings.Contains(out.Content, "SYSTEM NOTE: File truncated") {
			t.Errorf("expected raw content to not contain system note")
		}

		// Call ToolContent() and verify the note is present there
		toolContent := out.ToolContent()
		if len(toolContent) != 1 {
			t.Fatalf("expected 1 tool content block")
		}
		textBlock := toolContent[0].(*message.TextBlock)
		if !strings.Contains(textBlock.Text, "SYSTEM NOTE: File truncated at line") {
			t.Errorf("expected ToolContent text to contain system note, got: %q", textBlock.Text)
		}

		parts := strings.Split(textBlock.Text, "\n[SYSTEM NOTE:")
		if len(parts) != 2 {
			t.Fatalf("expected exactly one truncation notice in ToolContent")
		}

		expectedNotePrefix := fmt.Sprintf(" File truncated at line %d due to size limits. To read further, call view_file again with start_line=%d]", out.EndLine, out.EndLine+1)
		if !strings.HasPrefix(parts[1], expectedNotePrefix) {
			t.Errorf("expected note to match suffix, got %q", parts[1])
		}

		if out.TotalLines != 1000 {
			t.Errorf("expected TotalLines to be 1000, got %d", out.TotalLines)
		}
	})

	t.Run("missing file handles gracefully", func(t *testing.T) {
		_, err := ViewHandler(ctx, ViewArgs{Path: "does-not-exist.txt"})
		if err == nil {
			t.Fatalf("expected handler error for missing file, got nil")
		}
		if !strings.Contains(err.Error(), "no such file or directory") {
			t.Errorf("expected error string to contain 'no such file or directory', got: %q", err.Error())
		}
	})
}
