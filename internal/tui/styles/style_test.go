package styles

import (
	"image/color"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/tui/colorscheme"
)

// --- Unit Tests ---

// TestParseColorWithPrefix tests parseColor with a # prefix.
func TestParseColorWithPrefix(t *testing.T) {
	tests := []struct {
		input string
		wantR uint8
		wantG uint8
		wantB uint8
		wantA uint8
	}{
		{"#c8d0e0", 0xc8, 0xd0, 0xe0, 0xff},
		{"#1e1e2e", 0x1e, 0x1e, 0x2e, 0xff},
		{"#abcdef", 0xab, 0xcd, 0xef, 0xff},
		{"#ABCDEF", 0xab, 0xcd, 0xef, 0xff},
		{"#abc123", 0xab, 0xc1, 0x23, 0xff},
		// 8-char hex with alpha channel
		{"#ffffff0d", 0xff, 0xff, 0xff, 0x0d},
		{"#ffffff00", 0xff, 0xff, 0xff, 0x00}, // fully transparent
		{"#1e1e2ecc", 0x1e, 0x1e, 0x2e, 0xcc}, // semi-transparent
		{"#ff000080", 0xff, 0x00, 0x00, 0x80}, // red with 50% opacity
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseColor(tc.input)
			if err != nil {
				t.Fatalf("parseColor(%q) returned error: %v", tc.input, err)
			}
			if got.R != tc.wantR {
				t.Errorf("R = %02x, want %02x", got.R, tc.wantR)
			}
			if got.G != tc.wantG {
				t.Errorf("G = %02x, want %02x", got.G, tc.wantG)
			}
			if got.B != tc.wantB {
				t.Errorf("B = %02x, want %02x", got.B, tc.wantB)
			}
			if got.A != tc.wantA {
				t.Errorf("A = %02x, want %02x", got.A, tc.wantA)
			}
		})
	}
}

// TestParseColorWithoutPrefix tests parseColor without a # prefix.
func TestParseColorWithoutPrefix(t *testing.T) {
	tests := []struct {
		input string
		wantR uint8
		wantG uint8
		wantB uint8
		wantA uint8
	}{
		{"c8d0e0", 0xc8, 0xd0, 0xe0, 0xff},
		{"1e1e2e", 0x1e, 0x1e, 0x2e, 0xff},
		{"abcdef", 0xab, 0xcd, 0xef, 0xff},
		// 8-char hex without prefix
		{"ffffff0d", 0xff, 0xff, 0xff, 0x0d},
		{"1e1e2ecc", 0x1e, 0x1e, 0x2e, 0xcc},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseColor(tc.input)
			if err != nil {
				t.Fatalf("parseColor(%q) returned error: %v", tc.input, err)
			}
			if got.R != tc.wantR {
				t.Errorf("R = %02x, want %02x", got.R, tc.wantR)
			}
			if got.G != tc.wantG {
				t.Errorf("G = %02x, want %02x", got.G, tc.wantG)
			}
			if got.B != tc.wantB {
				t.Errorf("B = %02x, want %02x", got.B, tc.wantB)
			}
		})
	}
}

// TestParseColorInvalid tests parseColor with invalid inputs.
func TestParseColorInvalid(t *testing.T) {
	invalidInputs := []string{
		"#abc",       // too short
		"#abcdefg",   // too long
		"zzzzzz",     // invalid hex
		"#gggggg",    // invalid hex
		"",           // empty
		"#abcde",     // 5 chars
		"#abcdef0",   // 7 chars - invalid
		"#abcdef012", // 9 chars - invalid
	}

	for _, input := range invalidInputs {
		t.Run(input, func(t *testing.T) {
			_, err := parseColor(input)
			if err == nil {
				t.Errorf("parseColor(%q) returned no error, want error", input)
			}
		})
	}
}

// TestNormalHighlightMapsToNormalStyle tests scenario 1:
// Normal fg=#c8d0e0, bg=#1e1e2e → styles["Normal"] has correct fg/bg.
func TestNormalHighlightMapsToNormalStyle(t *testing.T) {
	resolved := map[string]colorscheme.ResolvedColor{
		"Normal": {Fg: "#c8d0e0", Bg: "#1e1e2e"},
	}
	styles := BuildFrom(resolved)

	norm, ok := styles["Normal"]
	if !ok {
		t.Fatal("styles[Normal] not found")
	}

	fg, ok := norm.ForegroundOpt().Get()
	if !ok {
		t.Fatal("Normal foreground not set")
	}
	rgba, ok := fg.(color.RGBA)
	if !ok {
		t.Fatalf("foreground is not RGBA: %T", fg)
	}
	if rgba.R != 0xc8 || rgba.G != 0xd0 || rgba.B != 0xe0 {
		t.Errorf("Normal fg = %02x%02x%02x, want c8d0e0", rgba.R, rgba.G, rgba.B)
	}

	bg, ok := norm.BackgroundOpt().Get()
	if !ok {
		t.Fatal("Normal background not set")
	}
	rgba2, ok := bg.(color.RGBA)
	if !ok {
		t.Fatalf("background is not RGBA: %T", bg)
	}
	if rgba2.R != 0x1e || rgba2.G != 0x1e || rgba2.B != 0x2e {
		t.Errorf("Normal bg = %02x%02x%02x, want 1e1e2e", rgba2.R, rgba2.G, rgba2.B)
	}
}

