package ripgrep

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAvailable(t *testing.T) {
	// Available should execute without issues.
	_ = Available()
}

func TestGlob(t *testing.T) {
	if !Available() {
		t.Skip("ripgrep (rg) not available on this system, skipping glob test")
	}

	dir := t.TempDir()

	// Write dummy files
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
