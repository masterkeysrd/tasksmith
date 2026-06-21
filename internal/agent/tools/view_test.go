package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	handlers := NewHandlers(nil, "")

	t.Run("read small file to EOF", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "small.txt")
		var sb strings.Builder
		for i := 1; i <= 50; i++ {
			sb.WriteString(fmt.Sprintf("Line %d\n", i))
		}
		if err := os.WriteFile(filePath, []byte(sb.String()), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		out, err := handlers.View(ctx, ViewArgs{Path: filePath})
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

		out, err := handlers.View(ctx, ViewArgs{Path: filePath})
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

		out, err := handlers.View(ctx, ViewArgs{Path: filePath})
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
		_, err := handlers.View(ctx, ViewArgs{Path: "does-not-exist.txt"})
		if err == nil {
			t.Fatalf("expected handler error for missing file, got nil")
		}
		if !strings.Contains(err.Error(), "no such file or directory") {
			t.Errorf("expected error string to contain 'no such file or directory', got: %q", err.Error())
		}
	})
}

type mockStorage struct {
	savedPath string
	savedData []byte
}

func (m *mockStorage) Save(ctx context.Context, relativePath string, r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	m.savedData = data
	m.savedPath = "/mock/dest/" + relativePath
	return m.savedPath, nil
}

func (m *mockStorage) Get(ctx context.Context, relativePath string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.savedData)), nil
}

func TestViewBinary(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-view-binary-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.WithValue(context.Background(), "tool_call_id", "call-test-1")
	storage := &mockStorage{}
	handlers := NewHandlers(storage, "")

	filePath := filepath.Join(tmpDir, "image.png")
	dummyBytes := []byte("fake-png-bytes")
	if err := os.WriteFile(filePath, dummyBytes, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	out, err := handlers.View(ctx, ViewArgs{Path: filePath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !out.IsBinary {
		t.Errorf("expected IsBinary to be true")
	}
	if out.MimeType != "image/png" {
		t.Errorf("expected MimeType to be image/png, got %q", out.MimeType)
	}
	expectedSavedPath := "/mock/dest/call-test-1_image.png"
	if out.CachedPath != expectedSavedPath {
		t.Errorf("expected CachedPath to be %q, got %q", expectedSavedPath, out.CachedPath)
	}
	if !bytes.Equal(storage.savedData, dummyBytes) {
		t.Errorf("expected saved data to match dummy bytes")
	}

	// Since we mock and the cached path "/mock/dest/call-test-1_image.png" doesn't exist on disk,
	// ToolContent() will try to read from out.Path (fallback) and return ImageBlock successfully.
	toolContent := out.ToolContent()
	if len(toolContent) != 1 {
		t.Fatalf("expected 1 block, got %d", len(toolContent))
	}
	imageBlock, ok := toolContent[0].(*message.ImageBlock)
	if !ok {
		t.Fatalf("expected ImageBlock, got %T", toolContent[0])
	}
	if imageBlock.Data != nil {
		t.Errorf("expected image data to be nil, got %q", string(imageBlock.Data))
	}
	if imageBlock.MIMEType != "image/png" {
		t.Errorf("expected image MIMEType to be image/png, got %q", imageBlock.MIMEType)
	}
}

func TestViewPathCleaningAndSpacing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-view-spacing-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	handlers := NewHandlers(nil, "")

	// 1. Create a file on disk with spaces and a narrow non-breaking space U+202F
	// E.g. "Screenshot 2026-06-20 at 10.13.29[NNBSP]PM.png"
	fileNameOnDisk := "Screenshot 2026-06-20 at 10.13.29\u202fPM.png"
	filePathOnDisk := filepath.Join(tmpDir, fileNameOnDisk)
	dummyBytes := []byte("fake-image-data")
	if err := os.WriteFile(filePathOnDisk, dummyBytes, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Case A: Path has backslash-escaped spaces (using standard spaces)
	// E.g. "Screenshot\ 2026-06-20\ at\ 10.13.29\ PM.png"
	escapedPathInput := filepath.Join(tmpDir, "Screenshot\\ 2026-06-20\\ at\\ 10.13.29\\ PM.png")
	out, err := handlers.View(ctx, ViewArgs{Path: escapedPathInput})
	if err != nil {
		t.Fatalf("Case A failed: unexpected error viewing file: %v", err)
	}
	if !out.IsBinary {
		t.Errorf("Case A: expected IsBinary to be true")
	}
	if filepath.Base(out.Path) != fileNameOnDisk {
		t.Errorf("Case A: expected resolved path to be %q, got %q", fileNameOnDisk, filepath.Base(out.Path))
	}

	// Case B: Path is quoted with surrounding double quotes (using standard spaces)
	quotedPathInput := `"` + filepath.Join(tmpDir, "Screenshot 2026-06-20 at 10.13.29 PM.png") + `"`
	out, err = handlers.View(ctx, ViewArgs{Path: quotedPathInput})
	if err != nil {
		t.Fatalf("Case B failed: unexpected error viewing file: %v", err)
	}
	if !out.IsBinary {
		t.Errorf("Case B: expected IsBinary to be true")
	}
	if filepath.Base(out.Path) != fileNameOnDisk {
		t.Errorf("Case B: expected resolved path to be %q, got %q", fileNameOnDisk, filepath.Base(out.Path))
	}
}
