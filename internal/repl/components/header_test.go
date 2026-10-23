package components

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHeaderComponent(t *testing.T) {
	render := renderMessagePart(t, 80)
	out := render(Header(HeaderProps{
		Version:  "0.1.0",
		Model:    "Claude 3.7 Sonnet",
		Thinking: "high",
		Path:     "/workspace/project",
	}))
	for _, want := range []string{"TaskSmith v0.1.0", "Claude 3.7 Sonnet with high effort", "/workspace/project"} {
		if !strings.Contains(out, want) {
			t.Errorf("header missing %q:\n%s", want, out)
		}
	}
}

func TestHeaderDefaults(t *testing.T) {
	render := renderMessagePart(t, 80)
	out := render(Header(HeaderProps{Path: "/work"}))
	for _, want := range []string{"TaskSmith v0.1.0", "default with auto effort"} {
		if !strings.Contains(out, want) {
			t.Errorf("header missing %q:\n%s", want, out)
		}
	}
}

func TestNormalizeVersion(t *testing.T) {
	if got := NormalizeVersion("0.1.0"); got != "v0.1.0" {
		t.Errorf("NormalizeVersion('0.1.0') = %q, want 'v0.1.0'", got)
	}
	if got := NormalizeVersion(""); got != "v0.1.0" {
		t.Errorf("NormalizeVersion('') = %q, want 'v0.1.0'", got)
	}
}

func TestShortenHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}
	if shortened := ShortenHomePath(filepath.Join(home, "src", "tasksmith")); !strings.HasPrefix(shortened, "~") {
		t.Errorf("expected ~ prefix for home path, got %q", shortened)
	}
	if got := ShortenHomePath(home); got != "~" {
		t.Errorf("expected ~ for exact home, got %q", got)
	}
}
