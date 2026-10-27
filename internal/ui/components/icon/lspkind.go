package icon

import (
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// LspKindIcon resolves a Nerd Font icon and theme color for a given LSP symbol kind.
func LspKindIcon(kind string, t *theme.Scheme) (kitex.Node, color.Color) {
	if t == nil {
		t = theme.UseTheme()
	}
	var glyph string
	var col color.Color

	switch strings.ToLower(kind) {
	case "file":
		glyph = "󰈔"
		col = t.Color.Text.Primary
	case "module", "namespace", "package":
		glyph = "󰏗" // Package icon
		col = t.Color.Icon.Info
	case "class":
		glyph = "󰠱" // Class
		col = t.Color.Syntax.Type
	case "method":
		glyph = "󰆧" // Method/cube
		col = t.Color.Syntax.Function
	case "property", "field":
		glyph = "󰫧" // Field
		col = t.Color.Text.Secondary
	case "constructor":
		glyph = "󰡱" // Constructor
		col = t.Color.Syntax.Function
	case "enum":
		glyph = "󰦻" // Enum
		col = t.Color.Syntax.Type
	case "interface":
		glyph = "󰓡" // Interface / Connection
		col = t.Color.Icon.Info
	case "function":
		glyph = "󰊕" // Function
		col = t.Color.Syntax.Function
	case "variable":
		glyph = "󰆦" // Variable
		col = t.Color.Syntax.Variable
	case "constant":
		glyph = "󰏿" // Tag / Constant
		col = t.Color.Syntax.Number
	case "struct":
		glyph = "󰆼" // Struct / database/structure icon
		col = t.Color.Syntax.Type
	case "typeparameter":
		glyph = "󰗄" // Type parameter
		col = t.Color.Syntax.Type
	default:
		glyph = "󰚞" // Diamond
		col = t.Color.Text.Muted
	}

	return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(col)}, kitex.Text(glyph)), col
}