// TestCommentHighlightMapsToCommentStyle tests scenario 2:
// Comment fg=#585b70 → styles["Comment"] has correct fg, no bg.
func TestCommentHighlightMapsToCommentStyle(t *testing.T) {
	resolved := map[string]colorscheme.ResolvedColor{
		"Comment": {Fg: "#585b70"},
	}
	styles := BuildFrom(resolved)

	cmt, ok := styles["Comment"]
	if !ok {
		t.Fatal("styles[Comment] not found")
	}

	fg, ok := cmt.ForegroundOpt().Get()
	if !ok {
		t.Fatal("Comment foreground not set")
	}
	rgba, ok := fg.(color.RGBA)
	if !ok {
		t.Fatalf("foreground is not RGBA: %T", fg)
	}
	if rgba.R != 0x58 || rgba.G != 0x5b || rgba.B != 0x70 {
		t.Errorf("Comment fg = %02x%02x%02x, want 585b70", rgba.R, rgba.G, rgba.B)
	}

	_, ok = cmt.BackgroundOpt().Get()
	if ok {
		t.Error("Comment background should not be set")
	}
}

// TestStatusLineBoldMapsToStatusLineStyle tests scenario 3:
// StatusLine bold=true → styles["StatusLine"] has Bold=true.
func TestStatusLineBoldMapsToStatusLineStyle(t *testing.T) {
	bold := true
	resolved := map[string]colorscheme.ResolvedColor{
		"StatusLine": {Fg: "#c8d0e0", Bg: "#1e1e2e", Bold: bold},
	}
	styles := BuildFrom(resolved)

	sl, ok := styles["StatusLine"]
	if !ok {
		t.Fatal("styles[StatusLine] not found")
	}

	boldVal, ok := sl.BoldOpt().Get()
	if !ok {
		t.Fatal("StatusLine bold not set")
	}
	if !boldVal {
		t.Error("StatusLine bold = false, want true")
	}
}

// TestEmptyColorscheme tests scenario 6:
// BuildFrom() with empty resolved map returns empty style map.
func TestEmptyColorscheme(t *testing.T) {
	styles := BuildFrom(nil)
	if len(styles) != 0 {
		t.Errorf("BuildFrom(nil) returned %d styles, want 0", len(styles))
	}

	styles = BuildFrom(map[string]colorscheme.ResolvedColor{})
	if len(styles) != 0 {
		t.Errorf("BuildFrom(empty) returned %d styles, want 0", len(styles))
	}
}

// TestMissingHighlightGroup tests scenario 7:
// If Normal is missing from resolved map, derived styles that depend on it use zero-values.
func TestMissingHighlightGroup(t *testing.T) {
	// Only provide Comment, not Normal
	resolved := map[string]colorscheme.ResolvedColor{
		"Comment": {Fg: "#585b70"},
	}
	styles := BuildFrom(resolved)

	// Normal should not be in the map
	_, ok := styles["Normal"]
	if ok {
		t.Error("Normal should not be in styles when Normal highlight is missing")
	}
}

// TestAllModifiers tests scenario 10:
// A highlight with fg, bg, bold, underline, italic → all mapped correctly.
func TestAllModifiers(t *testing.T) {
	bold := true
	underline := true
	italic := true
	resolved := map[string]colorscheme.ResolvedColor{
		"Normal": {
			Fg:        "#c8d0e0",
			Bg:        "#1e1e2e",
			Bold:      bold,
			Underline: underline,
			Italic:    italic,
		},
	}
	styles := BuildFrom(resolved)

	norm, ok := styles["Normal"]
	if !ok {
		t.Fatal("styles[Normal] not found")
	}

	// Check fg
	fg, ok := norm.ForegroundOpt().Get()
	if !ok {
		t.Fatal("Normal foreground not set")
	}
	rgba, ok := fg.(color.RGBA)
	if !ok {
		t.Fatalf("foreground is not RGBA: %T", fg)
	}
	if rgba.R != 0xc8 || rgba.G != 0xd0 || rgba.B != 0xe0 {
		t.Errorf("Normal fg = %02x%02x%02x, want c8d0e0", rgba.R, rgba.G, rgba.B)
	}

	// Check bg
	bg, ok := norm.BackgroundOpt().Get()
	if !ok {
		t.Fatal("Normal background not set")
	}
	rgba2, ok := bg.(color.RGBA)
	if !ok {
		t.Fatalf("background is not RGBA: %T", bg)
	}
	if rgba2.R != 0x1e || rgba2.G != 0x1e || rgba2.B != 0x2e {
		t.Errorf("Normal bg = %02x%02x%02x, want 1e1e2e", rgba2.R, rgba2.G, rgba2.B)
	}

	// Check bold
	boldVal, ok := norm.BoldOpt().Get()
	if !ok || !boldVal {
		t.Error("Normal bold should be true")
	}

	// Check underline
	underVal, ok := norm.UnderlineOpt().Get()
	if !ok || !underVal {
		t.Error("Normal underline should be true")
	}

	// Check italic
	italicVal, ok := norm.ItalicOpt().Get()
	if !ok || !italicVal {
		t.Error("Normal italic should be true")
	}
}

