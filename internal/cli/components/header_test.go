package components

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
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
	text := env.Render(Header(HeaderProps{HeaderData: store.HeaderData{
		Version:   "0.1.0",
		Model:     "Claude 3.7 Sonnet",
		Thinking:  "high",
		Workspace: "/workspace/project",
	}}))

	for _, want := range []string{
		"▜████████▛",
		"█▜▛█",
		"▟▛▀▀▜▙",
		"TaskSmith v0.1.0",
		"Claude 3.7 Sonnet with high effort",
		"/workspace/project",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("header output missing %q, got:\n%s", want, text)
		}
	}
}

func TestHeaderWorkspaceLine(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		mode   api.SandboxMode
		want   string
	}{
		{"branch and sandbox", "main", api.SandboxNative, "~/repo · main · native sandbox"},
		{"branch only", "main", "", "~/repo · main"},
		{"sandbox only", "", api.SandboxNative, "~/repo · native sandbox"},
		{"workspace only", "", "", "~/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := store.HeaderData{
				Workspace:   "~/repo",
				Branch:      tt.branch,
				SandboxMode: tt.mode,
			}
			out := testutils.Render(t, Header(HeaderProps{HeaderData: data}), 80, 12)
			if !strings.HasSuffix(strings.TrimSpace(out), tt.want) {
				t.Errorf("expected workspace line ending in %q, got:\n%s", tt.want, out)
			}
		})
	}
}

func TestHeaderResolvedProvider(t *testing.T) {
	store.ResetSession()
	t.Cleanup(store.ResetSession)
	store.SetSession(&api.Session{Model: &api.Model{Provider: "antigravity", Label: "Gemini 4 Neon"}})
	out := testutils.Render(t, Header(HeaderProps{HeaderData: store.CurrentHeaderData()}), 80, 12)
	if !strings.Contains(out, "Gemini 4 Neon (antigravity) with default effort") {
		t.Fatalf("unexpected banner:\n%s", out)
	}
}

func TestHeaderDefaults(t *testing.T) {
	env := testutils.NewEnv(t, 80, 24)
	text := env.Render(Header(HeaderProps{HeaderData: store.HeaderData{
		Workspace: "/work",
	}}))

	for _, want := range []string{
		"▜████████▛",
		"█▜▛█",
		"▟▛▀▀▜▙",
		"TaskSmith v0.0.0-dev",
		"default with default effort",
		"/work",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("header output missing default %q, got:\n%s", want, text)
		}
	}
}

func TestHeaderWithWarnings(t *testing.T) {
	env := testutils.NewEnv(t, 80, 24)
	text := env.Render(Header(HeaderProps{HeaderData: store.HeaderData{
		Version:   "0.2.0",
		Model:     "Claude 3.7 Sonnet",
		Thinking:  "high",
		Workspace: "/workspace/project",
		Warnings: []string{
			"Agent configuration missing tools",
			"Workspace config overrides default model",
		},
	}}))

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

func TestHeaderSandboxModeSuffix(t *testing.T) {
	env := testutils.NewEnv(t, 80, 24)
	text := env.Render(Header(HeaderProps{HeaderData: store.HeaderData{
		Version:     "0.3.0",
		Model:       "Claude 3.7 Sonnet",
		Thinking:    "high",
		Workspace:   "/workspace/project",
		SandboxMode: "native",
	}}))

	if !strings.Contains(text, "/workspace/project · native sandbox") {
		t.Errorf("expected workspace line with native sandbox suffix, got:\n%s", text)
	}

	if env.Backend.LastFrame().Surface == nil {
		t.Fatal("expected rendered surface")
	}

	env.Render(Header(HeaderProps{HeaderData: store.HeaderData{
		Version:     "0.3.0",
		Model:       "Claude 3.7 Sonnet",
		Thinking:    "high",
		Workspace:   "/workspace/project",
		SandboxMode: "local",
	}}))
	text = env.Text()
	if !strings.Contains(text, "/workspace/project · local sandbox") {
		t.Errorf("expected workspace line with local sandbox suffix, got:\n%s", text)
	}

	env.Render(Header(HeaderProps{HeaderData: store.HeaderData{
		Version:   "0.3.0",
		Model:     "Claude 3.7 Sonnet",
		Thinking:  "high",
		Workspace: "/workspace/project",
	}}))
	text = env.Text()
	if strings.Contains(text, "sandbox") {
		t.Errorf("expected no sandbox suffix before a session exists, got:\n%s", text)
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
	env.Render(Header(HeaderProps{HeaderData: store.HeaderData{
		Version:   "1.0.0",
		Model:     "default",
		Thinking:  "auto",
		Workspace: "/ws",
	}}))

	frame := env.Backend.LastFrame().Surface
	if frame == nil {
		t.Fatal("expected rendered surface")
	}

	textX, textY := -1, -1
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
	env.Render(Header(HeaderProps{HeaderData: store.HeaderData{
		Version:   "1.0.0",
		Model:     "default",
		Thinking:  "auto",
		Workspace: "/ws",
	}}))

	if env.Backend.LastFrame().Surface == nil {
		t.Fatal("expected rendered surface")
	}
}
