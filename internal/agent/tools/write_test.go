package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteBasic(t *testing.T) {
	dir := t.TempDir()

	handlers := NewHandlers(nil, dir)

	// Test writing to a new file
	targetPath := "subdir/main.go"
	content := "package main\n\nfunc main() {}\n"
	out, err := handlers.Write(context.Background(), WriteArgs{
		Path:    targetPath,
		Content: content,
	})
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	expectedPath := "./subdir/main.go"
	if out.Path != expectedPath {
		t.Errorf("expected Path %q, got %q", expectedPath, out.Path)
	}
	if !out.Success {
		t.Error("expected Success = true")
	}
	expectedBytes := len(content)
	if out.BytesWritten != expectedBytes {
		t.Errorf("expected BytesWritten = %d, got %d", expectedBytes, out.BytesWritten)
	}

	// Verify the file was written
	absPath := filepath.Join(dir, targetPath)
	data, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected content %q, got %q", content, string(data))
	}

	// Verify TextContent
	expectedText := "Successfully wrote 29 bytes to ./subdir/main.go"
	if out.TextContent() != expectedText {
		t.Errorf("expected TextContent %q, got %q", expectedText, out.TextContent())
	}

	// Test overwriting the file
	newContent := "package main\n\n// overwritten\n"
	outOverwrite, err := handlers.Write(context.Background(), WriteArgs{
		Path:    targetPath,
		Content: newContent,
	})
	if err != nil {
		t.Fatalf("Write overwrite failed: %v", err)
	}
	if !outOverwrite.Success {
		t.Error("expected Success = true for overwrite")
	}
	if outOverwrite.BytesWritten != len(newContent) {
		t.Errorf("expected BytesWritten = %d for overwrite, got %d", len(newContent), outOverwrite.BytesWritten)
	}

	data, err = os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("failed to read overwritten file: %v", err)
	}
	if string(data) != newContent {
		t.Errorf("expected content %q, got %q", newContent, string(data))
	}
}
