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
