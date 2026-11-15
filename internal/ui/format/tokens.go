package format

import (
	"fmt"
	"math"
)

// Tokens formats a token count into a human-readable string (e.g. 0, 840, 6.0k, 109.5k, 1.0M).
// It formats counts under 1,000 as raw integers, thousands with a 'k' suffix, and
// millions with an 'M' suffix, formatted to one decimal place. Negative counts
// are prefixed with a minus sign.
func Tokens(tokens int) string {
	if tokens < 0 {
		if tokens == math.MinInt {
			return fmt.Sprintf("-%.1fM", math.Abs(float64(tokens))/1_000_000.0)
		}
		return "-" + Tokens(-tokens)
	}
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	if tokens < 1_000_000 {
		return fmt.Sprintf("%.1fk", float64(tokens)/1000.0)
	}
	return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000.0)
}
