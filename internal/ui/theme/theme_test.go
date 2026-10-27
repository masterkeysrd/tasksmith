package theme

import (
	"image/color"
	"strings"
	"testing"
)

func TestBuiltinThemesLoadAndResolve(t *testing.T) {
	builtins := []string{"dark", "light", "tokyo-night", "solarized", "github-dark", "catppuccin", "gruvbox"}

	for _, name := range builtins {
		t.Run(name, func(t *testing.T) {
			th, err := Find(name)
			if err != nil {
				t.Fatalf("Find(%q) failed: %v", name, err)
			}
			if th.Name != name {
				t.Errorf("expected theme name %q, got %q", name, th.Name)
			}

			for _, client := range []Client{ClientCLI, ClientTUI, ClientDesktop} {
				s, err := ResolveFor(th, client)
				if err != nil {
					t.Fatalf("ResolveFor(%q, %q) failed: %v", name, client, err)
				}
				if s == nil {
					t.Fatalf("expected non-nil Scheme for %q", name)
				}
				if s.Color.Text.Primary == nil {
					t.Errorf("theme %q client %q has nil Text.Primary", name, client)
				}
				if s.Color.Border.Default == nil {
					t.Errorf("theme %q client %q has nil Border.Default", name, client)
				}
				if s.Color.Agent.Thinking == nil {
					t.Errorf("theme %q client %q has nil Agent.Thinking", name, client)
				}
				if s.Color.Mode.Plan == nil {
					t.Errorf("theme %q client %q has nil Mode.Plan", name, client)
				}
				if s.Color.Diff.AddedText == nil {
					t.Errorf("theme %q client %q has nil Diff.AddedText", name, client)
				}
				if s.Color.Syntax.Keyword == nil {
					t.Errorf("theme %q client %q has nil Syntax.Keyword", name, client)
				}
			}
		})
	}
}

func TestExtendsInheritanceAndCycleDetection(t *testing.T) {
	// 1. Inheritance
	th, err := Find("tokyo-night")
	if err != nil {
		t.Fatalf("Find(tokyo-night): %v", err)
	}
	s, err := ResolveFor(th, ClientTUI)
	if err != nil {
		t.Fatalf("ResolveFor(tokyo-night): %v", err)
	}
	// tokyo-night overrides text.primary to #c0caf5
	expected := color.RGBA{R: 0xc0, G: 0xca, B: 0xf5, A: 255}
	if s.Color.Text.Primary != expected {
		t.Errorf("expected Text.Primary %v, got %v", expected, s.Color.Text.Primary)
	}
	// Inherited from dark theme (agent.executing = #56d4dd)
	expectedAgent := color.RGBA{R: 0x56, G: 0xd4, B: 0xdd, A: 255}
	if s.Color.Agent.Executing != expectedAgent {
		t.Errorf("expected Agent.Executing %v, got %v", expectedAgent, s.Color.Agent.Executing)
	}

	// 2. Cycle detection: theme A extends A
	cycleTheme := &Theme{
		Name:    "self-cycle",
		Extends: "self-cycle",
	}
	_, err = ResolveFor(cycleTheme, ClientCLI)
	if err == nil || !strings.Contains(err.Error(), "cycle detected") {
		t.Errorf("expected cycle detected error, got %v", err)
	}

	// 3. Missing parent
	missingParent := &Theme{
		Name:    "child",
		Extends: "non-existent-parent",
	}
	_, err = ResolveFor(missingParent, ClientCLI)
	if err == nil || !strings.Contains(err.Error(), "parent theme") {
		t.Errorf("expected parent not found error, got %v", err)
	}
}

