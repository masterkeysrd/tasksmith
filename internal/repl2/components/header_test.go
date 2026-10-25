package components

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func surfaceText(b *mock.Backend) string {
	frame := b.LastFrame().Surface
	if frame == nil {
		return ""
	}
	var sb strings.Builder
	for y := 0; y < frame.Height; y++ {
		var line strings.Builder
		for x := 0; x < frame.Width; x++ {
			c := frame.CellAt(x, y)
			if c.Content != "" {
				line.WriteString(c.Content)
			} else {
				line.WriteByte(' ')
			}
		}
		trimmed := strings.TrimRight(line.String(), " ")
		if trimmed != "" {
			sb.WriteString(trimmed)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

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
	b := mock.New(80, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	node := Header(store.HeaderData{
		Version:   "0.1.0",
		Model:     "Claude 3.7 Sonnet",
		Thinking:  "high",
		Workspace: "/workspace/project",
	})

	go func() {
		_ = trail.Run(ctx, node, trail.WithBackend(b))
	}()

	var text string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		text = surfaceText(b)
		if strings.Contains(text, "TaskSmith") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

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
	b := mock.New(80, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	node := Header(store.HeaderData{
		Workspace: "/work",
	})

	go func() {
		_ = trail.Run(ctx, node, trail.WithBackend(b))
	}()

	var text string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		text = surfaceText(b)
		if strings.Contains(text, "TaskSmith") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	for _, want := range []string{
		"◥██████████◣",
		"▐██▌",
		"◢████◣",
		"TaskSmith v0.0.0-dev",
		"default with auto effort",
		"/work",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("header output missing default %q, got:\n%s", want, text)
		}
	}
}

func TestHeaderThemeSwitching(t *testing.T) {
	origTheme := theme.GetName()
	defer func() {
		_ = theme.Set(origTheme)
	}()

	_ = theme.Set("default")

	b := mock.New(80, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, Header(store.HeaderData{
			Version:   "1.0.0",
			Model:     "default",
			Thinking:  "auto",
			Workspace: "/ws",
		}), trail.WithBackend(b))
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.LastFrame().Surface != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	frame := b.LastFrame().Surface
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
		t.Fatalf("expected 'T' in rendered surface, got surface:\n%s", surfaceText(b))
	}

	// Switch theme to "github-dark"
	if err := theme.Set("github-dark"); err != nil {
		t.Fatalf("failed to set theme: %v", err)
	}

	// Wait for rerender with new color
	var updatedFg any
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		frame = b.LastFrame().Surface
		if frame != nil {
			c := frame.CellAt(textX, textY)
			if c.Fg != initialFg {
				updatedFg = c.Fg
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}

	if updatedFg == nil || updatedFg == initialFg {
		t.Fatalf("expected foreground color to change after theme switch, initial=%v, updated=%v", initialFg, updatedFg)
	}
}
