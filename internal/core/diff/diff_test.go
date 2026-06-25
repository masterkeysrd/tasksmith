package diff

import (
	"strings"
	"testing"
)

func TestFormatUnifiedIdentical(t *testing.T) {
	content := "line 1\nline 2\nline 3\n"
	diff := FormatUnified("fileA", "fileB", content, content)
	if diff != "" {
		t.Errorf("expected empty diff for identical content, got:\n%q", diff)
	}

	diffEmpty := FormatUnified("fileA", "fileB", "", "")
	if diffEmpty != "" {
		t.Errorf("expected empty diff for empty inputs, got:\n%q", diffEmpty)
	}
}

func TestFormatUnifiedSimple(t *testing.T) {
	contentA := "line 1\nline 2\nline 3\n"
	contentB := "line 1\nline 2 modified\nline 3\n"

	got := FormatUnified("a.txt", "b.txt", contentA, contentB)
	expected := strings.Join([]string{
		"--- a.txt",
		"+++ b.txt",
		"@@ -1,3 +1,3 @@",
		" line 1",
		"-line 2",
		"+line 2 modified",
		" line 3",
		"",
	}, "\n")

	if got != expected {
		t.Errorf("expected diff:\n%q\ngot:\n%q", expected, got)
	}
}

func TestFormatUnifiedHunks(t *testing.T) {
	contentA := strings.Join([]string{
		"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12",
	}, "\n")
	contentB := strings.Join([]string{
		"1", "2 modified", "3", "4", "5", "6", "7", "8", "9", "10 modified", "11", "12",
	}, "\n")

	got := FormatUnified("a.txt", "b.txt", contentA, contentB)

	// Verify that it split into 2 hunks because the gap of equals (5, 6, 7, 8, 9) is 5 lines (more than 2 * context = 6).
	// Let's verify standard output structure.
	if !strings.Contains(got, "@@ -1,5 +1,5 @@") {
		t.Error("expected first hunk starting at line 1")
	}
	if !strings.Contains(got, "@@ -7,6 +7,6 @@") {
		t.Error("expected second hunk starting at line 7")
	}
}

func TestMerge3(t *testing.T) {
	t.Run("Clean merge", func(t *testing.T) {
		ancestor := []string{"line 1", "line 2", "line 3"}
		left := []string{"line 1", "line 2", "line 3 modified"}
		right := []string{"line 1 modified", "line 2", "line 3"}

		got, hasConflict := Merge3(ancestor, left, right)
		if hasConflict {
			t.Error("expected no conflict")
		}

		expected := []string{"line 1 modified", "line 2", "line 3 modified"}
		if !equalSlices(got, expected) {
			t.Errorf("expected merged content: %v, got %v", expected, got)
		}
	})

	t.Run("Conflicting merge", func(t *testing.T) {
		ancestor := []string{"line 1", "line 2", "line 3"}
		left := []string{"line 1", "line 2", "line 3 left"}
		right := []string{"line 1", "line 2", "line 3 right"}

		_, hasConflict := Merge3(ancestor, left, right)
		if !hasConflict {
			t.Error("expected conflict")
		}
	})
}
