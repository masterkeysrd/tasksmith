package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobBasic(t *testing.T) {
	dir := t.TempDir()

	// Create test files
	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	writeFile(t, filepath.Join(dir, "README.md"), "# readme")
	writeFile(t, filepath.Join(dir, "config.json"), "{}")

	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(dir, "subdir", "helper.go"), "package subdir")
	writeFile(t, filepath.Join(dir, "subdir", "notes.txt"), "notes")

	// Change working directory to temp dir
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()

	handlers := NewHandlers(nil, dir)

	// Test 1: Match all Go files in current directory (non-recursive)
	out, err := handlers.Glob(context.Background(), GlobArgs{Pattern: "*.go"})
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	assertContains(t, out.Matches, "./main.go")
	assertNotContains(t, out.Matches, "./subdir/helper.go")
	assertNotContains(t, out.Matches, "./README.md")
	assertNotContains(t, out.Matches, "./config.json")
	assertNotContains(t, out.Matches, "./subdir/notes.txt")

	// Test 1b: Match all Go files recursively
	outRec, err := handlers.Glob(context.Background(), GlobArgs{Pattern: "**/*.go"})
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	assertContains(t, outRec.Matches, "./main.go")
	assertContains(t, outRec.Matches, "./subdir/helper.go")
	assertNotContains(t, outRec.Matches, "./README.md")

	// Test 2: Match with folder structure
	out2, err := handlers.Glob(context.Background(), GlobArgs{Pattern: "subdir/*.go"})
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	assertContains(t, out2.Matches, "./subdir/helper.go")
	assertNotContains(t, out2.Matches, "./main.go")

	// Test 3: Match with leading "./" in pattern
	out3, err := handlers.Glob(context.Background(), GlobArgs{Pattern: "./*.md"})
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	assertContains(t, out3.Matches, "./README.md")
	assertNotContains(t, out3.Matches, "./main.go")
}

func TestGlobIgnoreRules(t *testing.T) {
	dir := t.TempDir()

	// Setup a git repository structure
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	writeFile(t, filepath.Join(dir, ".env"), "API_KEY=secret")
	writeFile(t, filepath.Join(dir, ".gitignore"), "ignored_dir/\n*.tmp\n")

	// Create ignored files/dirs
	if err := os.Mkdir(filepath.Join(dir, "ignored_dir"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(dir, "ignored_dir", "file.go"), "package main")
	writeFile(t, filepath.Join(dir, "temp.tmp"), "temporary")

	// Create non-ignored files
	if err := os.Mkdir(filepath.Join(dir, "src"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(dir, "src", "helper.go"), "package src")

	// Change working directory to temp dir
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()

	handlers := NewHandlers(nil, dir)

	// Glob all Go files recursively - should ignore those in ignored_dir
	out, err := handlers.Glob(context.Background(), GlobArgs{Pattern: "**/*.go"})
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}

	assertContains(t, out.Matches, "./main.go")
	assertContains(t, out.Matches, "./src/helper.go")
	assertNotContains(t, out.Matches, "./ignored_dir/file.go")

	// Glob all .tmp files - should be empty since they are ignored by .gitignore
	out2, err := handlers.Glob(context.Background(), GlobArgs{Pattern: "**/*.tmp"})
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	assertNotContains(t, out2.Matches, "./temp.tmp")

	// Glob all .env files - should be empty since it is a predefined ignore
	out3, err := handlers.Glob(context.Background(), GlobArgs{Pattern: ".env"})
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	assertNotContains(t, out3.Matches, "./.env")
}

func TestGlobDoubleStarAndLimit(t *testing.T) {
	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "src", "a"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src", "b", "c"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	writeFile(t, filepath.Join(dir, "src", "main.go"), "1")
	writeFile(t, filepath.Join(dir, "src", "a", "foo.go"), "2")
	writeFile(t, filepath.Join(dir, "src", "b", "bar.go"), "3")
	writeFile(t, filepath.Join(dir, "src", "b", "c", "baz.go"), "4")
	writeFile(t, filepath.Join(dir, "src", "b", "c", "other.txt"), "5")

	// Change working directory to temp dir
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()

	handlers := NewHandlers(nil, dir)

	// Double star match: "src/**/*.go"
	out, err := handlers.Glob(context.Background(), GlobArgs{Pattern: "src/**/*.go"})
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	assertContains(t, out.Matches, "./src/main.go")
	assertContains(t, out.Matches, "./src/a/foo.go")
	assertContains(t, out.Matches, "./src/b/bar.go")
	assertContains(t, out.Matches, "./src/b/c/baz.go")
	assertNotContains(t, out.Matches, "./src/b/c/other.txt")
}

func TestGlobWithPath(t *testing.T) {
	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "src", "a"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), "1")
	writeFile(t, filepath.Join(dir, "src", "foo.go"), "2")
	writeFile(t, filepath.Join(dir, "src", "a", "bar.go"), "3")

	handlers := NewHandlers(nil, dir)

	// Search from "src" subdirectory
	out, err := handlers.Glob(context.Background(), GlobArgs{Pattern: "**/*.go", Path: "src"})
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}

	// Paths should be relative to base workspace directory
	assertContains(t, out.Matches, "./src/foo.go")
	assertContains(t, out.Matches, "./src/a/bar.go")
	assertNotContains(t, out.Matches, "./main.go")
}

func TestGlobTextContent(t *testing.T) {
	// Test empty matches
	outEmpty := GlobOutput{Matches: nil}
	if got := outEmpty.TextContent(); got != "No matches found." {
		t.Errorf("expected 'No matches found.', got %q", got)
	}

	// Test non-empty matches under limit
	out := GlobOutput{
		Matches:    []string{"./main.go", "./subdir/helper.go"},
		TotalCount: 2,
		Truncated:  false,
	}
	expected := "./main.go\n./subdir/helper.go\n\n[2 matches]"
	if got := out.TextContent(); got != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, got)
	}

	// Test truncated matches output
	outTruncated := GlobOutput{
		Matches:    []string{"./main.go"},
		TotalCount: 1500,
		Truncated:  true,
	}
	expectedTruncated := "./main.go\n\n[SYSTEM NOTE: Showing 1 of 1500 matches. Call glob again with a different pattern to narrow down results.]"
	if got := outTruncated.TextContent(); got != expectedTruncated {
		t.Errorf("expected:\n%q\ngot:\n%q", expectedTruncated, got)
	}

	// Test long filename capping
	longName := strings.Repeat("a", 130) + ".go"
	outLong := GlobOutput{
		Matches:    []string{"./subdir/" + longName},
		TotalCount: 1,
		Truncated:  false,
	}
	expectedLong := "./subdir/" + strings.Repeat("a", 128) + " ... [name truncated: 133 chars]\n\n[1 matches]"
	if got := outLong.TextContent(); got != expectedLong {
		t.Errorf("expected:\n%q\ngot:\n%q", expectedLong, got)
	}
}
