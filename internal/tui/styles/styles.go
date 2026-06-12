// Package styles maps resolved colorscheme highlights to Kite style.Style values.
package styles

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/colorscheme"
)

// parseColor converts a hex color string (with or without # prefix) to RGBA.
// Supports 6-char (RGB) and 8-char (RGBA) hex formats.
func parseColor(hex string) (color.RGBA, error) {
	hex = strings.TrimPrefix(hex, "#")
	switch len(hex) {
	case 6:
		var r, g, b uint8
		_, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
		if err != nil {
			return color.RGBA{}, fmt.Errorf("invalid hex color: %q", hex)
		}
		return color.RGBA{R: r, G: g, B: b, A: 255}, nil
	case 8:
		var r, g, b, a uint8
		_, err := fmt.Sscanf(hex, "%02x%02x%02x%02x", &r, &g, &b, &a)
		if err != nil {
			return color.RGBA{}, fmt.Errorf("invalid hex color: %q", hex)
		}
		return color.RGBA{R: r, G: g, B: b, A: a}, nil
	default:
		return color.RGBA{}, fmt.Errorf("invalid hex color: %q", hex)
	}
}

// highlightToStyle converts a ResolvedColor to a style.Style.
func highlightToStyle(h colorscheme.ResolvedColor) style.Style {
	s := style.S()
	if h.Fg != "" {
		if c, err := parseColor(h.Fg); err == nil {
			s = s.Foreground(color.Color(c))
		}
	}
	if h.Bg != "" {
		if c, err := parseColor(h.Bg); err == nil {
			s = s.Background(color.Color(c))
		}
	}
	if h.Bold {
		s = s.Bold(true)
	}
	if h.Underline {
		s = s.Underline(true)
	}
	if h.Italic {
		s = s.Italic(true)
	}
	return s
}

// BuildFrom takes a resolved colorscheme map and returns a map of
// style.Name -> style.Style for all highlight groups and UI elements.
// The returned map is immutable; callers must not modify it.
func BuildFrom(resolved map[string]colorscheme.ResolvedColor) map[string]style.Style {
	if len(resolved) == 0 {
		return nil
	}

	result := make(map[string]style.Style)

	// Direct highlight mappings — iterate over all keys in the resolved map.
	for name, h := range resolved {
		result[name] = highlightToStyle(h)
	}

	return result
}
