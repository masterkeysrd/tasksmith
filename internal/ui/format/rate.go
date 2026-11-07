package format

import (
	"fmt"
	"math"
)

// Rate formats a per-second rate, such as tokens generated per second. Values
// under 100 keep one decimal place, larger values are rounded to whole numbers
// so a column of rates stays the same width. The decision is made on the
// rounded value, otherwise 99.96 would print as "100.0" beside a neighbour's
// "100". A rate that is not finite or is negative means it could not be
// measured and renders as an em dash.
func Rate(perSecond float64) string {
	if math.IsNaN(perSecond) || math.IsInf(perSecond, 0) || perSecond < 0 {
		return "—"
	}
	if math.Round(perSecond) < 100 {
		return fmt.Sprintf("%.1f", perSecond)
	}
	return fmt.Sprintf("%.0f", perSecond)
}
