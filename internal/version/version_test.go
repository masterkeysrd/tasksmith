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
	v := Get()
	if v == "" {
		t.Fatal("expected non-empty version")
	}
	if v[0] != 'v' {
		t.Fatalf("expected version to start with 'v', got %q", v)
	}
}
