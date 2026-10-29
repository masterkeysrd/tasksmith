package strutils_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/masterkeysrd/tasksmith/internal/core/strutils"
)

func TestTruncateBytesAndLine(t *testing.T) {
	// Multi-byte UTF-8 rune: 'é' is 2 bytes, '🚀' is 4 bytes.
	input := "hello 🚀 world"
	// "hello " is 6 bytes; '🚀' spans bytes 6..9. Cutting at 8 must back up to 6.
	got, truncated := strutils.TruncateBytes(input, 8)
	if !truncated {
		t.Fatal("expected truncated=true")
	}
	if !utf8.ValidString(got) || got != "hello " {
		t.Fatalf("unexpected TruncateBytes result: %q", got)
	}

	line, truncated := strutils.TruncateLine(input, 8, "... [truncated]")
	if !truncated || line != "hello ... [truncated]" {
		t.Fatalf("unexpected TruncateLine result: %q", line)
	}

	unchanged, truncated := strutils.TruncateLine("short", 20, "...")
	if truncated || unchanged != "short" {
		t.Fatalf("expected unchanged line, got %q (truncated=%v)", unchanged, truncated)
	}
}

func TestTruncateHead(t *testing.T) {
	text := "line1\nline2\nline3\nline4\n"

	// Within limits
	got, truncated := strutils.TruncateHead(text, 10, 100)
	if truncated || got != text {
		t.Fatalf("expected no truncation, got %q (truncated=%v)", got, truncated)
	}

	// Exactly 4 lines with trailing newline should not truncate when maxLines=4
	got, truncated = strutils.TruncateHead(text, 4, 100)
	if truncated || got != text {
		t.Fatalf("expected no truncation at exact line count, got %q (truncated=%v)", got, truncated)
	}

	// Truncate by line count
	got, truncated = strutils.TruncateHead(text, 2, 100)
	if !truncated || got != "line1\nline2" {
		t.Fatalf("expected 2 lines, got %q (truncated=%v)", got, truncated)
	}

	// Truncate by byte count
	got, truncated = strutils.TruncateHead(text, 10, 8)
	if !truncated || !strings.HasPrefix(got, "line1\nli") {
		t.Fatalf("expected byte truncation, got %q (truncated=%v)", got, truncated)
	}
}

func TestCountLines(t *testing.T) {
	if n := strutils.CountLines(""); n != 0 {
		t.Fatalf("expected 0 lines for empty string, got %d", n)
	}
	if n := strutils.CountLines("a\nb\n"); n != 2 {
		t.Fatalf("expected 2 lines for 'a\\nb\\n', got %d", n)
	}
	if n := strutils.CountLines("a\nb"); n != 2 {
		t.Fatalf("expected 2 lines for 'a\\nb', got %d", n)
	}
}
