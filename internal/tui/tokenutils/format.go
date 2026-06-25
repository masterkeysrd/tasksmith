package tokenutils

import "fmt"

// FormatTokens formats an integer token count into a human-readable string
// with K/M/B suffixes.
func FormatTokens(tokens int) string {
	switch {
	case tokens >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(tokens)/1_000_000_000.0)
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000.0)
	case tokens >= 1000:
		return fmt.Sprintf("%.1fK", float64(tokens)/1000.0)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}
