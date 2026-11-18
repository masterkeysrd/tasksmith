package format

import "testing"

func TestBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1 KB"},
		{1536, "1.5 KB"},
		{142 * 1024, "142 KB"},
		{2 * 1024 * 1024, "2 MB"},
		{2560 * 1024, "2.5 MB"},
	}
	for _, tc := range tests {
		if got := Bytes(tc.in); got != tc.want {
			t.Errorf("Bytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
