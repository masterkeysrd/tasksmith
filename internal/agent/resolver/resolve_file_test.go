package resolver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveFile(t *testing.T) {
	// Create a temp workspace directory
	tmpDir, err := os.MkdirTemp("", "tasksmith-resolver-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file structure
	srcDir := filepath.Join(tmpDir, "internal", "foo")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to create internal/foo: %v", err)
	}

	testFilePath := filepath.Join(srcDir, "bar.go")
	testContent := "package foo\n\nfunc Hello() string {\n\treturn \"Hello World\"\n}"
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Create resolver (with nil lsp/tracker/storage for testing)
	r := New(nil, tmpDir, nil, nil)

	t.Run("resolve absolute path", func(t *testing.T) {
		res, err := r.ResolveFile(context.Background(), testFilePath)
		if err != nil {
			t.Fatalf("ResolveFile failed: %v", err)
		}
		if res.Type() != TypeFile {
			t.Errorf("expected TypeFile, got %v", res.Type())
		}
		if res.Handle() != "bar.go" {
			t.Errorf("expected Handle 'bar.go', got %v", res.Handle())
		}
		fileRes, ok := res.(*ResolvedFile)
		if !ok {
			t.Fatalf("expected resolved resource to be *ResolvedFile")
		}
		if !strings.Contains(fileRes.Content, "func Hello() string") {
			t.Errorf("expected content to contain function body, got: %s", fileRes.Content)
		}
	})

	t.Run("resolve relative path", func(t *testing.T) {
		res, err := r.ResolveFile(context.Background(), "internal/foo/bar.go")
		if err != nil {
			t.Fatalf("ResolveFile failed: %v", err)
		}
		if res.Handle() != "bar.go" {
			t.Errorf("expected Handle 'bar.go', got %v", res.Handle())
		}
	})

	t.Run("fuzzy find filename only", func(t *testing.T) {
		res, err := r.ResolveFile(context.Background(), "bar.go")
		if err != nil {
			t.Fatalf("ResolveFile failed: %v", err)
		}
		if res.Handle() != "bar.go" {
			t.Errorf("expected Handle 'bar.go', got %v", res.Handle())
		}
	})

	t.Run("resolve line range anchor", func(t *testing.T) {
		res, err := r.ResolveFile(context.Background(), testFilePath+"#L3-L4")
		if err != nil {
			t.Fatalf("ResolveFile failed: %v", err)
		}
		fileRes, ok := res.(*ResolvedFile)
		if !ok {
			t.Fatalf("expected resolved resource to be *ResolvedFile")
		}
		if fileRes.StartLine != 3 || fileRes.EndLine != 4 {
			t.Errorf("expected line range 3-4, got %d-%d", fileRes.StartLine, fileRes.EndLine)
		}
		if !strings.Contains(fileRes.Content, "func Hello() string") {
			t.Errorf("content missing range snippet: %s", fileRes.Content)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := r.ResolveFile(context.Background(), "nonexistent.go")
		if err == nil {
			t.Error("expected error for nonexistent file, got nil")
		}
	})
}
