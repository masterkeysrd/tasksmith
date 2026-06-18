package theme

import (
	"image/color"
	"testing"
)

func TestResolve(t *testing.T) {
	theme := &Theme{
		Name: "test",
		Type: "dark",
		Palette: map[string]string{
			"bg": "#16161e",
			"fg": "#7dcfff",
		},
		Colors: ThemeColors{
			Surface: ThemeSurfaceColor{
				Primary: "bg",
			},
			Text: ThemeTextColor{
				Primary: "fg",
			},
		},
	}

	scheme, err := Resolve(theme)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if scheme.Name != "test" {
		t.Errorf("expected name test, got %s", scheme.Name)
	}

	expectedPrimaryText := color.RGBA{R: 0x7d, G: 0xcf, B: 0xff, A: 255}
	if scheme.Color.Text.Primary != expectedPrimaryText {
		t.Errorf("expected text primary %v, got %v", expectedPrimaryText, scheme.Color.Text.Primary)
	}

	expectedPrimarySurface := color.RGBA{R: 0x16, G: 0x16, B: 0x1e, A: 255}
	if scheme.Color.Surface.Primary != expectedPrimarySurface {
		t.Errorf("expected surface primary %v, got %v", expectedPrimarySurface, scheme.Color.Surface.Primary)
	}
}

func TestFindBuiltin(t *testing.T) {
	theme, err := Find(Default)
	if err != nil {
		t.Fatalf("Find(Default) failed: %v", err)
	}

	if theme.Name != "tokyo-night" {
		t.Errorf("expected tokyo-night theme, got %s", theme.Name)
	}
}

func TestParseColor(t *testing.T) {
	tests := []struct {
		hex  string
		want color.RGBA
	}{
		{"#ffffff", color.RGBA{255, 255, 255, 255}},
		{"#00000000", color.RGBA{0, 0, 0, 0}},
		{"#ff0000", color.RGBA{255, 0, 0, 255}},
		{"112233", color.RGBA{0x11, 0x22, 0x33, 255}},
	}

	for _, tt := range tests {
		got, err := parseColor(tt.hex)
		if err != nil {
			t.Errorf("parseColor(%q) failed: %v", tt.hex, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseColor(%q) = %v, want %v", tt.hex, got, tt.want)
		}
	}
}
