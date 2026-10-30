package format

import (
	"fmt"
	"time"
)

// Duration renders a duration in its two largest units (e.g. "3h 52m").
func Duration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		if d < 10*time.Second {
			return fmt.Sprintf("%.1fs", d.Seconds())
		}
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		days := int(d.Hours()) / 24
		return fmt.Sprintf("%dd %dh", days, int(d.Hours())%24)
	}
}

// DurationMilis renders a duration in its two largest units (e.g. "3h 52m") from milliseconds.
func DurationMilis(ms int64) string {
	return Duration(time.Duration(ms) * time.Millisecond)
}
