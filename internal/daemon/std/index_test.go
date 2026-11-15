package std_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
)

func setupTestStdWithIndex(t *testing.T) (*std.Std, *std.FileIndex, string) {
	t.Helper()
	dir := t.TempDir()

	sb, err := sandbox.New(sandbox.Config{WorkDir: dir})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	tracker := std.NewMemoryTracker()
	logger := log.New(io.Discard, log.LevelDebug)
	s, err := std.New(std.Config{
		Sandbox: sb,
		Tracker: tracker,
		Logger:  logger,
	})
	if err != nil {
		t.Fatalf("failed to create std: %v", err)
	}

	idx, err := std.NewFileIndex(std.FileIndexOptions{
		Std:       s,
		AllowExec: false, // test pure-Go walk deterministically
	})
	if err != nil {
		t.Fatalf("failed to create file index: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })

	return s, idx, dir
}

func TestFileIndex_DiscoveryAndFiltering(t *testing.T) {
	s, idx, dir := setupTestStdWithIndex(t)
	_ = s

	// Create a variety of files
	_ = os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, ".bare"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755)

	_ = os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte("workspace"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "internal", "cli", "app.go"), []byte("package cli"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "internal", "cli", "app_test.go"), []byte("package cli"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=1"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, ".env.local"), []byte("SECRET=2"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, ".env.example"), []byte("SAMPLE=1"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".bare", "config"), []byte("conf"), 0o644)

	ctx := context.Background()
	if err := idx.Refresh(ctx); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	res, err := idx.Search(ctx, std.SearchOptions{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	foundPaths := make(map[string]bool)
	for _, f := range res {
		foundPaths[f.Path] = true
	}

	// Verify allowed files and derived directories exist
	if !foundPaths["WORKSPACE.md"] {
		t.Errorf("expected WORKSPACE.md to be indexed")
	}
	if !foundPaths["internal"] {
		t.Errorf("expected derived directory 'internal' to be indexed")
	}
	if !foundPaths["internal/cli"] {
		t.Errorf("expected derived directory 'internal/cli' to be indexed")
	}
	if !foundPaths["internal/cli/app.go"] {
		t.Errorf("expected internal/cli/app.go to be indexed")
	}
	if !foundPaths[".env.example"] {
		t.Errorf("expected .env.example template to be indexed")
	}

	// Verify excluded files
	if foundPaths[".env"] {
		t.Errorf(".env secret file must NOT be indexed")
	}
	if foundPaths[".env.local"] {
		t.Errorf(".env.local secret file must NOT be indexed")
	}
	if foundPaths[".git/HEAD"] || foundPaths[".git"] {
		t.Errorf(".git must NOT be indexed")
	}
	if foundPaths[".bare/config"] || foundPaths[".bare"] {
		t.Errorf(".bare must NOT be indexed")
	}
	if foundPaths["node_modules/pkg"] || foundPaths["node_modules"] {
		t.Errorf("node_modules must NOT be indexed")
	}
}

func TestFileIndex_SearchAndRanking(t *testing.T) {
	_, idx, dir := setupTestStdWithIndex(t)

	_ = os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "internal", "daemon"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte("workspace"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "internal", "cli", "app.go"), []byte("package cli"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "internal", "cli", "app_test.go"), []byte("package cli"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "internal", "daemon", "app.go"), []byte("package daemon"), 0o644)

	ctx := context.Background()
	_ = idx.Refresh(ctx)

	// 1. Path segment prefix match: "cli/app" should rank "internal/cli/app.go" ahead of "internal/daemon/app.go"
	res, err := idx.Search(ctx, std.SearchOptions{Query: "cli/app"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(res) == 0 {
		t.Fatalf("expected results for 'cli/app', got none")
	}
	if res[0].Path != "internal/cli/app.go" {
		t.Errorf("top match for 'cli/app' = %q, want 'internal/cli/app.go'", res[0].Path)
	}

	// 2. Non-test preference: "app" query should rank "app.go" before "app_test.go"
	resApp, err := idx.Search(ctx, std.SearchOptions{Query: "app"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	testIdx := -1
	nonTestIdx := -1
	for i, f := range resApp {
		if f.Path == "internal/cli/app.go" && nonTestIdx == -1 {
			nonTestIdx = i
		}
		if f.Path == "internal/cli/app_test.go" && testIdx == -1 {
			testIdx = i
		}
	}
	if nonTestIdx == -1 || testIdx == -1 || nonTestIdx >= testIdx {
		t.Errorf("expected non-test 'app.go' (idx %d) to rank before 'app_test.go' (idx %d)", nonTestIdx, testIdx)
	}

	// 3. Recent paths boost: when internal/daemon/app.go is recent, it gets boosted
	resRecent, err := idx.Search(ctx, std.SearchOptions{
		Query:       "app",
		RecentPaths: []string{"internal/daemon/app.go"},
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(resRecent) == 0 || resRecent[0].Path != "internal/daemon/app.go" {
		t.Errorf("expected recent file 'internal/daemon/app.go' to be top match, got %v", resRecent[0].Path)
	}
	if !resRecent[0].Recent {
		t.Errorf("expected top match to have Recent = true")
	}

	// 4. Empty query ranks Recent paths first
	resEmpty, err := idx.Search(ctx, std.SearchOptions{
		RecentPaths: []string{"internal/cli/app_test.go"},
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(resEmpty) == 0 || resEmpty[0].Path != "internal/cli/app_test.go" {
		t.Errorf("empty query expected recent path 'internal/cli/app_test.go' first, got %v", resEmpty[0].Path)
	}

	// 5. CamelCase subsequence boundary bonus: "fmr" ranks FileMentionResolver.go before flatmentionresolver.go
	_ = os.WriteFile(filepath.Join(dir, "FileMentionResolver.go"), []byte("package main"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "flatmentionresolver.go"), []byte("package main"), 0o644)
	_ = idx.Refresh(ctx)

	resCamel, err := idx.Search(ctx, std.SearchOptions{Query: "fmr"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(resCamel) < 2 || resCamel[0].Path != "FileMentionResolver.go" {
		t.Errorf("expected camelCase FileMentionResolver.go first for 'fmr', got %+v", resCamel)
	}
}

func TestFileIndex_LiveWatcherDebounce(t *testing.T) {
	dir := t.TempDir()

	sb, err := sandbox.New(sandbox.Config{WorkDir: dir})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	tracker := std.NewMemoryTracker()
	logger := log.New(io.Discard, log.LevelDebug)
	s, err := std.New(std.Config{
		Sandbox: sb,
		Tracker: tracker,
		Logger:  logger,
	})
	if err != nil {
		t.Fatalf("failed to create std: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	idx, err := std.NewFileIndex(std.FileIndexOptions{
		Std:       s,
		AllowExec: false,
		Lifecycle: ctx,
	})
	if err != nil {
		t.Fatalf("failed to create file index: %v", err)
	}
	defer func() { _ = idx.Close() }()

	// Write new file on disk
	_ = os.WriteFile(filepath.Join(dir, "dynamic.txt"), []byte("dynamic content"), 0o644)

	// Wait for debounce timer (~150ms + jitter) to pick up the event
	deadline := time.Now().Add(3 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		res, err := idx.Search(ctx, std.SearchOptions{Query: "dynamic.txt"})
		if err == nil && len(res) > 0 && res[0].Path == "dynamic.txt" {
			found = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !found {
		t.Fatalf("file index did not automatically pick up dynamically created file")
	}
}

func TestFileIndex_ShortPath(t *testing.T) {
	_, idx, dir := setupTestStdWithIndex(t)

	// Create structure with unique files, colliding files at depth 2 and depth 4, and colliding dirs
	_ = os.MkdirAll(filepath.Join(dir, "internal", "cli", "components"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "internal", "ui", "components"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "internal", "tui"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "docs"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "a", "x", "y"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "b", "x", "y"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "c", "z", "y"), 0o755)

	_ = os.WriteFile(filepath.Join(dir, "internal", "cli", "components", "composer.go"), []byte("package components"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "internal", "ui", "components", "view.go"), []byte("package components"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "internal", "cli", "app.go"), []byte("package cli"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "internal", "tui", "app.go"), []byte("package tui"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "docs", "readme.md"), []byte("# Docs"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "a", "x", "y", "file.txt"), []byte("a"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b", "x", "y", "file.txt"), []byte("b"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "c", "z", "y", "file.txt"), []byte("c"), 0o644)

	ctx := context.Background()
	if err := idx.Refresh(ctx); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	res, err := idx.Search(ctx, std.SearchOptions{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	byPath := make(map[string]string)
	for _, f := range res {
		byPath[f.Path] = f.ShortPath
	}

	expected := map[string]string{
		// Unique basename
		"internal/cli/components/composer.go": "composer.go",
		"docs/readme.md":                      "readme.md",

		// Colliding basenames at depth 1, resolving at depth 2
		"internal/cli/app.go": "cli/app.go",
		"internal/tui/app.go": "tui/app.go",

		// Colliding at depths 1, 2, 3
		"c/z/y/file.txt": "z/y/file.txt",
		"a/x/y/file.txt": "a/x/y/file.txt",
		"b/x/y/file.txt": "b/x/y/file.txt",

		// Unique directory
		"docs": "docs/",

		// Colliding directories
		"internal/cli/components": "cli/components/",
		"internal/ui/components":  "ui/components/",
	}

	for path, wantShort := range expected {
		gotShort, ok := byPath[path]
		if !ok {
			t.Errorf("path %q not found in index results", path)
			continue
		}
		if gotShort != wantShort {
			t.Errorf("ShortPath for %q = %q, want %q", path, gotShort, wantShort)
		}
	}
}
