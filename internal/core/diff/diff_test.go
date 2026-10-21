package diff

import (
	"reflect"
	"strings"
	"testing"
)

func TestOpString(t *testing.T) {
	if got := OpEqual.String(); got != "equal" {
		t.Errorf("OpEqual.String() = %q, want %q", got, "equal")
	}
	if got := OpDelete.String(); got != "delete" {
		t.Errorf("OpDelete.String() = %q, want %q", got, "delete")
	}
	if got := OpInsert.String(); got != "insert" {
		t.Errorf("OpInsert.String() = %q, want %q", got, "insert")
	}
	if got := Op(99).String(); got != "Op(99)" {
		t.Errorf("Op(99).String() = %q, want %q", got, "Op(99)")
	}
}

func TestMyersDiff(t *testing.T) {
	t.Run("Both empty", func(t *testing.T) {
		edits := MyersDiff(nil, nil)
		if len(edits) != 0 {
			t.Fatalf("expected 0 edits, got %v", edits)
		}
	})

	t.Run("Empty to non-empty", func(t *testing.T) {
		b := []string{"alpha", "beta"}
		edits := MyersDiff(nil, b)
		want := []Edit{
			{Op: OpInsert, Line: "alpha"},
			{Op: OpInsert, Line: "beta"},
		}
		if !reflect.DeepEqual(edits, want) {
			t.Fatalf("got %v, want %v", edits, want)
		}
	})

	t.Run("Non-empty to empty", func(t *testing.T) {
		a := []string{"alpha", "beta"}
		edits := MyersDiff(a, nil)
		want := []Edit{
			{Op: OpDelete, Line: "alpha"},
			{Op: OpDelete, Line: "beta"},
		}
		if !reflect.DeepEqual(edits, want) {
			t.Fatalf("got %v, want %v", edits, want)
		}
	})

	t.Run("Identical slices", func(t *testing.T) {
		a := []string{"a", "b", "c"}
		edits := MyersDiff(a, a)
		want := []Edit{
			{Op: OpEqual, Line: "a"},
			{Op: OpEqual, Line: "b"},
			{Op: OpEqual, Line: "c"},
		}
		if !reflect.DeepEqual(edits, want) {
			t.Fatalf("got %v, want %v", edits, want)
		}
	})

	t.Run("Shortest edit script reconstruction", func(t *testing.T) {
		a := []string{"A", "B", "C", "A", "B", "B", "A"}
		b := []string{"C", "B", "A", "B", "A", "C"}
		edits := MyersDiff(a, b)

		var reconstructedA, reconstructedB []string
		editDistance := 0
		for _, e := range edits {
			switch e.Op {
			case OpEqual:
				reconstructedA = append(reconstructedA, e.Line)
				reconstructedB = append(reconstructedB, e.Line)
			case OpDelete:
				reconstructedA = append(reconstructedA, e.Line)
				editDistance++
			case OpInsert:
				reconstructedB = append(reconstructedB, e.Line)
				editDistance++
			}
		}

		if !reflect.DeepEqual(reconstructedA, a) {
			t.Errorf("reconstructed A = %v, want %v", reconstructedA, a)
		}
		if !reflect.DeepEqual(reconstructedB, b) {
			t.Errorf("reconstructed B = %v, want %v", reconstructedB, b)
		}
		// Classic Myers 1986 paper example has shortest edit distance D = 5.
		if editDistance != 5 {
			t.Errorf("edit distance = %d, want 5", editDistance)
		}
	})
}

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

func TestFormatUnifiedEmptyToNonEmpty(t *testing.T) {
	contentB := "first line\nsecond line\n"
	got := FormatUnified("a.txt", "b.txt", "", contentB)
	expected := strings.Join([]string{
		"--- a.txt",
		"+++ b.txt",
		"@@ -1,0 +1,2 @@",
		"+first line",
		"+second line",
		"",
	}, "\n")

	if got != expected {
		t.Errorf("expected diff:\n%q\ngot:\n%q", expected, got)
	}
}

func TestFormatUnifiedNonEmptyToEmpty(t *testing.T) {
	contentA := "first line\nsecond line\n"
	got := FormatUnified("a.txt", "b.txt", contentA, "")
	expected := strings.Join([]string{
		"--- a.txt",
		"+++ b.txt",
		"@@ -1,2 +1,0 @@",
		"-first line",
		"-second line",
		"",
	}, "\n")

	if got != expected {
		t.Errorf("expected diff:\n%q\ngot:\n%q", expected, got)
	}
}

