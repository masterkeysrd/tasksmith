package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrepBasic(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {\n\t// search target here\n}")
	writeFile(t, filepath.Join(dir, "README.md"), "# readme\nnot matching")

	handlers := NewHandlers(nil, dir)

	out, err := handlers.Grep(context.Background(), GrepArgs{Path: "main.go", Pattern: "target"})
	if err != nil {
		t.Fatalf("grep failed: %v", err)
	}

	if len(out.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(out.Matches))
	}

	match, ok := out.Matches[0].(GrepMatch)
	if !ok {
		t.Fatalf("expected GrepMatch, got %T", out.Matches[0])
	}

	if match.Path != "./main.go" {
		t.Errorf("expected Path='./main.go', got %q", match.Path)
	}
	if match.Line != 4 {
		t.Errorf("expected Line=4, got %d", match.Line)
	}
	if match.Content != "\t// search target here" {
		t.Errorf("expected Content, got %q", match.Content)
	}
}

func TestGrepRecursiveAndIgnores(t *testing.T) {
	dir := t.TempDir()

	// Setup dir structure
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(dir, ".gitignore"), "ignored/\n")
	writeFile(t, filepath.Join(dir, "main.go"), "package main\ntarget line")
	writeFile(t, filepath.Join(dir, ".env"), "API_KEY=target") // default ignore

	if err := os.Mkdir(filepath.Join(dir, "ignored"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(dir, "ignored", "foo.go"), "package main\ntarget line")

	handlers := NewHandlers(nil, dir)

	// Search recursively for "target"
	out, err := handlers.Grep(context.Background(), GrepArgs{Path: ".", Pattern: "target"})
	if err != nil {
		t.Fatalf("grep failed: %v", err)
	}

	// Should match main.go but not .env or ignored/foo.go
	var foundMain bool
	for _, m := range out.Matches {
		if gm, ok := m.(GrepMatch); ok {
			if gm.Path == "./main.go" {
				foundMain = true
			}
			if gm.Path == "./.env" || gm.Path == "./ignored/foo.go" {
				t.Errorf("unwanted match in ignored file: %s", gm.Path)
			}
		}
	}

	if !foundMain {
		t.Error("expected match in ./main.go")
	}
}

func TestGrepTextContent(t *testing.T) {
	outEmpty := GrepOutput{Matches: nil}
	if got := outEmpty.TextContent(); got != "No matches found." {
		t.Errorf("expected 'No matches found.', got %q", got)
	}

	out := GrepOutput{
		Matches: []any{
			GrepMatch{Path: "./main.go", Line: 10, Content: "some code"},
			GrepMatch{Path: "./main.go", Line: 20, Content: "other code"},
			GrepMatch{Path: "./utils.go", Line: 5, Content: "helper func"},
		},
		TotalCount: 3,
		Truncated:  false,
	}
	expected := "./main.go:\n  10: some code\n  20: other code\n\n./utils.go:\n  5: helper func\n\n[3 matches]"
	if got := out.TextContent(); got != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, got)
	}

	// Test per-line truncation
	longContent := make([]byte, 600)
	for i := range longContent {
		longContent[i] = 'a'
	}
	outLong := GrepOutput{
		Matches: []any{
			GrepMatch{Path: "./long.go", Line: 1, Content: string(longContent)},
		},
		TotalCount: 1,
		Truncated:  false,
	}
	expectedLong := "./long.go:\n  1: " + string(longContent[:500]) + " [truncated]\n\n[1 matches]"
	if got := outLong.TextContent(); got != expectedLong {
		t.Errorf("expected long line to truncate, got:\n%q", got)
	}

	// Test total matches truncation (max 100 rendered)
	matches := make([]any, 105)
	for i := 0; i < 105; i++ {
		matches[i] = GrepMatch{Path: "./main.go", Line: i + 1, Content: "code"}
	}
	outTruncated := GrepOutput{
		Matches:    matches,
		TotalCount: 105,
		Truncated:  true,
	}
	gotTruncated := outTruncated.TextContent()
	if !strings.Contains(gotTruncated, "[SYSTEM NOTE: Showing 100 of 105 matches. Call grep again with a more specific pattern.]") {
		t.Errorf("expected system note for showing 100 of 105 matches, got:\n%q", gotTruncated)
	}
	// Verify it printed exactly 100 matches in main.go
	lines := strings.Split(gotTruncated, "\n")
	matchLinesCount := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "  ") {
			matchLinesCount++
		}
	}
	if matchLinesCount != 100 {
		t.Errorf("expected exactly 100 match lines in output, got %d", matchLinesCount)
	}
}
