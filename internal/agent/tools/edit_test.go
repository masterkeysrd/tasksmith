package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/session/filetrack"
)

func TestEditBasic(t *testing.T) {
	dir := t.TempDir()

	filename := "main.go"
	content := strings.Join([]string{
		"package main",
		"",
		"func main() {",
		"\t// old comment",
		"}",
	}, "\n")

	writeFile(t, filepath.Join(dir, filename), content)

	handlers := NewHandlers(nil, dir)

	// Test successful edit
	out, err := handlers.Edit(context.Background(), EditArgs{
		Path:        filename,
		Target:      "\t// old comment",
		Replacement: "\t// new comment",
	})
	if err != nil {
		t.Fatalf("Edit failed: %v", err)
	}

	if !out.Success {
		t.Error("expected Success = true")
	}
	if out.Path != "./main.go" {
		t.Errorf("expected path './main.go', got %q", out.Path)
	}
	if out.Additions != 1 {
		t.Errorf("expected Additions = 1, got %d", out.Additions)
	}
	if out.Deletions != 1 {
		t.Errorf("expected Deletions = 1, got %d", out.Deletions)
	}

	// Verify file content was modified
	data, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	expectedContent := strings.Join([]string{
		"package main",
		"",
		"func main() {",
		"\t// new comment",
		"}",
	}, "\n")
	if string(data) != expectedContent {
		t.Errorf("expected content:\n%q\ngot:\n%q", expectedContent, string(data))
	}

	// Verify diff output
	if !strings.Contains(out.Diff, "-\t// old comment") {
		t.Errorf("diff missing removed line: %s", out.Diff)
	}
	if !strings.Contains(out.Diff, "+\t// new comment") {
		t.Errorf("diff missing added line: %s", out.Diff)
	}

	// Test failure when target is not found
	out, err = handlers.Edit(context.Background(), EditArgs{
		Path:        filename,
		Target:      "non-existent line",
		Replacement: "new line",
	})
	if err != nil {
		t.Fatalf("expected nil error on validation failure, got %v", err)
	}
	if out.Success {
		t.Error("expected Success = false for non-existent target")
	}
	if !strings.Contains(out.Message, "not found") {
		t.Errorf("expected message to contain 'not found', got %q", out.Message)
	}

	// Test failure when target matches multiple times and replace_all is false
	multiContent := "foo\nfoo\n"
	writeFile(t, filepath.Join(dir, "multi.go"), multiContent)
	out, err = handlers.Edit(context.Background(), EditArgs{
		Path:        "multi.go",
		Target:      "foo",
		Replacement: "bar",
		ReplaceAll:  false,
	})
	if err != nil {
		t.Fatalf("expected nil error on validation failure, got %v", err)
	}
	if out.Success {
		t.Error("expected Success = false for non-unique target")
	}
	if !strings.Contains(out.Message, "matches 2 occurrences") {
		t.Errorf("expected message to mention occurrences count, got %q", out.Message)
	}

	// Test success with ReplaceAll = true when target matches multiple times
	out, err = handlers.Edit(context.Background(), EditArgs{
		Path:        "multi.go",
		Target:      "foo",
		Replacement: "bar",
		ReplaceAll:  true,
	})
	if err != nil {
		t.Fatalf("expected nil error on replace_all, got %v", err)
	}
	if !out.Success {
		t.Error("expected Success = true when ReplaceAll = true")
	}

	multiData, err := os.ReadFile(filepath.Join(dir, "multi.go"))
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(multiData) != "bar\nbar\n" {
		t.Errorf("expected 'bar\\nbar\\n', got %q", string(multiData))
	}
}

type mockFileTracker struct {
	filetrack.FileTracker
	knownMap map[string]bool
	readMap  map[string]bool
}

func (m *mockFileTracker) IsKnown(ctx context.Context, path string) (bool, error) {
	return m.knownMap[path], nil
}

func (m *mockFileTracker) Record(ctx context.Context, change filetrack.Change, diff string, oldContent string) error {
	m.knownMap["./"+change.Path] = true
	return nil
}

func (m *mockFileTracker) RecordRead(ctx context.Context, path string) error {
	m.readMap[path] = true
	m.knownMap[path] = true
	return nil
}

func TestEditKnownValidation(t *testing.T) {
	dir := t.TempDir()

	filename := "main.go"
	content := "package main\n\nfunc main() {\n\t// old comment\n}\n"
	writeFile(t, filepath.Join(dir, filename), content)

	ft := &mockFileTracker{
		knownMap: make(map[string]bool),
		readMap:  make(map[string]bool),
	}

	handlers := NewHandlers(nil, dir).WithFileTracker(ft)
	ctx := context.Background()

	// 1. Try to edit without reading -> should fail with stale content error
	_, err := handlers.Edit(ctx, EditArgs{
		Path:        filename,
		Target:      "\t// old comment",
		Replacement: "\t// new comment",
	})
	if err == nil {
		t.Error("expected error due to unknown file content, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "modified externally") && !strings.Contains(err.Error(), "must view the file") {
		t.Errorf("unexpected error message: %v", err)
	}

	// 2. Mark as read / simulate view tool recording read
	err = ft.RecordRead(ctx, "./"+filename)
	if err != nil {
		t.Fatalf("RecordRead failed: %v", err)
	}

	// 3. Try to edit again -> should succeed
	out, err := handlers.Edit(ctx, EditArgs{
		Path:        filename,
		Target:      "\t// old comment",
		Replacement: "\t// new comment",
	})
	if err != nil {
		t.Fatalf("Edit failed: %v", err)
	}
	if !out.Success {
		t.Error("expected Success = true after file is known")
	}
}
