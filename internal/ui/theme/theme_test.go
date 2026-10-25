package theme

import (
	"image/color"
	"testing"

	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/testenv"
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

func TestUseThemeFallback(t *testing.T) {
	scheme := UseTheme()
	if scheme == nil {
		t.Fatal("expected UseTheme() to fall back to global scheme when outside provider, got nil")
	}
	if scheme.Name == "" {
		t.Error("expected non-empty scheme name")
	}
}

func TestResolveColorTransparent(t *testing.T) {
	s := &Scheme{Palette: map[string]color.Color{}}
	for _, val := range []string{"", "transparent", "TRANSPARENT", "none", "NONE", "None"} {
		c, err := s.resolveColor(val)
		if err != nil {
			t.Fatalf("resolveColor(%q) error: %v", val, err)
		}
		if c != nil {
			t.Errorf("resolveColor(%q) = %v, want nil", val, c)
		}
	}
}

func TestSchemeWithTransparent(t *testing.T) {
	if (*Scheme)(nil).WithTransparent(true) != nil {
		t.Fatal("expected nil scheme WithTransparent to return nil")
	}

	focusColor := color.RGBA{R: 40, G: 40, B: 40, A: 255}
	s := &Scheme{
		Name: "test-scheme",
		Color: Color{
			Surface: SurfaceColor{
				Base:         color.RGBA{R: 10, G: 10, B: 10, A: 255},
				BaseHover:    color.RGBA{R: 20, G: 20, B: 20, A: 255},
				BasePressed:  color.RGBA{R: 30, G: 30, B: 30, A: 255},
				BaseFocus:    focusColor,
				BaseDisabled: color.RGBA{R: 50, G: 50, B: 50, A: 255},
			},
		},
	}

	trans := s.WithTransparent(true)
	if !trans.Transparent {
		t.Errorf("expected trans.Transparent to be true")
	}
	if trans.Color.Surface.Base != nil {
		t.Errorf("expected Base to be nil, got %v", trans.Color.Surface.Base)
	}
	if trans.Color.Surface.BaseHover != nil {
		t.Errorf("expected BaseHover to be nil, got %v", trans.Color.Surface.BaseHover)
	}
	if trans.Color.Surface.BasePressed != nil {
		t.Errorf("expected BasePressed to be nil, got %v", trans.Color.Surface.BasePressed)
	}
	if trans.Color.Surface.BaseDisabled != nil {
		t.Errorf("expected BaseDisabled to be nil, got %v", trans.Color.Surface.BaseDisabled)
	}
	if trans.Color.Surface.BaseFocus != focusColor {
		t.Errorf("expected BaseFocus to remain %v, got %v", focusColor, trans.Color.Surface.BaseFocus)
	}

	// Verify original scheme is unchanged
	if s.Color.Surface.Base == nil {
		t.Errorf("original scheme mutated by WithTransparent")
	}

	nonTrans := trans.WithTransparent(false)
	if nonTrans.Transparent {
		t.Errorf("expected nonTrans.Transparent to be false")
	}
}

func TestResolveTransparentTheme(t *testing.T) {
	theme := &Theme{
		Name:        "trans-theme",
		Type:        "dark",
		Transparent: true,
		Palette: map[string]string{
			"bg":    "#111111",
			"focus": "#444444",
		},
		Colors: ThemeColors{
			Surface: ThemeSurfaceColor{
				Base:         "bg",
				BaseHover:    "bg",
				BasePressed:  "bg",
				BaseFocus:    "focus",
				BaseDisabled: "bg",
			},
		},
	}

	scheme, err := Resolve(theme)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if !scheme.Transparent {
		t.Errorf("expected scheme.Transparent to be true")
	}
	if scheme.Color.Surface.Base != nil {
		t.Errorf("expected Base to be nil, got %v", scheme.Color.Surface.Base)
	}
	if scheme.Color.Surface.BaseHover != nil {
		t.Errorf("expected BaseHover to be nil, got %v", scheme.Color.Surface.BaseHover)
	}
	if scheme.Color.Surface.BasePressed != nil {
		t.Errorf("expected BasePressed to be nil, got %v", scheme.Color.Surface.BasePressed)
	}
	if scheme.Color.Surface.BaseDisabled != nil {
		t.Errorf("expected BaseDisabled to be nil, got %v", scheme.Color.Surface.BaseDisabled)
	}
	expectedFocus := color.RGBA{R: 0x44, G: 0x44, B: 0x44, A: 255}
	if scheme.Color.Surface.BaseFocus != expectedFocus {
		t.Errorf("expected BaseFocus %v, got %v", expectedFocus, scheme.Color.Surface.BaseFocus)
	}
}

func TestProviderTransparent(t *testing.T) {
	var captured *Scheme
	TestComp := kitex.FC("TestComp", func(struct{}) kitex.Node {
		captured = UseTheme()
		return nil
	})

	env := testenv.Default(80, 24)
	defer env.Close()
	container := element.NewBox(env.Document())
	env.Mount(container)

	focusColor := color.RGBA{R: 0x55, G: 0x55, B: 0x55, A: 255}
	baseScheme := &Scheme{
		Name: "provider-test",
		Color: Color{
			Surface: SurfaceColor{
				Base:         color.RGBA{R: 1, G: 1, B: 1, A: 255},
				BaseHover:    color.RGBA{R: 2, G: 2, B: 2, A: 255},
				BasePressed:  color.RGBA{R: 3, G: 3, B: 3, A: 255},
				BaseFocus:    focusColor,
				BaseDisabled: color.RGBA{R: 5, G: 5, B: 5, A: 255},
			},
		},
	}

	kitex.Render(
		Provider(Props{Theme: baseScheme, Transparent: true},
			TestComp(struct{}{}),
		),
		container,
	)
	env.Flush()

	if captured == nil {
		t.Fatal("expected UseTheme() to capture scheme in Provider, got nil")
	}
	if !captured.Transparent {
		t.Errorf("expected captured.Transparent to be true")
	}
	if captured.Color.Surface.Base != nil {
		t.Errorf("expected Base to be nil, got %v", captured.Color.Surface.Base)
	}
	if captured.Color.Surface.BaseHover != nil {
		t.Errorf("expected BaseHover to be nil, got %v", captured.Color.Surface.BaseHover)
	}
	if captured.Color.Surface.BasePressed != nil {
		t.Errorf("expected BasePressed to be nil, got %v", captured.Color.Surface.BasePressed)
	}
	if captured.Color.Surface.BaseDisabled != nil {
		t.Errorf("expected BaseDisabled to be nil, got %v", captured.Color.Surface.BaseDisabled)
	}
	if captured.Color.Surface.BaseFocus != focusColor {
		t.Errorf("expected BaseFocus %v, got %v", focusColor, captured.Color.Surface.BaseFocus)
	}
}

func TestSetTransparentAndThemeSwitch(t *testing.T) {
	// Restore original state after test
	origName := GetName()
	origTrans := IsTransparent()
	defer func() {
		_ = Set(origName)
		SetTransparent(origTrans)
	}()

	SetTransparent(true)
	if !IsTransparent() {
		t.Fatal("expected IsTransparent() to be true")
	}

	sch := UseTheme()
	if sch == nil || !sch.Transparent || sch.Color.Surface.Base != nil {
		t.Fatalf("expected transparent scheme with nil Base surface, got %+v", sch)
	}

	// Switch theme with Set
	if err := Set("github-dark"); err != nil {
		t.Fatalf("Set theme failed: %v", err)
	}

	sch2 := UseTheme()
	if sch2.Name != "github-dark" {
		t.Errorf("expected theme github-dark, got %s", sch2.Name)
	}
	if !sch2.Transparent {
		t.Errorf("expected scheme to preserve Transparent=true after Set")
	}
	if sch2.Color.Surface.Base != nil {
		t.Errorf("expected Base surface to be nil after theme switch with transparency, got %v", sch2.Color.Surface.Base)
	}

	// Toggle transparency back off
	SetTransparent(false)
	if IsTransparent() {
		t.Fatal("expected IsTransparent() to be false")
	}
	sch3 := UseTheme()
	if sch3.Transparent {
		t.Errorf("expected Transparent=false after SetTransparent(false)")
	}
	if sch3.Color.Surface.Base == nil {
		t.Errorf("expected non-nil Base surface after SetTransparent(false)")
	}
}
