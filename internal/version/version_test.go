package version

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"0.1.0", "v0.1.0"},
		{"v0.1.0", "v0.1.0"},
		{"  1.2.3  ", "v1.2.3"},
		{"", "v0.0.0-dev"},
	}

	for _, tt := range tests {
		if got := Normalize(tt.in); got != tt.want {
			t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGet(t *testing.T) {
	if got, want := Get(), Normalize(""); got != want {
		t.Errorf("Get() without a release stamp = %q, want %q", got, want)
	}

	original := Version
	t.Cleanup(func() { Version = original })
	Version = "1.2.3"

	if got, want := Get(), "v1.2.3"; got != want {
		t.Errorf("Get() with a release stamp = %q, want %q", got, want)
	}
}
