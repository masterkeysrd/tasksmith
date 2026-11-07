package components

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"0.1.0", "v0.1.0"},
		{"v1.2.3", "v1.2.3"},
		{"", "v0.0.0-dev"},
		{"   ", "v0.0.0-dev"},
		{"  v2.0.0  ", "v2.0.0"},
	}
	for _, tt := range tests {
		if got := NormalizeVersion(tt.in); got != tt.want {
			t.Errorf("NormalizeVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestShortenHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory available")
	}

	if shortened := ShortenHomePath(filepath.Join(home, "projects", "tasksmith")); !strings.HasPrefix(shortened, "~") {
		t.Errorf("expected ~ prefix for home path, got %q", shortened)
	}
	if got := ShortenHomePath(home); got != "~" {
		t.Errorf("expected ~ for exact home, got %q", got)
	}
	if got := ShortenHomePath("/other/path"); got != "/other/path" {
		t.Errorf("expected unchanged non-home path, got %q", got)
	}
}

func TestHeaderRender(t *testing.T) {
	env := testutils.NewEnv(t, 80, 24)
	text := env.Render(Header(store.HeaderData{
		Version:   "0.1.0",
		Model:     "Claude 3.7 Sonnet",
		Thinking:  "high",
		Workspace: "/workspace/project",
	}))

	for _, want := range []string{
		"◥██████████◣",
		"▐██▌",
		"◢████◣",
		"TaskSmith v0.1.0",
		"Claude 3.7 Sonnet with high effort",
		"/workspace/project",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("header output missing %q, got:\n%s", want, text)
		}
	}
}

func TestHeaderDefaults(t *testing.T) {
	env := testutils.NewEnv(t, 80, 24)
	text := env.Render(Header(store.HeaderData{
		Workspace: "/work",
	}))

	for _, want := range []string{
		"◥██████████◣",
		"▐██▌",
		"◢████◣",
		"TaskSmith v0.0.0-dev",
		"default · provider default",
		"/work",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("header output missing default %q, got:\n%s", want, text)
		}
	}
}

func TestHeaderWithWarnings(t *testing.T) {
	env := testutils.NewEnv(t, 80, 24)
	text := env.Render(Header(store.HeaderData{
		Version:   "0.2.0",
		Model:     "Claude 3.7 Sonnet",
		Thinking:  "high",
		Workspace: "/workspace/project",
		Warnings: []string{
			"Agent configuration missing tools",
			"Workspace config overrides default model",
		},
	}))

	for _, want := range []string{
		"TaskSmith v0.2.0",
		"Claude 3.7 Sonnet with high effort",
		"/workspace/project",
		"⚠ Agent configuration missing tools",
		"⚠ Workspace config overrides default model",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("header output missing %q, got:\n%s", want, text)
		}
	}

	bannerIdx := strings.Index(text, "TaskSmith v0.2.0")
	warn1Idx := strings.Index(text, "⚠ Agent configuration missing tools")
	warn2Idx := strings.Index(text, "⚠ Workspace config overrides default model")

	if warn1Idx <= bannerIdx {
		t.Errorf("expected warning 1 to appear after banner (banner=%d, warn1=%d)", bannerIdx, warn1Idx)
	}
	if warn2Idx <= warn1Idx {
		t.Errorf("expected warning 2 to appear after warning 1 (warn1=%d, warn2=%d)", warn1Idx, warn2Idx)
	}

	frame := env.Backend.LastFrame().Surface
	if frame == nil {
		t.Fatal("expected rendered surface")
	}
	scheme := theme.Get()
	expectedFg := scheme.Color.Text.Warning
	var foundIcon bool
	for y := 0; y < frame.Height; y++ {
		for x := 0; x < frame.Width; x++ {
			c := frame.CellAt(x, y)
			if c.Content == "⚠" {
				foundIcon = true
				if c.Fg != expectedFg {
					t.Errorf("expected warning icon color %v, got %v", expectedFg, c.Fg)
				}
				break
			}
		}
		if foundIcon {
			break
		}
	}
	if !foundIcon {
		t.Errorf("warning icon '⚠' not found in surface")
	}
}

func TestHeaderThemeSwitching(t *testing.T) {
	testHeaderThemeSwitching(t)
}

func TestHeaderThemeReactivity(t *testing.T) {
	testHeaderThemeSwitching(t)
}

func testHeaderThemeSwitching(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-header-test")
	t.Cleanup(xdg.ClearCache)

	origTheme := theme.GetName()
	defer func() {
		_ = theme.Set(origTheme)
	}()

	_ = theme.Set("dark")

	env := testutils.NewEnv(t, 80, 24)
	env.Render(Header(store.HeaderData{
		Version:   "1.0.0",
		Model:     "default",
		Thinking:  "auto",
		Workspace: "/ws",
	}))

	frame := env.Backend.LastFrame().Surface
	if frame == nil {
		t.Fatal("expected rendered surface")
	}

	var textX, textY int = -1, -1
	var initialFg any
	for y := 0; y < frame.Height; y++ {
		for x := 0; x < frame.Width; x++ {
			c := frame.CellAt(x, y)
			if c.Content == "T" {
				textX, textY = x, y
				initialFg = c.Fg
				break
			}
		}
		if textX != -1 {
			break
		}
	}
	if textX == -1 {
		t.Fatalf("expected 'T' in rendered surface, got surface:\n%s", env.Text())
	}

	// Switching the theme publishes new scheme state, which re-renders on the
	// next flushed frame.
	if err := theme.Set("github-dark"); err != nil {
		t.Fatalf("failed to set theme: %v", err)
	}
	env.Flush()

	frame = env.Backend.LastFrame().Surface
	if frame == nil {
		t.Fatal("expected rendered surface after theme switch")
	}
	updatedFg := frame.CellAt(textX, textY).Fg

	if updatedFg == nil || updatedFg == initialFg {
		t.Fatalf("expected foreground color to change after theme switch, initial=%v, updated=%v", initialFg, updatedFg)
	}
}

func TestHeaderCustomRegisteredTheme(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-header-test")
	t.Cleanup(xdg.ClearCache)

	origTheme := theme.GetName()
	defer func() {
		_ = theme.Set(origTheme)
	}()

	dir, err := xdg.SubConfigDir("themes")
	if err != nil {
		t.Fatalf("failed to get themes dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create themes dir: %v", err)
	}
	customJSON := `{
		"name": "custom-header-test",
		"extends": "dark",
		"colors": {
			"text": {
				"primary": "#123456"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "custom-header-test.json"), []byte(customJSON), 0o644); err != nil {
		t.Fatalf("failed to write custom theme: %v", err)
	}

	if err := theme.Set("custom-header-test"); err != nil {
		t.Fatalf("failed to set custom theme: %v", err)
	}

	env := testutils.NewEnv(t, 80, 24)
	env.Render(Header(store.HeaderData{
		Version:   "1.0.0",
		Model:     "default",
		Thinking:  "auto",
		Workspace: "/ws",
	}))

	if env.Backend.LastFrame().Surface == nil {
		t.Fatal("expected rendered surface")
	}
}
