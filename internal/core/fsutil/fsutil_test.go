package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDir(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "fsutil-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	t.Run("CreateNewDir", func(t *testing.T) {
		path := filepath.Join(tempDir, "new-dir")
		if err := EnsureDir(path); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("failed to stat created dir: %v", err)
		}
		if !info.IsDir() {
			t.Errorf("expected path to be a directory")
		}
	})

	t.Run("ExistingDir", func(t *testing.T) {
		path := filepath.Join(tempDir, "existing-dir")
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatalf("failed to create existing dir: %v", err)
		}

		if err := EnsureDir(path); err != nil {
			t.Errorf("expected no error for existing dir, got %v", err)
		}
	})

	t.Run("PathIsFile", func(t *testing.T) {
		path := filepath.Join(tempDir, "file.txt")
		if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}

		err := EnsureDir(path)
		if err == nil {
			t.Errorf("expected error when path is a file, got nil")
		}
	})

	t.Run("NestedDir", func(t *testing.T) {
		path := filepath.Join(tempDir, "nested/a/b/c")
		if err := EnsureDir(path); err != nil {
			t.Errorf("expected no error for nested dir creation, got %v", err)
		}

		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("failed to stat nested dir: %v", err)
		}
		if !info.IsDir() {
			t.Errorf("expected path to be a directory")
		}
	})
}