// TestImmutableMap tests scenario 12:
// Modifying the returned map does not affect subsequent calls to BuildFrom().
func TestImmutableMap(t *testing.T) {
	resolved := map[string]colorscheme.ResolvedColor{
		"Normal": {Fg: "#c8d0e0", Bg: "#1e1e2e"},
	}

	// Build again
	styles2 := BuildFrom(resolved)

	// The second build should still have the original color
	norm2, ok := styles2["Normal"]
	if !ok {
		t.Fatal("Normal not found in styles2")
	}
	fg2, ok := norm2.ForegroundOpt().Get()
	if !ok {
		t.Fatal("Normal foreground not set in styles2")
	}
	rgba2, ok := fg2.(color.RGBA)
	if !ok {
		t.Fatalf("foreground is not RGBA: %T", fg2)
	}
	if rgba2.R != 0xc8 {
		t.Errorf("styles2 Normal fg.R = %02x, want c8 (immutable)", rgba2.R)
	}
}

// TestHighlightToStyleDirect tests highlightToStyle with direct highlight values.
func TestHighlightToStyleDirect(t *testing.T) {
	h := colorscheme.ResolvedColor{
		Fg:        "#c8d0e0",
		Bg:        "#1e1e2e",
		Bold:      true,
		Underline: true,
		Italic:    true,
	}
	s := highlightToStyle(h)

	// Check fg
	fg, ok := s.ForegroundOpt().Get()
	if !ok {
		t.Fatal("Foreground not set")
	}
	rgba, ok := fg.(color.RGBA)
	if !ok {
		t.Fatalf("foreground is not RGBA: %T", fg)
	}
	if rgba.R != 0xc8 || rgba.G != 0xd0 || rgba.B != 0xe0 {
		t.Errorf("fg = %02x%02x%02x, want c8d0e0", rgba.R, rgba.G, rgba.B)
	}

	// Check bg
	bg, ok := s.BackgroundOpt().Get()
	if !ok {
		t.Fatal("Background not set")
	}
	rgba2, ok := bg.(color.RGBA)
	if !ok {
		t.Fatalf("background is not RGBA: %T", bg)
	}
	if rgba2.R != 0x1e || rgba2.G != 0x1e || rgba2.B != 0x2e {
		t.Errorf("bg = %02x%02x%02x, want 1e1e2e", rgba2.R, rgba2.G, rgba2.B)
	}

	// Check bold
	boldVal, ok := s.BoldOpt().Get()
	if !ok || !boldVal {
		t.Error("Bold should be true")
	}

	// Check underline
	underVal, ok := s.UnderlineOpt().Get()
	if !ok || !underVal {
		t.Error("Underline should be true")
	}

	// Check italic
	italicVal, ok := s.ItalicOpt().Get()
	if !ok || !italicVal {
		t.Error("Italic should be true")
	}
}

// TestHighlightToStyleNoProperties tests highlightToStyle with zero-value ResolvedColor.
func TestHighlightToStyleNoProperties(t *testing.T) {
	h := colorscheme.ResolvedColor{}
	s := highlightToStyle(h)

	if _, ok := s.ForegroundOpt().Get(); ok {
		t.Error("Foreground should not be set")
	}
	if _, ok := s.BackgroundOpt().Get(); ok {
		t.Error("Background should not be set")
	}
	if _, ok := s.BoldOpt().Get(); ok {
		t.Error("Bold should not be set")
	}
	if _, ok := s.UnderlineOpt().Get(); ok {
		t.Error("Underline should not be set")
	}
	if _, ok := s.ItalicOpt().Get(); ok {
		t.Error("Italic should not be set")
	}
}

// TestBuildFromImmutableMap tests that modifying the returned map
// does not affect subsequent calls.
func TestBuildFromImmutableMap(t *testing.T) {
	resolved := map[string]colorscheme.ResolvedColor{
		"Normal": {Fg: "#c8d0e0", Bg: "#1e1e2e"},
	}

	styles1 := BuildFrom(resolved)

	// Modify the map directly
	delete(styles1, "Normal")

	// Build again
	styles2 := BuildFrom(resolved)

	// The second build should still have Normal
	if _, ok := styles2["Normal"]; !ok {
		t.Error("styles2 should have Normal after deleting from styles1")
	}
}
