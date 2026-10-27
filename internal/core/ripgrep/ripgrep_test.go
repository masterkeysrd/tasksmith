package ripgrep

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
)

func newHostRunner(workDir string) *Runner {
	return New(func(ctx context.Context, name string, args []string, dir string) ([]byte, int, error) {
		cmdDir := workDir
		if dir != "" && dir != "." {
			if filepath.IsAbs(dir) {
				cmdDir = dir
			} else {
				cmdDir = filepath.Join(workDir, dir)
			}
		}
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = cmdDir
		out, err := cmd.Output()
		if err != nil {
			if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
				return out, exitErr.ExitCode(), nil
			}
			return nil, -1, err
		}
		return out, 0, nil
	})
}

func setupRipgrepTestTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	files := map[string]string{
		"main.go":               "package main\n\nfunc main() {}\n",
		"tools/toolgen/main.go": "package main\n\n// toolgen main\n",
		"pkg/util/util.go":      "package util\n\nconst Greeting = \"Hello World\"\n",
		"pkg/util/other.txt":    "some other text\n",
		"vendor/dep/dep.go":     "package dep\n",
		".git/config":           "[core]\n",
		".hidden.txt":           "hidden file content\n",
	}

	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll %q: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %q: %v", full, err)
		}
	}

	return dir
}

func TestRunner_IsAvailable_Mock(t *testing.T) {
	ctx := context.Background()

	// 1. Nil runner or nil exec
	var nilRunner *Runner
	if nilRunner.IsAvailable(ctx) {
		t.Errorf("expected nil runner to not be available")
	}
	emptyRunner := New(nil)
	if emptyRunner.IsAvailable(ctx) {
		t.Errorf("expected runner with nil exec to not be available")
	}

	// 2. Mock missing/failing rg
	var calls atomic.Int32
	failingRunner := New(func(ctx context.Context, name string, args []string, dir string) ([]byte, int, error) {
		calls.Add(1)
		return nil, -1, errors.New("command not found")
	})

	if failingRunner.IsAvailable(ctx) {
		t.Errorf("expected failingRunner to not be available")
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 call, got %d", calls.Load())
	}

	// Immediate second call should be cached (within 30s) without calling exec again
	if failingRunner.IsAvailable(ctx) {
		t.Errorf("expected failingRunner to still not be available")
	}
	if calls.Load() != 1 {
		t.Errorf("expected calls to remain 1 due to 30s cache, got %d", calls.Load())
	}

	// 3. Mock available rg
	var successCalls atomic.Int32
	successRunner := New(func(ctx context.Context, name string, args []string, dir string) ([]byte, int, error) {
		successCalls.Add(1)
		if name == "rg" && len(args) > 0 && args[0] == "--version" {
			return []byte("ripgrep 14.1.0\n"), 0, nil
		}
		return nil, 0, nil
	})

	if !successRunner.IsAvailable(ctx) {
		t.Errorf("expected successRunner to be available")
	}
	if successCalls.Load() != 1 {
		t.Errorf("expected 1 call, got %d", successCalls.Load())
	}

	// Second call should return true immediately without re-checking
	if !successRunner.IsAvailable(ctx) {
		t.Errorf("expected successRunner to remain available")
	}
	if successCalls.Load() != 1 {
		t.Errorf("expected successCalls to remain 1, got %d", successCalls.Load())
	}
}

func TestGlob(t *testing.T) {
	ctx := context.Background()
	dir := setupRipgrepTestTree(t)
	runner := newHostRunner(dir)

	if !runner.IsAvailable(ctx) {
		t.Skip("ripgrep (rg) binary not installed on host")
	}

	// 1. Directory-prefix matching nested files: **/toolgen* matches tools/toolgen/main.go
	matches, err := runner.Glob(ctx, "", "**/toolgen*")
	if err != nil {
		t.Fatalf("Glob(**/toolgen*) error: %v", err)
	}
	expected := []string{"tools/toolgen/main.go"}
	if !slices.Equal(matches, expected) {
		t.Errorf("Glob(**/toolgen*) = %v, want %v", matches, expected)
	}

	// 2. Ignore directory name: ignoring "vendor" ignores files under vendor/
	matches, err = runner.Glob(ctx, "", "*.go", GlobOptions{
		Ignore: []string{"vendor"},
	})
	if err != nil {
		t.Fatalf("Glob(*.go with ignore vendor) error: %v", err)
	}
	if slices.Contains(matches, "vendor/dep/dep.go") {
		t.Errorf("expected vendor/dep/dep.go to be ignored, got matches: %v", matches)
	}

	// 3. .git is ignored by default
	allFiles, err := runner.Glob(ctx, "", "")
	if err != nil {
		t.Fatalf("Glob() error: %v", err)
	}
	for _, m := range allFiles {
		if filepath.HasPrefix(m, ".git") {
			t.Errorf("expected .git files to be ignored, got %q", m)
		}
	}

	// 4. Hidden files: excluded by default, included when IncludeHidden: true
	if slices.Contains(allFiles, ".hidden.txt") {
		t.Errorf("expected .hidden.txt excluded by default")
	}
	hiddenMatches, err := runner.Glob(ctx, "", "", GlobOptions{IncludeHidden: true})
	if err != nil {
		t.Fatalf("Glob(hidden) error: %v", err)
	}
	if !slices.Contains(hiddenMatches, ".hidden.txt") {
		t.Errorf("expected .hidden.txt included with IncludeHidden=true, got %v", hiddenMatches)
	}

	// 5. No matches -> returns nil, nil
	none, err := runner.Glob(ctx, "", "nonexistent_pattern_*.xyz")
	if err != nil {
		t.Fatalf("Glob(nonexistent) error: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("expected 0 matches, got %v", none)
	}
}

func TestSearch(t *testing.T) {
	ctx := context.Background()
	dir := setupRipgrepTestTree(t)
	runner := newHostRunner(dir)

	if !runner.IsAvailable(ctx) {
		t.Skip("ripgrep (rg) binary not installed on host")
	}

	// 1. Literal search
	matches, err := runner.Search(ctx, "", "Hello World", SearchOptions{Literal: true})
	if err != nil {
		t.Fatalf("Search(Hello World) error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %+v", len(matches), matches)
	}
	m := matches[0]
	if m.Path != "pkg/util/util.go" || m.Line != 3 || m.Column != 19 || m.Content != "const Greeting = \"Hello World\"" {
		t.Errorf("unexpected match: %+v", m)
	}

	// 2. Case-insensitive search
	matches, err = runner.Search(ctx, "", "hello world", SearchOptions{
		Literal:         true,
		CaseInsensitive: true,
	})
	if err != nil {
		t.Fatalf("Search(case-insensitive) error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for case-insensitive, got %d", len(matches))
	}

	// 3. Include filter
	matches, err = runner.Search(ctx, "", "package", SearchOptions{Include: "*.txt"})
	if err != nil {
		t.Fatalf("Search(with include) error: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 matches in *.txt for 'package', got %v", matches)
	}

	// 4. No matches -> returns nil, nil
	matches, err = runner.Search(ctx, "", "ZzZzNonExistentZzZz")
	if err != nil {
		t.Fatalf("Search(nonexistent) error: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %v", matches)
	}
}
