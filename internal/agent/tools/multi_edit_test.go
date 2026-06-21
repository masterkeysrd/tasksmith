package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultiEdit(t *testing.T) {
	dir := t.TempDir()
	handlers := NewHandlers(nil, dir)

	filename := "test.txt"
	initialContent := "line 1\nline 2\nline 3\n"
	writeFile(t, filepath.Join(dir, filename), initialContent)

	// Case 1: All edits succeed
	out, err := handlers.MultiEdit(context.Background(), MultiEditArgs{
		Path: filename,
		Edits: []MultiEditArgsEditsItem{
			{Target: "line 1", Replacement: "line one"},
			{Target: "line 3", Replacement: "line three"},
		},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !out.Success {
		t.Error("expected Success = true when edits succeed")
	}

	// Verify file was written
	data, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	expected := "line one\nline 2\nline three\n"
	if string(data) != expected {
		t.Errorf("expected %q, got %q", expected, string(data))
	}

	// Verify results list
	if len(out.Results) != 2 || !out.Results[0].Success || !out.Results[1].Success {
		t.Errorf("expected 2 successful results, got %v", out.Results)
	}

	// Case 2: Partial edits success
	// We restore initial content
	writeFile(t, filepath.Join(dir, filename), initialContent)
	out, err = handlers.MultiEdit(context.Background(), MultiEditArgs{
		Path: filename,
		Edits: []MultiEditArgsEditsItem{
			{Target: "line 2", Replacement: "line two"},
			{Target: "non-existent", Replacement: "new"},
		},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !out.Success {
		t.Error("expected Success = true since at least one edit succeeded")
	}

	// Verify file was written with the successful change
	data, err = os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	expected = "line 1\nline two\nline 3\n"
	if string(data) != expected {
		t.Errorf("expected %q, got %q", expected, string(data))
	}

	// Verify results statuses
	if len(out.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(out.Results))
	}
	if !out.Results[0].Success {
		t.Error("expected edit 1 to succeed")
	}
	if out.Results[1].Success {
		t.Error("expected edit 2 to fail")
	}
	if !strings.Contains(out.Results[1].Message, "not found") {
		t.Errorf("expected message to mention not found, got %q", out.Results[1].Message)
	}

	// Verify TextContent warnings output
	text := out.TextContent()
	if !strings.Contains(text, "Warnings / Failed Edits") {
		t.Errorf("expected warnings section in TextContent, got:\n%s", text)
	}
	if !strings.Contains(text, "Edit #2 failed: target block not found") {
		t.Errorf("expected Edit #2 warning message, got:\n%s", text)
	}
}
