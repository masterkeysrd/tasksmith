package fsutils

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanPathEmpty(t *testing.T) {
	for _, input := range []string{"", "   ", "\t\n"} {
		_, err := CleanPath(input)
		if !errors.Is(err, ErrEmptyPath) {
			t.Errorf("CleanPath(%q) = %v, want ErrEmptyPath", input, err)
		}
	}
}

func TestCleanPathValid(t *testing.T) {
	tmp := t.TempDir()
	subDir := filepath.Join(tmp, "sub")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	symlink := filepath.Join(tmp, "sym")
	if err := os.Symlink(subDir, symlink); err != nil {
		t.Fatal(err)
	}

	cleanedSub, err := CleanPath(subDir)
	if err != nil {
		t.Fatalf("CleanPath(%q): %v", subDir, err)
	}

	cleanedSym, err := CleanPath(symlink)
	if err != nil {
		t.Fatalf("CleanPath(%q): %v", symlink, err)
	}

	if cleanedSym != cleanedSub {
		t.Errorf("CleanPath(symlink) = %q, want %q", cleanedSym, cleanedSub)
	}
}

func TestExpandAndCleanPath(t *testing.T) {
	home := "/Users/testuser"
	base := "/Users/testuser/project"

	tests := []struct {
		target   string
		baseDir  string
		homeDir  string
		expected string
	}{
		{"", base, home, base},
		{"~", base, home, home},
		{"~/", base, home, home},
		{"~/docs", base, home, "/Users/testuser/docs"},
		{"file.txt", base, home, "/Users/testuser/project/file.txt"},
		{"./sub/file.txt", base, home, "/Users/testuser/project/sub/file.txt"},
		{"../other.txt", base, home, "/Users/testuser/other.txt"},
		{"/abs/path.txt", base, home, "/abs/path.txt"},
		{"relative.txt", "", home, "relative.txt"},
		{"~/file.txt", base, "", "/Users/testuser/project/~/file.txt"},
	}

	for _, tt := range tests {
		got := ExpandAndCleanPath(tt.target, tt.baseDir, tt.homeDir)
		if got != tt.expected {
			t.Errorf("ExpandAndCleanPath(%q, %q, %q) = %q, want %q", tt.target, tt.baseDir, tt.homeDir, got, tt.expected)
		}
	}
}

func TestIsInsideDir(t *testing.T) {
	tests := []struct {
		target   string
		dir      string
		expected bool
	}{
		{"/a/b/c", "/a/b", true},
		{"/a/b", "/a/b", true},
		{"/a/b/", "/a/b", true},
		{"/a/b/c/d", "/a/b", true},
		{"/a/bc", "/a/b", false},
		{"/a", "/a/b", false},
		{"/other/path", "/a/b", false},
		{"/any/file", "/", true},
		{"/", "/", true},
	}

	for _, tt := range tests {
		got := IsInsideDir(tt.target, tt.dir)
		if got != tt.expected {
			t.Errorf("IsInsideDir(%q, %q) = %t, want %t", tt.target, tt.dir, got, tt.expected)
		}
	}
}

func TestPortablePath(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "mock", "home", "user")
	root := filepath.Join(home, "project")

	tests := []struct {
		target   string
		rootDir  string
		homeDir  string
		expected string
	}{
		{"", root, home, ""},
		{filepath.Join(root, ".agents", "skills", "loom"), root, home, filepath.Join(".agents", "skills", "loom")},
		{root, root, home, "."},
		{filepath.Join(home, ".config", "tasksmith", "skills", "custom"), root, home, filepath.Join("~", ".config", "tasksmith", "skills", "custom")},
		{filepath.Join(string(filepath.Separator), "opt", "global", "skill"), root, home, filepath.Join(string(filepath.Separator), "opt", "global", "skill")},
		{"relative/path/skill", "", "", "relative/path/skill"},
	}

	for _, tt := range tests {
		got := PortablePath(tt.target, tt.rootDir, tt.homeDir)
		if got != tt.expected {
			t.Errorf("PortablePath(%q, %q, %q) = %q, want %q", tt.target, tt.rootDir, tt.homeDir, got, tt.expected)
		}
	}
}
