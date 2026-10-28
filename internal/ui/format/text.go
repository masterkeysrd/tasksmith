package format

import "strings"

// CompactSingleLine collapses consecutive whitespace and newlines into single spaces,
// trims leading and trailing whitespace, and truncates the string to maxRunes with an
// ellipsis ("…") if it exceeds that length. If maxRunes <= 0, no truncation is performed.
func CompactSingleLine(s string, maxRunes int) string {
	collapsed := strings.Join(strings.Fields(s), " ")
	if maxRunes > 0 {
		runes := []rune(collapsed)
		if len(runes) > maxRunes {
			return string(runes[:maxRunes-1]) + "…"
		}
	}
	return collapsed
}
