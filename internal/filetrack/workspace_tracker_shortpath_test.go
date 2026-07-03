package filetrack

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShortestUniqueSuffix(t *testing.T) {
	tests := []struct {
		name         string
		fullPath     string
		allPaths     []string
		expectSuffix string
	}{
		{
			name:         "unique basename",
			fullPath:     "internal/app/app.go",
			allPaths:     []string{"internal/app/app.go", "internal/agent/agent.go"},
			expectSuffix: "app.go",
		},
		{
			name:         "ambiguous basename - parent dir needed",
			fullPath:     "internal/app/app.go",
			allPaths:     []string{"internal/app/app.go", "internal/agent/app.go"},
			expectSuffix: "app/app.go",
		},
		{
			name:         "ambiguous basename - grandparent dir needed",
			fullPath:     "a/b/c/file.go",
			allPaths:     []string{"a/b/c/file.go", "a/x/c/file.go", "y/c/file.go"},
			expectSuffix: "b/c/file.go",
		},
		{
			name:         "only file in workspace",
			fullPath:     "main.go",
			allPaths:     []string{"main.go"},
			expectSuffix: "main.go",
		},
		{
			name:         "deeply nested unique file",
			fullPath:     "a/b/c/d/e.go",
			allPaths:     []string{"a/b/c/d/e.go", "a/b/c/d/f.go", "a/b/c/g/e.go"},
			expectSuffix: "d/e.go",
		},
		{
			name:         "all files share same basename",
			fullPath:     "a/b/c.go",
			allPaths:     []string{"a/b/c.go", "x/y/c.go", "z/c.go"},
			expectSuffix: "b/c.go",
		},
		{
			name:         "fallback to full path when no suffix is unique",
			fullPath:     "a/b/c.go",
			allPaths:     []string{"a/b/c.go", "a/b/c.go"},
			expectSuffix: "a/b/c.go",
		},
		{
			name:         "single-level directory",
			fullPath:     "pkg/main.go",
			allPaths:     []string{"pkg/main.go", "lib/main.go"},
			expectSuffix: "pkg/main.go",
		},
		{
			name:         "file in root with same name as another in subdirectory",
			fullPath:     "config.yaml",
			allPaths:     []string{"config.yaml", "pkg/config.yaml"},
			expectSuffix: "config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShortestUniqueSuffix(tt.fullPath, tt.allPaths)
			if got != tt.expectSuffix {
				t.Errorf("ShortestUniqueSuffix(%q, %v) = %q, want %q",
					tt.fullPath, tt.allPaths, got, tt.expectSuffix)
			}
		})
	}
}

func TestShortestUniqueSuffix_EdgeCases(t *testing.T) {
	// Empty allPaths - should return full path
	got := ShortestUniqueSuffix("foo.go", []string{})
	if got != "foo.go" {
		t.Errorf("expected full path fallback for empty allPaths, got %q", got)
	}

	// Single file among many with different basenames
	allPaths := []string{
		"a/x.go",
		"b/x.go",
		"c/x.go",
		"d/unique.go",
	}
	got = ShortestUniqueSuffix("d/unique.go", allPaths)
	if got != "unique.go" {
		t.Errorf("expected unique.go, got %q", got)
	}
}

func TestWorkspaceTracker_ShortPaths(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-shortpath-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create files with overlapping basenames
	subDir1 := filepath.Join(tmpDir, "pkg1")
	subDir2 := filepath.Join(tmpDir, "pkg2")
	if err := os.MkdirAll(subDir1, 0755); err != nil {
		t.Fatalf("failed to create pkg1: %v", err)
	}
	if err := os.MkdirAll(subDir2, 0755); err != nil {
		t.Fatalf("failed to create pkg2: %v", err)
	}

	// Create files with the same basename
	if err := os.WriteFile(filepath.Join(subDir1, "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatalf("failed to write main.go in pkg1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir2, "main.go"), []byte("package main2"), 0644); err != nil {
		t.Fatalf("failed to write main.go in pkg2: %v", err)
	}
	// Unique basename
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Readme"), 0644); err != nil {
		t.Fatalf("failed to write README.md: %v", err)
	}

	wt := NewWorkspaceTracker(tmpDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := wt.Start(ctx); err != nil {
		t.Fatalf("failed to start workspace tracker: %v", err)
	}
	defer wt.Stop()

	// Give the watcher time to process
	time.Sleep(200 * time.Millisecond)

	// Search for all files
	results := wt.Search("")
	if len(results) != 5 { // 2 dirs + 3 files
		t.Fatalf("expected 5 results, got %d: %v", len(results), results)
	}

	// Find the file results
	var fileResults []SearchResult
	for _, r := range results {
		if !r.IsDir {
			fileResults = append(fileResults, r)
		}
	}

	// Both main.go files should have short paths that include their parent dir
	for _, r := range fileResults {
		if r.ShortPath == "" {
			t.Errorf("expected non-empty ShortPath for %q", r.Path)
		}
	}

	// README.md should be uniquely identified by basename alone
	var readmeResult *SearchResult
	for i := range fileResults {
		if fileResults[i].Path == "README.md" {
			readmeResult = &fileResults[i]
			break
		}
	}
	if readmeResult != nil && readmeResult.ShortPath != "README.md" {
		t.Errorf("expected README.md to have ShortPath 'README.md', got %q", readmeResult.ShortPath)
	}

	// Both main.go files should include their parent directory
	for _, r := range fileResults {
		if r.Path == "pkg1/main.go" && r.ShortPath != "pkg1/main.go" {
			t.Errorf("expected pkg1/main.go ShortPath to be 'pkg1/main.go', got %q", r.ShortPath)
		}
		if r.Path == "pkg2/main.go" && r.ShortPath != "pkg2/main.go" {
			t.Errorf("expected pkg2/main.go ShortPath to be 'pkg2/main.go', got %q", r.ShortPath)
		}
	}
}

func TestWorkspaceTracker_ShortPathOnFileCreation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-shortpath-create-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create one initial file
	if err := os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("a"), 0644); err != nil {
		t.Fatalf("failed to write a.go: %v", err)
	}

	wt := NewWorkspaceTracker(tmpDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := wt.Start(ctx); err != nil {
		t.Fatalf("failed to start workspace tracker: %v", err)
	}
	defer wt.Stop()

	time.Sleep(200 * time.Millisecond)

	// Initial search should find a.go with ShortPath "a.go"
	results := wt.Search("")
	var found bool
	for _, r := range results {
		if r.Path == "a.go" {
			found = true
			if r.ShortPath != "a.go" {
				t.Errorf("expected ShortPath 'a.go', got %q", r.ShortPath)
			}
		}
	}
	if !found {
		t.Fatal("expected to find a.go in initial search results")
	}

	// Create a new file with the same basename in a subdirectory
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create sub dir: %v", err)
	}
	newFile := filepath.Join(subDir, "a.go")
	if err := os.WriteFile(newFile, []byte("b"), 0644); err != nil {
		t.Fatalf("failed to write a.go in sub: %v", err)
	}

	// Wait for the watcher to process the new file
	time.Sleep(500 * time.Millisecond)

	// Now both a.go files should have ShortPaths that include their parent
	results = wt.Search("a.go")
	for _, r := range results {
		if r.Path == "a.go" && r.ShortPath != "a.go" {
			t.Errorf("a.go ShortPath changed unexpectedly to %q", r.ShortPath)
		}
		if r.Path == "sub/a.go" && r.ShortPath != "sub/a.go" {
			t.Errorf("sub/a.go expected ShortPath 'sub/a.go', got %q", r.ShortPath)
		}
	}
}
