package resolver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-resolver-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "internal", "foo")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to create internal/foo: %v", err)
	}

	testFilePath := filepath.Join(srcDir, "bar.go")
	testContent := "package foo\n\nfunc Hello() string {\n\treturn \"Hello World\"\n}"
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	r := New(nil, tmpDir, nil, nil)

	t.Run("resolve absolute path", func(t *testing.T) {
		path, err := r.ResolvePath(context.Background(), testFilePath)
		if err != nil {
			t.Fatalf("ResolvePath failed: %v", err)
		}
		if path != testFilePath {
			t.Errorf("expected path %s, got %s", testFilePath, path)
		}
	})

	t.Run("resolve relative path", func(t *testing.T) {
		path, err := r.ResolvePath(context.Background(), "internal/foo/bar.go")
		if err != nil {
			t.Fatalf("ResolvePath failed: %v", err)
		}
		expected := filepath.Join(tmpDir, "internal", "foo", "bar.go")
		if path != expected {
			t.Errorf("expected path %s, got %s", expected, path)
		}
	})

	t.Run("fuzzy find filename only", func(t *testing.T) {
		path, err := r.ResolvePath(context.Background(), "bar.go")
		if err != nil {
			t.Fatalf("ResolvePath failed: %v", err)
		}
		if !strings.HasSuffix(path, "bar.go") {
			t.Errorf("expected path ending with bar.go, got %s", path)
		}
	})

	t.Run("strip line range anchor", func(t *testing.T) {
		path, err := r.ResolvePath(context.Background(), testFilePath+"#L3-L4")
		if err != nil {
			t.Fatalf("ResolvePath failed: %v", err)
		}
		if path != testFilePath {
			t.Errorf("expected path %s, got %s", testFilePath, path)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := r.ResolvePath(context.Background(), "nonexistent.go")
		if err == nil {
			t.Error("expected error for nonexistent file, got nil")
		}
	})
}

func TestLoadResource(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-resolver-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "internal", "foo")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to create internal/foo: %v", err)
	}

	testFilePath := filepath.Join(srcDir, "bar.go")
	testContent := "package foo\n\nfunc Hello() string {\n\treturn \"Hello World\"\n}"
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	r := New(nil, tmpDir, nil, nil)

	t.Run("load absolute path", func(t *testing.T) {
		res, err := r.LoadResource(context.Background(), testFilePath)
		if err != nil {
			t.Fatalf("LoadResource failed: %v", err)
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

	t.Run("load non-existent absolute path", func(t *testing.T) {
		_, err := r.LoadResource(context.Background(), filepath.Join(tmpDir, "does_not_exist.go"))
		if err == nil {
			t.Error("expected error for non-existent file, got nil")
		}
	})
}

func TestResolveFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-resolver-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "internal", "foo")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to create internal/foo: %v", err)
	}

	testFilePath := filepath.Join(srcDir, "bar.go")
	testContent := "package foo\n\nfunc Hello() string {\n\treturn \"Hello World\"\n}"
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

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

func TestResolveReferences(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-resolver-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "internal", "foo")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to create internal/foo: %v", err)
	}

	testFilePath := filepath.Join(srcDir, "bar.go")
	testContent := "package foo\n\nfunc Hello() string {\n\treturn \"Hello World\"\n}"
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	r := New(nil, tmpDir, nil, nil)

	t.Run("extract and resolve manual reference", func(t *testing.T) {
		text := "Check @file:bar.go"
		resources, err := r.ResolveReferences(context.Background(), text, nil)
		if err != nil {
			t.Fatalf("ResolveReferences failed: %v", err)
		}
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}
		fileRes, ok := resources[0].(*ResolvedFile)
		if !ok {
			t.Fatalf("expected resolved resource to be *ResolvedFile")
		}
		if !strings.Contains(fileRes.Content, "func Hello() string") {
			t.Errorf("expected content to contain function body")
		}
	})

	t.Run("dedup same file referenced twice", func(t *testing.T) {
		text := "Check @file:bar.go and also @file:bar.go"
		resources, err := r.ResolveReferences(context.Background(), text, nil)
		if err != nil {
			t.Fatalf("ResolveReferences failed: %v", err)
		}
		if len(resources) != 1 {
			t.Errorf("expected 1 deduplicated resource, got %d", len(resources))
		}
	})

	t.Run("multiple different files", func(t *testing.T) {
		fooDir := filepath.Join(tmpDir, "internal", "bar")
		if err := os.MkdirAll(fooDir, 0755); err != nil {
			t.Fatalf("failed to create internal/bar: %v", err)
		}
		fooFile := filepath.Join(fooDir, "baz.go")
		if err := os.WriteFile(fooFile, []byte("package bar\n"), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		text := "Check @file:bar.go and @file:baz.go"
		resources, err := r.ResolveReferences(context.Background(), text, nil)
		if err != nil {
			t.Fatalf("ResolveReferences failed: %v", err)
		}
		if len(resources) != 2 {
			t.Errorf("expected 2 resources, got %d", len(resources))
		}
	})

	t.Run("skip unresolvable reference", func(t *testing.T) {
		text := "Check @file:nonexistent.go"
		resources, err := r.ResolveReferences(context.Background(), text, nil)
		if err != nil {
			t.Fatalf("ResolveReferences failed: %v", err)
		}
		if len(resources) != 0 {
			t.Errorf("expected 0 resources (file not found), got %d", len(resources))
		}
	})

	t.Run("skip already tracked reference", func(t *testing.T) {
		trackedRefs := []Reference{
			{Type: TypeFile, Value: testFilePath, InsertText: "@file:bar.go", FromTracker: true},
		}
		text := "Also check @file:bar.go"
		resources, err := r.ResolveReferences(context.Background(), text, trackedRefs)
		if err != nil {
			t.Fatalf("ResolveReferences failed: %v", err)
		}
		if len(resources) != 1 {
			t.Errorf("expected 1 resource (deduplicated), got %d", len(resources))
		}
	})
}
