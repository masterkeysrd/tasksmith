package strutils

import (
	"strings"
	"unicode/utf8"
)

// TruncateBytes truncates s to at most maxBytes without splitting multi-byte
// UTF-8 runes. It returns the (possibly truncated) string and true if s was
// truncated.
func TruncateBytes(s string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s, false
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// TruncateLine truncates a single line s to at most maxBytes (UTF-8 safe) and
// appends suffix when truncated.
func TruncateLine(s string, maxBytes int, suffix string) (string, bool) {
	prefix, truncated := TruncateBytes(s, maxBytes)
	if !truncated {
		return s, false
	}
	return prefix + suffix, true
}

// TruncateHead truncates multi-line text s so that it contains at most
// maxLines lines and at most maxBytes bytes (UTF-8 safe). If maxLines <= 0,
// line count is not bounded; if maxBytes <= 0, byte count is not bounded.
// Trailing CR/LF characters are trimmed when truncation occurs.
func TruncateHead(s string, maxLines, maxBytes int) (string, bool) {
	cutIdx := len(s)
	truncated := false

	if maxBytes > 0 && cutIdx > maxBytes {
		cutIdx = maxBytes
		for cutIdx > 0 && !utf8.RuneStart(s[cutIdx]) {
			cutIdx--
		}
		truncated = true
	}

	if maxLines > 0 {
		lines := 0
		for i := 0; i < cutIdx; i++ {
			if s[i] == '\n' {
				lines++
				if lines >= maxLines {
					if i+1 < len(s) {
						cutIdx = i
						truncated = true
					}
					break
				}
			}
		}
	}

	if !truncated {
		return s, false
	}
	return strings.TrimRight(s[:cutIdx], "\r\n"), true
}

// CountLines returns the number of lines in s (1-based for non-empty strings,
// ignoring a single trailing newline).
func CountLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}
