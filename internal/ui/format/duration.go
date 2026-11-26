package format

import (
	"fmt"
	"time"
)

// DurationOption tunes how [Duration] renders a span.
type DurationOption func(*durationConfig)

type durationConfig struct {
	compact bool
}

// Compact renders each span in its tightest form: no space between units and
// a trailing zero unit dropped ("3h40m", "42m", "3h" instead of "3h 40m",
// "42m 00s", "3h 00m"). Use it inside badges, countdowns, and error sentences
// where width is scarce.
func Compact() DurationOption {
	return func(c *durationConfig) { c.compact = true }
}

// Duration renders a duration in its two largest units (e.g. "3h 52m").
// Pass [Compact] for the tight single-token form. Callers are responsible
// for non-positive durations: pass only spans that make sense to render.
func Duration(d time.Duration, opts ...DurationOption) string {
	cfg := durationConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if d < 0 {
		d = 0
	}
	if cfg.compact {
		return compactDuration(d)
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

// compactDuration renders a duration as a single compact token: "50s", "42m",
// "3h", "3h40m", "2d", "6d18h".
func compactDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d >= 24*time.Hour {
		days := int(d.Hours()) / 24
		hours := int(d.Hours()) % 24
		if hours == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd%dh", days, hours)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h <= 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dh%dm", h, m)
	}
}

// DurationMilis renders a duration in its two largest units (e.g. "3h 52m") from milliseconds.
func DurationMilis(ms int64, opts ...DurationOption) string {
	return Duration(time.Duration(ms)*time.Millisecond, opts...)
}
