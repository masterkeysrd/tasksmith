package icon

import (
	"image/color"

	"github.com/lrstanley/go-nf/glyphs/neo"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// FileIconProps defines properties for the FileIcon component.
type FileIconProps struct {
	Path string
}

// FileIcon resolves a Nerd Font icon dynamically by the given file path.
// It relies on the go-nf library's internal map-lookup cache (which is O(1) and extremely fast),
// and falls back to a default file icon if no matching icon is found.
var FileIcon = kitex.FC("FileIcon", func(props FileIconProps) kitex.Node {
	t := theme.UseTheme()
	dark := true
	if t != nil && t.Type == "light" {
		dark = false
	}

	res := neo.ByPath(props.Path)
	if res == nil {
		var fg color.Color
		if t != nil {
			fg = t.Color.Text.Secondary
		}
		return kitex.Span(kitex.SpanProps{
			Style: style.S().Foreground(fg),
		}, File)
	}

	return kitex.Span(kitex.SpanProps{
		Style: style.S().Foreground(res.Color(dark)),
	}, kitex.Text(res.String()))
})
