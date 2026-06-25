package tokenutils

import "testing"

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{999499, "999.5K"},
		{1000000, "1.0M"},
		{1500000, "1.5M"},
		{999499999, "999.5M"},
		{1000000000, "1.0B"},
		{1500000000, "1.5B"},
		{1232200, "1.2M"},
	}

	for _, tt := range tests {
		result := FormatTokens(tt.input)
		if result != tt.expected {
			t.Errorf("FormatTokens(%d) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
