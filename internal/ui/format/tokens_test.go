package format

import (
	"math"
	"testing"
)

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{840, "840"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{6000, "6.0k"},
		{109500, "109.5k"},
		{400000, "400.0k"},
		{1000000, "1.0M"},
		{2500000, "2.5M"},
		{-500, "-500"},
		{-1500, "-1.5k"},
		{-109500, "-109.5k"},
		{-2500000, "-2.5M"},
	}
	for _, tc := range tests {
		got := Tokens(tc.input)
		if got != tc.want {
			t.Errorf("FormatTokens(%d) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestFormatTokensMinInt(t *testing.T) {
	got := Tokens(math.MinInt)
	if got == "" || got[0] != '-' {
		t.Errorf("FormatTokens(math.MinInt) expected negative result, got %q", got)
	}
}
