package ui

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// Header holds the view-model fields for the 3-line REPL startup header.
type Header struct {
	Version  string
	Model    string
	Thinking string
	Path     string
}

// NormalizeVersion ensures a version string starts with 'v' and defaults to "v0.1.0".
func NormalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "v0.1.0"
	}
	if !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}

// ShortenHomePath replaces a leading user home directory with "~".
func ShortenHomePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	home = filepath.Clean(home)
	cleaned := filepath.Clean(path)
	if cleaned == home {
		return "~"
	}
	if strings.HasPrefix(cleaned, home+string(filepath.Separator)) {
		return "~" + cleaned[len(home):]
	}
	return path
}

// Render formats the 3-line startup header using the REPL theme styles.
func (h Header) Render() string {
	version := NormalizeVersion(h.Version)
	model := cmp.Or(strings.TrimSpace(h.Model), "default")
	thinking := cmp.Or(strings.TrimSpace(h.Thinking), "auto")
	path := ShortenHomePath(h.Path)

	line1 := theme.BannerTitleStyle.Render("TaskSmith") + " " + theme.BannerMetaStyle.Render(version)
	line2 := theme.BannerMetaStyle.Render(fmt.Sprintf("%s with %s effort", model, thinking))
	line3 := theme.StatusBarStyle.Render(path)

	return line1 + "\n" + line2 + "\n" + line3
}

// FormatHeader renders the 3-line startup header banner for a REPL session.
func FormatHeader(h Header) string {
	return h.Render()
}
