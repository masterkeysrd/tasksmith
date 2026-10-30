package tokenizer

import (
	"strings"
	"testing"
)

func TestDefaultCharsPerToken(t *testing.T) {
	if DefaultCharsPerToken != 3.0 {
		t.Fatalf("DefaultCharsPerToken = %v, want 3.0", DefaultCharsPerToken)
	}
}

func TestEstimate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		// Empty input yields zero tokens.
		{"empty", "", 0},

		// ASCII: one token per up-to-3-byte chunk, rounding up.
		{"single byte", "a", 1},
		{"exactly one token", "abc", 1},
		{"rounds up partial token", "abcd", 2},
		{"two full tokens", "abcdef", 2},
		{"rounds up at boundary", "abcdefg", 3},
		{"three full tokens", "abcdefghi", 3},

		// Large ASCII inputs exercise the integer rounding boundary.
		{"large exact multiple", strings.Repeat("x", 3000), 1000},
		{"large rounds up", strings.Repeat("x", 3001), 1001},

		// Multi-byte UTF-8: estimation is byte-based, so accented and CJK
		// characters inflate the count relative to their rune count.
		{"accented", "café", 2},               // 5 bytes -> ceil(5/3)
		{"emoji", "😀", 2},                     // 4 bytes -> ceil(4/3)
		{"cjk", "日本語", 3},                     // 9 bytes -> ceil(9/3)
		{"mixed multibyte", "héllo wörld", 5}, // 13 bytes -> ceil(13/3)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Estimate(tc.input); got != tc.want {
				t.Errorf("Estimate(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestEstimateAgreesWithDefaultCustom(t *testing.T) {
	for _, s := range []string{"", "a", "abc", "abcd", strings.Repeat("y", 1234), "café", "日本語"} {
		if got, want := Estimate(s), EstimateCustom(s, DefaultCharsPerToken); got != want {
			t.Errorf("Estimate(%q) = %d, want EstimateCustom = %d", s, got, want)
		}
	}
}

func TestEstimateBytes(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  int
	}{
		{"empty", []byte{}, 0},
		{"one token", []byte("abc"), 1},
		{"rounds up", []byte("abcd"), 2},
		{"eight bytes", []byte("12345678"), 3},
		{"utf8 bytes", []byte("café"), 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := EstimateBytes(tc.input); got != tc.want {
				t.Errorf("EstimateBytes(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestEstimateBytesMatchesEstimate(t *testing.T) {
	for _, s := range []string{"", "abc", "abcd", strings.Repeat("z", 777), "café", "日本語"} {
		if got, want := EstimateBytes([]byte(s)), Estimate(s); got != want {
			t.Errorf("EstimateBytes(%q) = %d, want Estimate = %d", s, got, want)
		}
	}
}

func TestEstimateCustom(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		charsPerTok float64
		want        int
	}{
		{"empty any density", "", 4.0, 0},
		{"density 4 rounds up", "abcde", 4.0, 2},
		{"density 4 exact", "abcd", 4.0, 1},
		{"density 1", "abcde", 1.0, 5},
		{"density 2 rounds up", "abcde", 2.0, 3},
		{"zero density falls back to default", "abc", 0, 1},
		{"negative density falls back to default", "abcd", -1.0, 2},
		{"default density matches Estimate", "abcdefghi", DefaultCharsPerToken, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := EstimateCustom(tc.input, tc.charsPerTok); got != tc.want {
				t.Errorf("EstimateCustom(%q, %v) = %d, want %d", tc.input, tc.charsPerTok, got, tc.want)
			}
		})
	}
}

func TestEstimateRoundsUp(t *testing.T) {
	// Verify the ceil boundary: every length in (3k, 3k+3] maps to k+1 tokens.
	for n := 1; n <= 30; n++ {
		want := (n + 2) / 3 // integer ceil of n/3
		if got := Estimate(strings.Repeat("a", n)); got != want {
			t.Errorf("Estimate(len=%d) = %d, want %d", n, got, want)
		}
	}
}
