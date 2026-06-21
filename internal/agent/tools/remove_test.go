package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveFile(t *testing.T) {
	dir := t.TempDir()
	handlers := NewHandlers(nil, dir)

	// Setup file to remove
	filename := "file.txt"
	absPath := filepath.Join(dir, filename)
	if err := os.WriteFile(absPath, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to setup file: %v", err)
	}

	// Remove it
	out, err := handlers.Remove(context.Background(), RemoveArgs{
		Path: filename,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !out.Success {
		t.Error("expected Success = true")
	}
	expectedPath := "./file.txt"
	if out.Path != expectedPath {
		t.Errorf("expected Path %q, got %q", expectedPath, out.Path)
	}
	if out.Content != "test content" {
		t.Errorf("expected Content %q, got %q", "test content", out.Content)
	}

	// Verify file is gone
	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Error("expected file to be removed, but it still exists")
	}

	// Verify TextContent
	expectedText := "Successfully removed ./file.txt"
	if out.TextContent() != expectedText {
		t.Errorf("expected TextContent %q, got %q", expectedText, out.TextContent())
	}
}

func TestRemoveDir(t *testing.T) {
	dir := t.TempDir()
	handlers := NewHandlers(nil, dir)

	// Setup dir structure to remove
	subDirName := "subdir"
	subDirPath := filepath.Join(dir, subDirName)
	if err := os.MkdirAll(subDirPath, 0755); err != nil {
		t.Fatalf("failed to setup subdir: %v", err)
	}
	filePath := filepath.Join(subDirPath, "file.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to setup file: %v", err)
	}

	// 1. Attempt to remove subdir without recursive flag (should fail)
	out, err := handlers.Remove(context.Background(), RemoveArgs{
		Path:      subDirName,
		Recursive: false,
	})
	if err == nil {
		t.Error("expected error when removing a directory without recursive=true, got nil")
	} else if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("expected error message to contain 'is a directory', got %q", err.Error())
	}
	if out.Success {
		t.Error("expected Success = false when recursive=false")
	}

	// 2. Remove subdir with recursive flag (should succeed)
	out, err = handlers.Remove(context.Background(), RemoveArgs{
		Path:      subDirName,
		Recursive: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !out.Success {
		t.Error("expected Success = true when recursive=true")
	}
	expectedPath := "./subdir"
	if out.Path != expectedPath {
		t.Errorf("expected Path %q, got %q", expectedPath, out.Path)
	}

	// Verify subdir and nested file are gone
	if _, err := os.Stat(subDirPath); !os.IsNotExist(err) {
		t.Error("expected subdir to be removed, but it still exists")
	}
}

func TestRemoveNonExistent(t *testing.T) {
	dir := t.TempDir()
	handlers := NewHandlers(nil, dir)

	out, err := handlers.Remove(context.Background(), RemoveArgs{
		Path: "does-not-exist.txt",
	})
	if err == nil {
		t.Error("expected error for non-existent path, got nil")
	} else if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected error message to contain 'does not exist', got %q", err.Error())
	}

	if out.Success {
		t.Error("expected Success = false")
	}
	if out.Path != "does-not-exist.txt" {
		t.Errorf("expected Path 'does-not-exist.txt', got %q", out.Path)
	}

	// Verify TextContent
	expectedText := "Failed to remove does-not-exist.txt"
	if out.TextContent() != expectedText {
		t.Errorf("expected TextContent %q, got %q", expectedText, out.TextContent())
	}
}

func TestRemoveBinaryFile(t *testing.T) {
	dir := t.TempDir()
	handlers := NewHandlers(nil, dir)

	filename := "test.png"
	absPath := filepath.Join(dir, filename)
	pngBytes := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	if err := os.WriteFile(absPath, pngBytes, 0644); err != nil {
		t.Fatalf("failed to setup file: %v", err)
	}

	out, err := handlers.Remove(context.Background(), RemoveArgs{
		Path: filename,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !out.Success {
		t.Error("expected Success = true")
	}
	if !out.IsBinary {
		t.Error("expected IsBinary to be true")
	}
	if out.MimeType != "image/png" {
		t.Errorf("expected MimeType to be image/png, got %q", out.MimeType)
	}
	if out.Content != "" {
		t.Errorf("expected Content to be empty for binary file, got %q", out.Content)
	}

	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Error("expected file to be removed")
	}
}
