package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIgnorerPredefinedNames(t *testing.T) {
	dir := t.TempDir()

	ig, err := NewIgnorer(dir)
	if err != nil {
		t.Fatalf("NewIgnorer: %v", err)
	}

	ignored := []string{
		".git", "node_modules", "__pycache__", "vendor",
		"dist", "build", "target", ".next", ".DS_Store", ".venv", "venv",
	}
	for _, name := range ignored {
		if !ig.ShouldIgnore(name, filepath.Join(dir, name), true) {
			t.Errorf("expected %q to be ignored", name)
		}
	}

	visible := []string{"main.go", "README.md", "src", ".gitignore"}
	for _, name := range visible {
		if ig.ShouldIgnore(name, filepath.Join(dir, name), false) {
			t.Errorf("expected %q NOT to be ignored", name)
		}
	}
}

func TestIgnorerGitignoreRules(t *testing.T) {
	repoDir := t.TempDir()
	mustMkdir(t, filepath.Join(repoDir, ".git"))
	mustWriteFile(t, filepath.Join(repoDir, ".gitignore"), "*.log\nsecrets/\n")

	ig, err := NewIgnorer(repoDir)
	if err != nil {
		t.Fatalf("NewIgnorer: %v", err)
	}

	if !ig.ShouldIgnore("debug.log", filepath.Join(repoDir, "debug.log"), false) {
		t.Error("expected debug.log to be ignored")
	}
	if !ig.ShouldIgnore("secrets", filepath.Join(repoDir, "secrets"), true) {
		t.Error("expected secrets/ to be ignored")
	}
	if ig.ShouldIgnore("main.go", filepath.Join(repoDir, "main.go"), false) {
		t.Error("expected main.go NOT to be ignored")
	}
}

func TestIgnorerNestedGitignore(t *testing.T) {
	repoDir := t.TempDir()
	mustMkdir(t, filepath.Join(repoDir, ".git"))
	mustWriteFile(t, filepath.Join(repoDir, ".gitignore"), "")

	subDir := filepath.Join(repoDir, "src")
	mustMkdir(t, subDir)
	mustWriteFile(t, filepath.Join(subDir, ".gitignore"), "*.tmp\n")

	ig, err := NewIgnorer(subDir)
	if err != nil {
		t.Fatalf("NewIgnorer: %v", err)
	}

	if !ig.ShouldIgnore("scratch.tmp", filepath.Join(subDir, "scratch.tmp"), false) {
		t.Error("expected scratch.tmp to be ignored by nested .gitignore")
	}
	if ig.ShouldIgnore("app.go", filepath.Join(subDir, "app.go"), false) {
		t.Error("expected app.go NOT to be ignored")
	}
}

func TestIgnorerNonGitDir(t *testing.T) {
	dir := t.TempDir()

	ig, err := NewIgnorer(dir)
	if err != nil {
		t.Fatalf("NewIgnorer: %v", err)
	}

	// Only predefined rules apply; arbitrary files should not be ignored.
	if ig.ShouldIgnore("app.log", filepath.Join(dir, "app.log"), false) {
		t.Error("expected app.log NOT to be ignored outside git repo")
	}
	if !ig.ShouldIgnore("node_modules", filepath.Join(dir, "node_modules"), true) {
		t.Error("expected node_modules to be ignored even outside git repo")
	}
}

// --- helpers ---

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
