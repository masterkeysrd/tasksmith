package ripgrep

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAvailable(t *testing.T) {
	_ = Available()
}

func TestGlob(t *testing.T) {
	if !Available() {
		t.Skip("ripgrep (rg) not available on this system, skipping glob test")
	}

	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# readme"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	matches, err := Glob(context.Background(), dir, "", "*.go")
	if err != nil {
		t.Fatalf("Glob failed: %v", err)
	}

	if len(matches) != 1 || matches[0] != "./main.go" {
		t.Errorf("expected [./main.go], got %v", matches)
	}
}

func TestSearch(t *testing.T) {
	if !Available() {
		t.Skip("ripgrep (rg) not available on this system, skipping search test")
	}

	dir := t.TempDir()

	content := "line 1\nsearch target here\nline 3\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	matches, err := Search(context.Background(), dir, "", "target")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}

	if matches[0].Path != "./main.go" {
		t.Errorf("expected Path='./main.go', got %q", matches[0].Path)
	}
	if matches[0].Line != 2 {
		t.Errorf("expected Line=2, got %d", matches[0].Line)
	}
	if matches[0].Content != "search target here" {
		t.Errorf("expected Content='search target here', got %q", matches[0].Content)
	}
}
