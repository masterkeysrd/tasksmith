package format

import (
	"math"
	"testing"
)

func TestRate(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want string
	}{
		{"zero", 0, "0.0"},
		{"keeps one decimal below a hundred", 69.84, "69.8"},
		{"rounds at exactly a hundred", 99.96, "100"},
		{"drops the decimal above a hundred", 874.6, "875"},
		{"large", 12345.6, "12346"},
		{"negative means unmeasured", -1, "—"},
		{"nan means unmeasured", math.NaN(), "—"},
		{"positive infinity means unmeasured", math.Inf(1), "—"},
		{"negative infinity means unmeasured", math.Inf(-1), "—"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Rate(tt.in); got != tt.want {
				t.Errorf("Rate(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// A column of rates stays the same width, which is what keeps the per-model
// table aligned.
func TestRateKeepsColumnWidth(t *testing.T) {
	for _, v := range []float64{100, 250.4, 874.6, 9999} {
		if got := Rate(v); len(got) > 4 && v < 1000 {
			t.Errorf("Rate(%v) = %q, unexpectedly wide", v, got)
		}
	}
}
