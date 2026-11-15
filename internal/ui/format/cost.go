package format

import "fmt"

// FormatCostNano formats a nano-USD amount as dollars. Amounts under one cent
// keep four decimals so small costs stay visible. When unpriced is true the
// amount is a lower bound and is prefixed with "≥ ".
func FormatCostNano(nano int64, unpriced bool) string {
	usd := float64(nano) / 1e9
	var s string
	if usd < 0.01 && nano != 0 {
		s = fmt.Sprintf("$%.4f", usd)
	} else {
		s = fmt.Sprintf("$%.2f", usd)
	}
	if unpriced {
		return "≥ " + s
	}
	return s
}
