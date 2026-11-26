package format

import (
	"testing"
	"time"
)

func TestDurationDefault(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{-time.Minute, "0ms"},
		{500 * time.Millisecond, "500ms"},
		{5 * time.Second, "5.0s"},
		{50 * time.Second, "50s"},
		{42*time.Minute + 10*time.Second, "42m 10s"},
		{3 * time.Hour, "3h 00m"},
		{3*time.Hour + 40*time.Minute, "3h 40m"},
		{26 * time.Hour, "1d 2h"},
	}
	for _, tc := range tests {
		if got := Duration(tc.d); got != tc.want {
			t.Errorf("Duration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestDurationCompact(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{50 * time.Second, "50s"},
		{59 * time.Second, "59s"},
		{42 * time.Minute, "42m"},
		{3 * time.Hour, "3h"},
		{3*time.Hour + 40*time.Minute, "3h40m"},
		{48 * time.Hour, "2d"},
		{162*time.Hour + 51*time.Minute, "6d18h"},
	}
	for _, tc := range tests {
		if got := Duration(tc.d, Compact()); got != tc.want {
			t.Errorf("Duration(%v, Compact()) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestDurationMilis(t *testing.T) {
	if got := DurationMilis(3_600_000); got != "1h 00m" {
		t.Errorf("DurationMilis = %q, want %q", got, "1h 00m")
	}
	if got := DurationMilis(3_600_000, Compact()); got != "1h" {
		t.Errorf("DurationMilis compact = %q, want %q", got, "1h")
	}
}
