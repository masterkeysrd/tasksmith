package icon

import (
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// LspKindIcon resolves a Nerd Font icon and theme color for a given LSP symbol kind.
func LspKindIcon(kind string, t *theme.Scheme) (kitex.Node, color.Color) {
	if t == nil {
		return nil, nil
	}
	var glyph string
	var col color.Color

	switch strings.ToLower(kind) {
	case "file":
		glyph = "󰈔"
		col = t.Color.Text.Primary
	case "module", "namespace", "package":
		glyph = "󰏗" // Package icon
		col = t.Color.Surface.Info
	case "class":
		glyph = "󰠱" // Class
		col = t.Color.Text.Purple
	case "method":
		glyph = "󰆧" // Method/cube
		col = t.Color.Text.Purple
	case "property", "field":
		glyph = "󰫧" // Field
		col = t.Color.Text.Secondary
	case "constructor":
		glyph = "󰡱" // Constructor
		col = t.Color.Text.Purple
	case "enum":
		glyph = "󰦻" // Enum
		col = t.Color.Text.Purple
	case "interface":
		glyph = "󰓡" // Interface / Connection
		col = t.Color.Surface.Info
	case "function":
		glyph = "󰊕" // Function
		col = t.Color.Text.Purple
	case "variable":
		glyph = "󰆦" // Variable
		col = t.Color.Text.Secondary
	case "constant":
		glyph = "󰏿" // Tag / Constant
		col = t.Color.Text.Purple
	case "struct":
		glyph = "󰆼" // Struct / database/structure icon
		col = t.Color.Text.Primary
	case "typeparameter":
		glyph = "󰗄" // Type parameter
		col = t.Color.Text.Primary
	default:
		glyph = "󰚞" // Diamond
		col = t.Color.Text.Tertiary
	}

	return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(col)}, kitex.Text(glyph)), col
}
