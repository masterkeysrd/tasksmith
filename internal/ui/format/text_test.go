package format

import "testing"

func TestCompactSingleLine(t *testing.T) {
	tests := []struct {
		input    string
		maxRunes int
		want     string
	}{
		{"hello\nworld", 0, "hello world"},
		{"  lots   of   \t  whitespace\n\n\r  ", 0, "lots of whitespace"},
		{"abcdefghij", 5, "abcd…"},
		{"abcdefghij", 10, "abcdefghij"},
		{"abcdefghij", 15, "abcdefghij"},
		{"", 5, ""},
		{"Expert in loom\narchitecture", 15, "Expert in loom…"},
	}
	for _, tc := range tests {
		got := CompactSingleLine(tc.input, tc.maxRunes)
		if got != tc.want {
			t.Errorf("CompactSingleLine(%q, %d) = %q, want %q", tc.input, tc.maxRunes, got, tc.want)
		}
	}
}