func TestClientsPerClientOverrides(t *testing.T) {
	th, err := Find("dark")
	if err != nil {
		t.Fatalf("Find(dark): %v", err)
	}

	cliScheme, err := ResolveFor(th, ClientCLI)
	if err != nil {
		t.Fatalf("ResolveFor(dark, cli): %v", err)
	}
	if !cliScheme.Transparent {
		t.Error("expected ClientCLI to be transparent")
	}
	if cliScheme.Color.Surface.Canvas != nil {
		t.Errorf("expected Surface.Canvas nil for transparent CLI, got %v", cliScheme.Color.Surface.Canvas)
	}
	if cliScheme.Color.Transcript.UserBg != nil {
		t.Errorf("expected Transcript.UserBg nil for CLI override, got %v", cliScheme.Color.Transcript.UserBg)
	}

	tuiScheme, err := ResolveFor(th, ClientTUI)
	if err != nil {
		t.Fatalf("ResolveFor(dark, tui): %v", err)
	}
	if tuiScheme.Transparent {
		t.Error("expected ClientTUI not to be transparent")
	}
	if tuiScheme.Color.Surface.Canvas == nil {
		t.Error("expected Surface.Canvas non-nil for non-transparent TUI")
	}
	if tuiScheme.Color.Transcript.UserBg == nil {
		t.Error("expected Transcript.UserBg non-nil for TUI")
	}
}

func TestMissingRequiredTokenError(t *testing.T) {
	// Copy a valid theme and blank out a token
	th := &Theme{
		Name: "missing-token",
		Palette: map[string]string{
			"fg": "#ffffff",
		},
		Colors: ThemeColors{
			Text: ThemeTextColor{
				// Primary is empty!
				Secondary: "fg",
			},
		},
	}
	_, err := ResolveFor(th, ClientCLI)
	if err == nil || !strings.Contains(err.Error(), "missing required color token") {
		t.Fatalf("expected missing required color token error, got %v", err)
	}
}

func TestInvalidHexColorError(t *testing.T) {
	th := &Theme{
		Name: "invalid-hex",
		Colors: ThemeColors{
			Text: ThemeTextColor{
				Primary: "#not-a-hex",
			},
		},
	}
	_, err := ResolveFor(th, ClientCLI)
	if err == nil || !strings.Contains(err.Error(), "invalid hex color") {
		t.Fatalf("expected invalid hex color error, got %v", err)
	}
}

func TestTransparentSurfacesCleared(t *testing.T) {
	th, err := Find("dark")
	if err != nil {
		t.Fatalf("Find(dark): %v", err)
	}
	s, err := ResolveFor(th, ClientTUI)
	if err != nil {
		t.Fatalf("ResolveFor: %v", err)
	}

	// In TUI (non-transparent), canvas and panel are non-nil
	if s.Color.Surface.Canvas == nil || s.Color.Surface.Panel == nil {
		t.Fatal("expected non-nil surfaces before transparency")
	}

	trans := s.WithTransparent(true)
	if trans.Color.Surface.Canvas != nil {
		t.Errorf("expected Canvas nil, got %v", trans.Color.Surface.Canvas)
	}
	if trans.Color.Surface.Panel != nil {
		t.Errorf("expected Panel nil, got %v", trans.Color.Surface.Panel)
	}
	if trans.Color.Surface.Hover != nil {
		t.Errorf("expected Hover nil, got %v", trans.Color.Surface.Hover)
	}
	if trans.Color.Surface.Active != nil {
		t.Errorf("expected Active nil, got %v", trans.Color.Surface.Active)
	}
	if trans.Color.Surface.Disabled != nil {
		t.Errorf("expected Disabled nil, got %v", trans.Color.Surface.Disabled)
	}
	// Preserved surfaces and tints
	if trans.Color.Surface.Elevated == nil {
		t.Error("expected Elevated to be preserved")
	}
	if trans.Color.Surface.Code == nil {
		t.Error("expected Code to be preserved")
	}
	if trans.Color.Surface.Selected == nil {
		t.Error("expected Selected to be preserved")
	}
	if trans.Color.Tint.Accent == nil {
		t.Error("expected Tint.Accent to be preserved")
	}
}

func TestZeroFallbacksGet(t *testing.T) {
	sch := Get()
	if sch == nil {
		t.Fatal("expected Get() to never return nil")
	}
	if sch.Color.Text.Primary == nil {
		t.Fatal("expected Get().Color.Text.Primary to be non-nil")
	}
}
