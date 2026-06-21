package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLsBasicListing(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	writeFile(t, filepath.Join(dir, "README.md"), "# readme")
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	handlers := NewHandlers(nil, "")
	out, err := handlers.Ls(context.Background(), LsArgs{Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := extractNames(out)
	assertContains(t, names, "main.go")
	assertContains(t, names, "README.md")
	assertContains(t, names, "subdir")
	if out.Truncated {
		t.Error("expected Truncated=false for a small directory")
	}
	if out.TotalCount != 3 {
		t.Errorf("expected TotalCount=3, got %d", out.TotalCount)
	}
}

func TestLsFormattedOutput(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "hello.txt"), "world")

	handlers := NewHandlers(nil, "")
	out, err := handlers.Ls(context.Background(), LsArgs{Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(out.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(out.Files))
	}
	fe, ok := out.Files[0].(FileEntry)
	if !ok {
		t.Fatalf("expected FileEntry, got %T", out.Files[0])
	}
	if fe.Name != "hello.txt" {
		t.Errorf("expected Name=hello.txt, got %q", fe.Name)
	}
	if fe.IsDir {
		t.Error("expected IsDir=false for a regular file")
	}
	if fe.IsSymlink {
		t.Error("expected IsSymlink=false for a regular file")
	}
	if fe.Size != int64(len("world")) {
		t.Errorf("expected Size=%d, got %d", len("world"), fe.Size)
	}
}

func TestLsDirectoryEntry(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "mydir"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	handlers := NewHandlers(nil, "")
	out, err := handlers.Ls(context.Background(), LsArgs{Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(out.Files) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(out.Files))
	}
	fe := out.Files[0].(FileEntry)
	if !fe.IsDir {
		t.Error("expected IsDir=true for a directory")
	}
}

func TestLsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	writeFile(t, target, "link target content")
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	handlers := NewHandlers(nil, "")
	out, err := handlers.Ls(context.Background(), LsArgs{Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := extractNames(out)
	assertContains(t, names, "link.txt")
	assertContains(t, names, "target.txt")

	for _, f := range out.Files {
		fe := f.(FileEntry)
		if fe.Name == "link.txt" {
			if !fe.IsSymlink {
				t.Error("expected IsSymlink=true for symbolic link")
			}
			if fe.LinkTarget == "" {
				t.Error("expected LinkTarget to be set for symlink")
			}
		}
	}
}

func TestLsDefaultIgnores(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{".git", "node_modules", "__pycache__", "vendor", "dist", "build", ".next", ".DS_Store", ".venv", "venv"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	writeFile(t, filepath.Join(dir, "visible.txt"), "hello")

	handlers := NewHandlers(nil, "")
	out, err := handlers.Ls(context.Background(), LsArgs{Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := extractNames(out)
	if len(names) != 1 || names[0] != "visible.txt" {
		t.Errorf("expected only [visible.txt], got %v", names)
	}
}

func TestLsGitignoreRules(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	writeFile(t, filepath.Join(repoDir, ".gitignore"), "*.log\nsecrets/\n")
	writeFile(t, filepath.Join(repoDir, "main.go"), "package main")
	writeFile(t, filepath.Join(repoDir, "debug.log"), "log data")
	if err := os.Mkdir(filepath.Join(repoDir, "secrets"), 0755); err != nil {
		t.Fatalf("mkdir secrets: %v", err)
	}

	handlers := NewHandlers(nil, "")
	out, err := handlers.Ls(context.Background(), LsArgs{Path: repoDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := extractNames(out)
	assertContains(t, names, "main.go")
	assertContains(t, names, ".gitignore")
	assertNotContains(t, names, "debug.log")
	assertNotContains(t, names, "secrets")
}

func TestLsNestedGitignore(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	writeFile(t, filepath.Join(repoDir, ".gitignore"), "")

	subDir := filepath.Join(repoDir, "src")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	writeFile(t, filepath.Join(subDir, ".gitignore"), "*.tmp\n")
	writeFile(t, filepath.Join(subDir, "app.go"), "package app")
	writeFile(t, filepath.Join(subDir, "scratch.tmp"), "temp")

	handlers := NewHandlers(nil, "")
	out, err := handlers.Ls(context.Background(), LsArgs{Path: subDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := extractNames(out)
	assertContains(t, names, "app.go")
	assertNotContains(t, names, "scratch.tmp")
}

func TestLsPatternFilter(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), "")
	writeFile(t, filepath.Join(dir, "util.go"), "")
	writeFile(t, filepath.Join(dir, "README.md"), "")
	writeFile(t, filepath.Join(dir, "Makefile"), "")

	handlers := NewHandlers(nil, "")
	out, err := handlers.Ls(context.Background(), LsArgs{Path: dir, Pattern: "*.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := extractNames(out)
	assertContains(t, names, "main.go")
	assertContains(t, names, "util.go")
	assertNotContains(t, names, "README.md")
	assertNotContains(t, names, "Makefile")
	if out.TotalCount != 2 {
		t.Errorf("expected TotalCount=2, got %d", out.TotalCount)
	}
}

func TestLsTypeFilter(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "file.go"), "")
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	handlers := NewHandlers(nil, "")

	// filter to dirs only
	outDirs, err := handlers.Ls(context.Background(), LsArgs{Path: dir, Type: "dir"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dirNames := extractNames(outDirs)
	assertContains(t, dirNames, "subdir")
	assertNotContains(t, dirNames, "file.go")

	// filter to files only
	outFiles, err := handlers.Ls(context.Background(), LsArgs{Path: dir, Type: "file"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fileNames := extractNames(outFiles)
	assertContains(t, fileNames, "file.go")
	assertNotContains(t, fileNames, "subdir")
}

func TestLsLimit(t *testing.T) {
	dir := t.TempDir()
	for i := range 10 {
		writeFile(t, filepath.Join(dir, strings.Repeat("a", i+1)+".txt"), "")
	}

	handlers := NewHandlers(nil, "")
	out, err := handlers.Ls(context.Background(), LsArgs{Path: dir, Limit: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(out.Files) != 3 {
		t.Errorf("expected 3 entries, got %d", len(out.Files))
	}
	if !out.Truncated {
		t.Error("expected Truncated=true")
	}
	if out.TotalCount != 10 {
		t.Errorf("expected TotalCount=10, got %d", out.TotalCount)
	}
}

func TestLsLongFilename(t *testing.T) {
	dir := t.TempDir()
	// 200 chars — valid on all Unix filesystems (limit 255) but exceeds MaxFilenameChars (128).
	longName := strings.Repeat("a", 200) + ".txt"
	writeFile(t, filepath.Join(dir, longName), "")

	handlers := NewHandlers(nil, "")
	out, err := handlers.Ls(context.Background(), LsArgs{Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(out.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(out.Files))
	}
	fe := out.Files[0].(FileEntry)
	if !fe.NameTruncated {
		t.Error("expected NameTruncated=true for very long filename")
	}
	if fe.Name != longName {
		t.Error("expected full name preserved in Name field despite truncation in TextContent")
	}
	text := out.TextContent()
	if strings.Contains(text, longName) {
		t.Error("expected TextContent NOT to contain the full long name")
	}
	if !strings.Contains(text, "[name truncated:") {
		t.Errorf("expected TextContent to contain truncation marker, got: %q", text)
	}
}

func TestLsTextContent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	writeFile(t, filepath.Join(dir, "util.go"), "package main")

	handlers := NewHandlers(nil, "")
	out, err := handlers.Ls(context.Background(), LsArgs{Path: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := out.TextContent()

	// Should contain the formatted lines.
	if !strings.Contains(text, "main.go") {
		t.Errorf("expected TextContent to contain 'main.go', got:\n%s", text)
	}
	if !strings.Contains(text, "util.go") {
		t.Errorf("expected TextContent to contain 'util.go', got:\n%s", text)
	}
	// Non-truncated output should end with an entry count note.
	if !strings.Contains(text, "[2 entries]") {
		t.Errorf("expected TextContent to contain '[2 entries]', got:\n%s", text)
	}
}

func TestLsTextContentTruncated(t *testing.T) {
	dir := t.TempDir()
	for i := range 5 {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("file%d.go", i)), "")
	}

	handlers := NewHandlers(nil, "")
	out, err := handlers.Ls(context.Background(), LsArgs{Path: dir, Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := out.TextContent()
	if !strings.Contains(text, "SYSTEM NOTE") {
		t.Errorf("expected truncation note in TextContent, got:\n%s", text)
	}
	if !strings.Contains(text, "2 of 5") {
		t.Errorf("expected '2 of 5' in TextContent, got:\n%s", text)
	}
}

func TestLsInvalidPath(t *testing.T) {
	handlers := NewHandlers(nil, "")
	_, err := handlers.Ls(context.Background(), LsArgs{Path: "/this/does/not/exist/at/all"})
	if err == nil {
		t.Fatal("expected error for non-existent directory, got nil")
	}
}

// --- helpers ---

func TestFormatSize(t *testing.T) {
	cases := []struct {
		input int64
		want  string
	}{
		{0, "0B"},
		{18, "18B"},
		{1023, "1023B"},
		{1024, "1.0K"},
		{1536, "1.5K"},
		{1024 * 1024, "1.0M"},
		{int64(1.5 * 1024 * 1024), "1.5M"},
		{1024 * 1024 * 1024, "1.0G"},
		{2254857830, "2.1G"}, // ~2.1 GiB		{1024 * 1024 * 1024 * 1024, "1.0T"},
	}
	for _, c := range cases {
		got := FormatSize(c.input)
		if got != c.want {
			t.Errorf("FormatSize(%d) = %q, want %q", c.input, got, c.want)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}

func extractNames(out LsOutput) []string {
	var names []string
	for _, f := range out.Files {
		if fe, ok := f.(FileEntry); ok {
			names = append(names, fe.Name)
		}
	}
	return names
}

func assertContains(t *testing.T, names []string, want string) {
	t.Helper()
	for _, n := range names {
		if n == want {
			return
		}
	}
	t.Errorf("expected %q in %v", want, names)
}

func assertNotContains(t *testing.T, names []string, unwanted string) {
	t.Helper()
	for _, n := range names {
		if n == unwanted {
			t.Errorf("expected %q NOT to be in %v", unwanted, names)
			return
		}
	}
}