func TestFormatUnifiedSingleHunk(t *testing.T) {
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

func TestFormatUnifiedMultiHunk(t *testing.T) {
	contentA := strings.Join([]string{
		"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12",
	}, "\n")
	contentB := strings.Join([]string{
		"1", "2 modified", "3", "4", "5", "6", "7", "8", "9", "10 modified", "11", "12",
	}, "\n")

	got := FormatUnified("a.txt", "b.txt", contentA, contentB)

	// Verify that it splits into 2 hunks because the gap of equals (3..9) is 7 lines (> 2*context = 6).
	if !strings.Contains(got, "@@ -1,5 +1,5 @@") {
		t.Errorf("expected first hunk starting at line 1, got:\n%s", got)
	}
	if !strings.Contains(got, "@@ -7,6 +7,6 @@") {
		t.Errorf("expected second hunk starting at line 7, got:\n%s", got)
	}
}

func TestFormatUnifiedCRLFNormalization(t *testing.T) {
	contentA := "line 1\r\nline 2\r\nline 3\r\n"
	contentB := "line 1\nline 2\nline 3\n"

	if got := FormatUnified("a.txt", "b.txt", contentA, contentB); got != "" {
		t.Errorf("expected empty diff when only CRLF vs LF differs, got:\n%q", got)
	}

	contentBMod := "line 1\r\nline 2 changed\r\nline 3\r\n"
	got := FormatUnified("a.txt", "b.txt", contentA, contentBMod)
	expected := strings.Join([]string{
		"--- a.txt",
		"+++ b.txt",
		"@@ -1,3 +1,3 @@",
		" line 1",
		"-line 2",
		"+line 2 changed",
		" line 3",
		"",
	}, "\n")
	if got != expected {
		t.Errorf("expected normalized diff:\n%q\ngot:\n%q", expected, got)
	}
}

func TestStats(t *testing.T) {
	t.Run("Identical and empty", func(t *testing.T) {
		add, rem := Stats("", "")
		if add != 0 || rem != 0 {
			t.Errorf("Stats empty = (%d, %d), want (0, 0)", add, rem)
		}

		add, rem = Stats("a\nb\nc\n", "a\nb\nc\n")
		if add != 0 || rem != 0 {
			t.Errorf("Stats identical = (%d, %d), want (0, 0)", add, rem)
		}
	})

	t.Run("Empty to non-empty", func(t *testing.T) {
		add, rem := Stats("", "a\nb\nc\n")
		if add != 3 || rem != 0 {
			t.Errorf("Stats empty->3 lines = (%d, %d), want (3, 0)", add, rem)
		}
	})

	t.Run("Non-empty to empty", func(t *testing.T) {
		add, rem := Stats("a\nb\n", "")
		if add != 0 || rem != 2 {
			t.Errorf("Stats 2 lines->empty = (%d, %d), want (0, 2)", add, rem)
		}
	})

	t.Run("Mixed additions and deletions with CRLF", func(t *testing.T) {
		contentA := "one\r\ntwo\r\nthree\r\nfour\r\n"
		contentB := "one\ntwo modified\nfour\nfive\nsix\n"
		// "two" deleted, "three" deleted (removed=2)
		// "two modified", "five", "six" inserted (added=3)
		add, rem := Stats(contentA, contentB)
		if add != 3 || rem != 2 {
			t.Errorf("Stats mixed = (%d, %d), want (3, 2)", add, rem)
		}
	})
}

func TestMerge3(t *testing.T) {
	t.Run("Non-overlapping changes merge cleanly", func(t *testing.T) {
		ancestor := []string{"1", "2", "3", "4", "5"}
		left := []string{"1-left", "2", "3", "4", "5"}
		right := []string{"1", "2", "3", "4", "5-right"}

		merged, conflict := Merge3(ancestor, left, right)
		if conflict {
			t.Fatalf("expected no conflict, got conflict=true")
		}
		want := []string{"1-left", "2", "3", "4", "5-right"}
		if !reflect.DeepEqual(merged, want) {
			t.Errorf("merged = %v, want %v", merged, want)
		}
	})

	t.Run("Overlapping divergent changes report conflict", func(t *testing.T) {
		ancestor := []string{"1", "2", "3"}
		left := []string{"1", "2-left", "3"}
		right := []string{"1", "2-right", "3"}

		_, conflict := Merge3(ancestor, left, right)
		if !conflict {
			t.Fatalf("expected conflict=true for overlapping divergent edits")
		}
	})

	t.Run("Identical overlapping changes do not conflict", func(t *testing.T) {
		ancestor := []string{"1", "2", "3"}
		left := []string{"1", "2-same", "3"}
		right := []string{"1", "2-same", "3"}

		merged, conflict := Merge3(ancestor, left, right)
		if conflict {
			t.Fatalf("expected conflict=false for identical edits")
		}
		if !reflect.DeepEqual(merged, left) {
			t.Errorf("merged = %v, want %v", merged, left)
		}
	})
}
